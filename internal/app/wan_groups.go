package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/devices"
	"gatehouse/internal/fw"
	"gatehouse/internal/netd"
	"gatehouse/internal/wanmon"
)

// Connection groups. The Default group is every connection in the order and shares kept on the
// connections themselves, and carries everything that is not sent elsewhere. Named groups pick
// some connections and send chosen networks, devices or ranges through them instead.

const (
	maxWanGroups   = wanmon.GroupTableMax - wanmon.GroupTableMin + 1
	defaultLabelKV = "wan.default_label"
)

type groupMember struct {
	WanID   int64  `json:"wan_id"`
	Name    string `json:"name,omitempty"`
	Weight  int    `json:"weight"`
	Enabled bool   `json:"enabled"`
}

type groupUse struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Label string `json:"label"`
}

type groupRow struct {
	ID       int64         `json:"id"`
	Name     string        `json:"name"`
	Mode     string        `json:"mode"`
	Fallback string        `json:"fallback"`
	Members  []groupMember `json:"members"`
	Uses     []groupUse    `json:"uses"`
	Resolved []string      `json:"resolved"`
	Warnings []string      `json:"warnings,omitempty"`
}

func (a *App) listGroups(q querier) ([]groupRow, error) {
	rows, err := q.Query(`SELECT id,name,mode,fallback FROM wan_groups ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	out := []groupRow{}
	for rows.Next() {
		var g groupRow
		if err := rows.Scan(&g.ID, &g.Name, &g.Mode, &g.Fallback); err != nil {
			rows.Close()
			return nil, err
		}
		g.Members, g.Uses, g.Resolved = []groupMember{}, []groupUse{}, []string{}
		out = append(out, g)
	}
	rows.Close()
	for i := range out {
		g := &out[i]
		mr, err := q.Query(`SELECT m.wan_id,w.name,m.weight,w.enabled FROM wan_group_members m JOIN wan_links w ON w.id=m.wan_id
			WHERE m.group_id=? ORDER BY m.position`, g.ID)
		if err != nil {
			return nil, err
		}
		for mr.Next() {
			var m groupMember
			var en int
			mr.Scan(&m.WanID, &m.Name, &m.Weight, &en)
			m.Enabled = en == 1
			g.Members = append(g.Members, m)
		}
		mr.Close()
		ur, err := q.Query(`SELECT kind,value,label FROM wan_group_routes WHERE group_id=? ORDER BY id`, g.ID)
		if err != nil {
			return nil, err
		}
		for ur.Next() {
			var u groupUse
			ur.Scan(&u.Kind, &u.Value, &u.Label)
			g.Uses = append(g.Uses, u)
		}
		ur.Close()
		seen := map[string]bool{}
		for _, u := range g.Uses {
			srcs, _ := fw.TunnelSourcesFor(q, u.Kind, u.Value)
			for _, s := range srcs {
				if !seen[s] {
					seen[s] = true
					g.Resolved = append(g.Resolved, s)
				}
			}
		}
	}
	return out, nil
}

// addGroupsToPlan turns saved groups into what the helper installs. A group whose connections
// are all switched off is left out, so its devices simply use the default connections.
func (a *App) addGroupsToPlan(q querier, p *netd.Plan) error {
	gs, err := a.listGroups(q)
	if err != nil {
		return err
	}
	on := map[string]bool{}
	for _, l := range p.Links {
		on[l.Name] = true
	}
	idx := 0
	for _, g := range gs {
		spec := netd.GroupSpec{Name: g.Name, Table: wanmon.GroupTableMin + idx, Prio: wanmon.GroupPrioMin + idx,
			Mode: g.Mode, Block: g.Fallback == "block", Sources: g.Resolved}
		for _, m := range g.Members {
			if on[m.Name] {
				spec.Members = append(spec.Members, netd.GroupMember{Link: m.Name, Weight: max(m.Weight, 1)})
			}
		}
		if len(spec.Members) == 0 {
			continue
		}
		if spec.Sources == nil {
			spec.Sources = []string{}
		}
		p.Groups = append(p.Groups, spec)
		idx++
	}
	return nil
}

type groupIn struct {
	ID       int64         `json:"id"`
	Name     string        `json:"name"`
	Mode     string        `json:"mode"`
	Fallback string        `json:"fallback"`
	Members  []groupMember `json:"members"`
	Uses     []groupUse    `json:"uses"`
}

func (a *App) registerGroupRoutes() {
	s := a.API
	s.Handle("GET /api/net/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		gs, err := a.listGroups(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		vpnSrc, _ := fw.TunnelSources(a.Config)
		taken := map[string]bool{}
		for _, v := range vpnSrc {
			for _, s := range v {
				taken[s] = true
			}
		}
		for i := range gs {
			for _, s := range gs[i].Resolved {
				if taken[s] {
					gs[i].Warnings = append(gs[i].Warnings, s+" is also sent through a VPN, and the VPN wins")
				}
			}
		}
		wans, err := a.listWans(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		def := []groupMember{}
		for _, x := range wans {
			def = append(def, groupMember{WanID: x.ID, Name: x.Name, Weight: x.Weight, Enabled: x.Enabled})
		}
		api.JSON(w, 200, map[string]any{
			"default": map[string]any{"label": a.Config.Setting(defaultLabelKV, "Default"), "mode": a.Config.Setting("wan.mode", "failover"), "members": def},
			"groups":  gs,
			"max":     maxWanGroups,
		})
	})
	s.HandleAdmin("PUT /api/net/default-group", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Label   string        `json:"label"`
			Mode    string        `json:"mode"`
			Members []groupMember `json:"members"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Label = strings.TrimSpace(in.Label)
		if in.Label == "" || len(in.Label) > 40 {
			fail(w, errors.New("give the default group a short name, like Default"))
			return
		}
		if in.Mode != "failover" && in.Mode != "balance" {
			fail(w, errors.New("choose failover or balance"))
			return
		}
		if err := checkShares(in.Mode, in.Members); err != nil {
			fail(w, err)
			return
		}
		err := a.mutateNet(func(tx *sql.Tx) error {
			for i, m := range in.Members {
				if _, err := tx.Exec(`UPDATE wan_links SET priority=?,weight=? WHERE id=?`, i+1, max(m.Weight, 1), m.WanID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "wan.mode", in.Mode); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, defaultLabelKV, in.Label)
			return err
		})
		if err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		ok(w)
	})
	s.HandleAdmin("PUT /api/net/groups", a.saveGroup)
	s.HandleAdmin("DELETE /api/net/groups/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := a.mutateNet(func(tx *sql.Tx) error { _, e := tx.Exec(`DELETE FROM wan_groups WHERE id=?`, id); return e }); err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		ok(w)
	})
}

// checkShares makes sure sharing the load is spelled out clearly: percentages that add up to 100.
func checkShares(mode string, ms []groupMember) error {
	if len(ms) == 0 {
		return errors.New("choose at least one connection")
	}
	if mode != "balance" || len(ms) < 2 {
		return nil
	}
	sum := 0
	for _, m := range ms {
		if m.Weight < 1 || m.Weight > 100 {
			return errors.New("each share must be between 1 and 100 percent")
		}
		sum += m.Weight
	}
	if sum != 100 {
		return fmt.Errorf("the shares add up to %d%%; they need to add up to 100%%", sum)
	}
	return nil
}

func (a *App) saveGroup(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in groupIn
	if !api.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 40 {
		fail(w, errors.New("give the group a short name"))
		return
	}
	if strings.EqualFold(in.Name, a.Config.Setting(defaultLabelKV, "Default")) {
		fail(w, errors.New("that name is already used by the default group"))
		return
	}
	if in.Mode != "failover" && in.Mode != "balance" {
		fail(w, errors.New("choose failover or balance"))
		return
	}
	if in.Fallback != "default" && in.Fallback != "block" {
		in.Fallback = "default"
	}
	if err := checkShares(in.Mode, in.Members); err != nil {
		fail(w, err)
		return
	}
	devs := map[string]devices.Device{}
	if list, err := a.Devices.List(); err == nil {
		for _, d := range list {
			devs[strings.ToLower(d.MAC)] = d
		}
	}
	var warnings []string
	var id int64
	err := a.mutateNet(func(tx *sql.Tx) error {
		if in.ID == 0 {
			var n int
			tx.QueryRow(`SELECT count(*) FROM wan_groups`).Scan(&n)
			if n >= maxWanGroups {
				return fmt.Errorf("you can have up to %d groups", maxWanGroups)
			}
			res, err := tx.Exec(`INSERT INTO wan_groups(name,mode,fallback,position) VALUES(?,?,?,?)`, in.Name, in.Mode, in.Fallback, n)
			if err != nil {
				return friendlyDup(err, "a group with that name already exists")
			}
			id, _ = res.LastInsertId()
		} else {
			id = in.ID
			res, err := tx.Exec(`UPDATE wan_groups SET name=?,mode=?,fallback=? WHERE id=?`, in.Name, in.Mode, in.Fallback, id)
			if err != nil {
				return friendlyDup(err, "a group with that name already exists")
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errBadID
			}
			tx.Exec(`DELETE FROM wan_group_members WHERE group_id=?`, id)
			tx.Exec(`DELETE FROM wan_group_routes WHERE group_id=?`, id)
		}
		for i, m := range in.Members {
			if _, err := tx.Exec(`INSERT INTO wan_group_members(group_id,wan_id,position,weight) VALUES(?,?,?,?)`, id, m.WanID, i, max(m.Weight, 1)); err != nil {
				return errors.New("one of the chosen connections no longer exists, or is chosen twice")
			}
		}
		seen := map[string]bool{}
		for _, u := range in.Uses {
			u.Value = strings.TrimSpace(u.Value)
			key := u.Kind + "|" + strings.ToLower(u.Value)
			if seen[key] {
				continue
			}
			seen[key] = true
			switch u.Kind {
			case "network":
				var n int
				tx.QueryRow(`SELECT count(*) FROM zones WHERE name=? AND kind NOT IN ('wan','vpn')`, u.Value).Scan(&n)
				if n == 0 {
					return fmt.Errorf("there is no network called %q", u.Value)
				}
			case "device":
				u.Value = strings.ToLower(u.Value)
				d, known := devs[u.Value]
				if !known {
					return fmt.Errorf("that device is not known to the router (%s)", u.Value)
				}
				if err := a.reserveForDevice(tx, d); err != nil {
					warnings = append(warnings, err.Error())
				}
			case "cidr":
				if p, err := netip.ParsePrefix(u.Value); err != nil || !p.Addr().Is4() {
					if ad, err2 := netip.ParseAddr(u.Value); err2 != nil || !ad.Is4() {
						return fmt.Errorf("%q is not an IPv4 address or range like 192.168.1.50 or 192.168.1.0/24", u.Value)
					}
				}
			default:
				return errors.New("choose a network, a device or an address range")
			}
			// A source belongs to one group; choosing it here takes it out of any other.
			if _, err := tx.Exec(`DELETE FROM wan_group_routes WHERE kind=? AND value=? AND group_id<>?`, u.Kind, u.Value, id); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO wan_group_routes(group_id,kind,value,label) VALUES(?,?,?,?)`, id, u.Kind, u.Value, strings.TrimSpace(u.Label)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.DHCP.Reload()
	a.publishNet()
	api.JSON(w, 200, map[string]any{"id": id, "warnings": warnings})
}

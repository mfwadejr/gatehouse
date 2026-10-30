package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/devices"
	"gatehouse/internal/netd"
)

// Virtual Network Override (H38) and new-device approval (H62).
//
// A device is placed on a VLAN network by its hardware address. Gatehouse does this at its own
// port (see netd.OverrideCommands), so the device's DHCP, DNS, firewall rules and logs are those of
// the VLAN network, as if a managed switch had put it there. Approval uses the same move: a device
// never seen before waits on a holding network until the person decides.

const (
	approvalOn      = "approval.enabled"
	approvalHolding = "approval.holding"
)

type ovTarget struct {
	Iface   string `json:"iface"`
	Network string `json:"network"`
	VID     int    `json:"vid"`
	Parent  string `json:"parent"`
}

type ovState struct {
	mu      sync.Mutex
	hash    [32]byte
	have    bool
	at      time.Time
	lastErr string
	holds   map[string]int
}

// ovTargets lists the VLAN networks a device can be placed on.
func (a *App) ovTargets() []ovTarget {
	var out []ovTarget
	rows, err := a.Config.Query(`SELECT i.name,COALESCE(z.name,i.name),i.vlan_id,i.parent FROM interfaces i LEFT JOIN zones z ON z.id=i.zone_id
		WHERE i.kind='vlan' AND i.vlan_id>0 AND i.parent<>'' AND i.enabled=1 ORDER BY i.name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var t ovTarget
		if rows.Scan(&t.Iface, &t.Network, &t.VID, &t.Parent) == nil {
			out = append(out, t)
		}
	}
	return out
}

func ovFind(ts []ovTarget, iface string) (ovTarget, bool) {
	for _, t := range ts {
		if t.Iface == iface {
			return t, true
		}
	}
	return ovTarget{}, false
}

// overrideMaps returns each device's override (by hardware address) and approval state.
func (a *App) overrideMaps() (map[string]ovTarget, map[string]string) {
	ts := a.ovTargets()
	ov, ap := map[string]ovTarget{}, map[string]string{}
	rows, err := a.Config.Query(`SELECT lower(mac),override_iface,approval FROM devices`)
	if err != nil {
		return ov, ap
	}
	type r struct{ mac, ov, ap string }
	var all []r
	for rows.Next() {
		var x r
		if rows.Scan(&x.mac, &x.ov, &x.ap) == nil {
			all = append(all, x)
		}
	}
	rows.Close()
	for _, x := range all {
		ap[x.mac] = x.ap
		if x.ov != "" {
			if t, ok := ovFind(ts, x.ov); ok {
				ov[x.mac] = t
			}
		}
	}
	return ov, ap
}

// deviceHomeIface is the port or network a device is currently reached on, from its lease.
func (a *App) deviceHomeIface(mac string) string {
	ls, err := a.DHCP.Leases()
	if err != nil {
		return ""
	}
	var scope string
	for _, l := range ls {
		if strings.EqualFold(l.MAC, mac) {
			scope = l.Scope
		}
	}
	if scope == "" {
		return ""
	}
	var iface string
	a.Config.QueryRow(`SELECT iface FROM dhcp_scopes WHERE name=?`, scope).Scan(&iface)
	return iface
}

// overrideSync makes the rules in place match the devices that have an override. It reports a plain
// error if they could not be put in place; the devices then stay where they were.
func (a *App) overrideSync(force bool) error {
	if a.Net == nil {
		return nil
	}
	ts := a.ovTargets()
	rows, err := a.Config.Query(`SELECT lower(mac),override_iface FROM devices WHERE override_iface<>''`)
	if err != nil {
		return err
	}
	type pair struct{ mac, iface string }
	var ps []pair
	for rows.Next() {
		var p pair
		if rows.Scan(&p.mac, &p.iface) == nil {
			ps = append(ps, p)
		}
	}
	rows.Close()
	plan := netd.OverridePlan{}
	seen := map[string]bool{}
	for _, t := range ts {
		if !seen[t.Parent] {
			seen[t.Parent] = true
			plan.Parents = append(plan.Parents, t.Parent)
		}
	}
	for _, p := range ps {
		t, ok := ovFind(ts, p.iface)
		if !ok {
			continue // the network was removed; the device simply returns to its own
		}
		plan.Rules = append(plan.Rules, netd.OverrideRule{Parent: t.Parent, MAC: p.mac, VID: t.VID})
	}
	sort.Slice(plan.Rules, func(i, j int) bool { return plan.Rules[i].MAC < plan.Rules[j].MAC })
	sort.Strings(plan.Parents)
	h := sha256.Sum256([]byte(fmt.Sprintf("%v", plan)))
	st := &a.ov
	st.mu.Lock()
	defer st.mu.Unlock()
	if !force && st.have && st.hash == h && time.Since(st.at) < 10*time.Minute {
		return nil
	}
	if len(plan.Rules) == 0 && !st.have && !force {
		st.hash, st.have, st.at = h, true, time.Now()
		return nil // nothing was ever applied and nothing is wanted
	}
	if err := a.Net.OverrideApply(plan); err != nil {
		st.lastErr = err.Error()
		st.have = false
		return err
	}
	st.hash, st.have, st.at, st.lastErr = h, true, time.Now(), ""
	return nil
}

func (a *App) overrideLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second):
	}
	for {
		a.overrideSync(false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

// setOverride changes one device. An empty network returns it to its own network.
func (a *App) setOverride(id int64, iface string) error {
	var mac string
	if a.Config.QueryRow(`SELECT lower(mac) FROM devices WHERE id=?`, id).Scan(&mac) != nil {
		return errors.New("that device is not in the list")
	}
	if iface != "" {
		ts := a.ovTargets()
		t, ok := ovFind(ts, iface)
		if !ok {
			return errors.New("choose one of your VLAN networks (a network with a VLAN number)")
		}
		if home := a.deviceHomeIface(mac); home != "" && home != t.Parent {
			if h, ok := ovFind(ts, home); !ok || h.Parent != t.Parent {
				return fmt.Errorf("this device is on %s, but %s is on %s. Gatehouse can only place a device on a VLAN that is on the port the device is plugged into", home, t.Network, t.Parent)
			}
		}
	}
	prev := ""
	a.Config.QueryRow(`SELECT override_iface FROM devices WHERE id=?`, id).Scan(&prev)
	if _, err := a.Config.Exec(`UPDATE devices SET override_iface=? WHERE id=?`, iface, id); err != nil {
		return err
	}
	if err := a.overrideSync(true); err != nil {
		a.Config.Exec(`UPDATE devices SET override_iface=? WHERE id=?`, prev, id)
		a.overrideSync(true)
		return err
	}
	// Ask the device to ask again now, so it picks up an address on its new network.
	a.DHCP.ForgetMAC(mac)
	a.Devices.Rebuild()
	a.Bus.Publish("devices", map[string]int64{"at": time.Now().Unix()})
	return nil
}

// approvalHold is asked by the DHCP engine before it offers a new device an address.
func (a *App) approvalHold(mac, iface string) bool {
	if a.Config.Setting(approvalOn, "0") != "1" {
		return false
	}
	ts := a.ovTargets()
	hold, ok := ovFind(ts, a.Config.Setting(approvalHolding, ""))
	if !ok || iface == hold.Iface {
		return false
	}
	var ap, ov string
	err := a.Config.QueryRow(`SELECT approval,override_iface FROM devices WHERE mac=?`, mac).Scan(&ap, &ov)
	changed := false
	switch {
	case errors.Is(err, sql.ErrNoRows):
		now := time.Now().Unix()
		if _, e := a.Config.Exec(`INSERT OR IGNORE INTO devices(mac,name,first_seen,last_seen,override_iface,approval) VALUES(?,?,?,?,?,'pending')`, mac, "", now, now, hold.Iface); e != nil {
			return false
		}
		changed = true
		a.event("devices", "info", "A new device is waiting for approval", mac+" was put on the holding network "+hold.Network+".")
	case err != nil:
		return false
	case ap == "ok":
		return false
	case ov != hold.Iface:
		a.Config.Exec(`UPDATE devices SET override_iface=? WHERE mac=?`, hold.Iface, mac)
		changed = true
	}
	// Never leave a device without a network: after a few quiet answers, let it onto its own.
	st := &a.ov
	st.mu.Lock()
	if st.holds == nil {
		st.holds = map[string]int{}
	}
	st.holds[mac]++
	n := st.holds[mac]
	st.mu.Unlock()
	if n > 4 {
		return false
	}
	if changed {
		if err := a.overrideSync(true); err != nil {
			return false
		}
		a.Devices.Rebuild()
	}
	return true
}

type pendingRow struct {
	ID    int64  `json:"id"`
	MAC   string `json:"mac"`
	Name  string `json:"name"`
	State string `json:"state"`
	First int64  `json:"first_seen"`
	Maker string `json:"vendor"`
}

func (a *App) approvalStatus() map[string]any {
	var pend []pendingRow
	if rows, err := a.Config.Query(`SELECT id,mac,name,approval,first_seen FROM devices WHERE approval<>'ok' ORDER BY first_seen DESC`); err == nil {
		for rows.Next() {
			var p pendingRow
			if rows.Scan(&p.ID, &p.MAC, &p.Name, &p.State, &p.First) == nil {
				pend = append(pend, p)
			}
		}
		rows.Close()
	}
	for i := range pend {
		pend[i].Maker = devices.Vendor(pend[i].MAC)
	}
	if pend == nil {
		pend = []pendingRow{}
	}
	ts := a.ovTargets()
	if ts == nil {
		ts = []ovTarget{}
	}
	a.ov.mu.Lock()
	e := a.ov.lastErr
	a.ov.mu.Unlock()
	return map[string]any{"enabled": a.Config.Setting(approvalOn, "0") == "1", "holding": a.Config.Setting(approvalHolding, ""),
		"networks": ts, "pending": pend, "error": e}
}

func (a *App) registerOverrideRoutes() {
	s := a.API
	a.DHCP.Hold = a.approvalHold
	s.Handle("GET /api/approval", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.approvalStatus())
	})
	s.HandleAdmin("PUT /api/devices/{id}/override", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			api.Error(w, 404, "no such device")
			return
		}
		var in struct {
			Network string `json:"network"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.setOverride(id, in.Network); err != nil {
			api.Error(w, 409, err.Error())
			return
		}
		ok(w)
	})
	s.HandleAdmin("PUT /api/approval", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Enabled bool   `json:"enabled"`
			Holding string `json:"holding"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Enabled {
			t, good := ovFind(a.ovTargets(), in.Holding)
			if !good {
				api.Error(w, 409, "Choose the holding network first. It must be a network with a VLAN number, ideally of the Isolated type so waiting devices cannot reach anything.")
				return
			}
			var n int
			a.Config.QueryRow(`SELECT COUNT(*) FROM dhcp_scopes WHERE iface=? AND enabled=1 AND mode='serve'`, t.Iface).Scan(&n)
			if n == 0 {
				api.Error(w, 409, "The holding network has no address service (DHCP), so waiting devices would get no address. Turn DHCP on for it first.")
				return
			}
		}
		a.Config.SetSetting(approvalOn, map[bool]string{true: "1", false: "0"}[in.Enabled])
		a.Config.SetSetting(approvalHolding, in.Holding)
		ok(w)
	})
	decide := func(kind string) api.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
			id, good := pathID(r)
			if !good {
				api.Error(w, 404, "no such device")
				return
			}
			var in struct {
				Network string `json:"network"`
			}
			if kind == "place" && !api.Decode(w, r, &in) {
				return
			}
			holding := a.Config.Setting(approvalHolding, "")
			var cur string
			if a.Config.QueryRow(`SELECT override_iface FROM devices WHERE id=?`, id).Scan(&cur) != nil {
				api.Error(w, 404, "no such device")
				return
			}
			var target, state string
			switch kind {
			case "allow":
				state = "ok"
				if cur == holding {
					target = ""
				} else {
					target = cur
				}
			case "block":
				state, target = "blocked", holding
			case "place":
				if in.Network == "" {
					api.Error(w, 400, "choose a network")
					return
				}
				state, target = "ok", in.Network
			}
			a.Config.Exec(`UPDATE devices SET approval=? WHERE id=?`, state, id)
			if err := a.setOverride(id, target); err != nil {
				api.Error(w, 409, err.Error())
				return
			}
			ok(w)
		}
	}
	s.HandleAdmin("POST /api/approval/{id}/allow", decide("allow"))
	s.HandleAdmin("POST /api/approval/{id}/block", decide("block"))
	s.HandleAdmin("POST /api/approval/{id}/place", decide("place"))
}

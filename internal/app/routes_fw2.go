package app

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// chip is one thing a rule names, ready to show: a device, a group, a domain, a country or a plain address.
type chip struct {
	Kind   string `json:"kind"`   // any, device, group, domain, country, address, mac
	Label  string `json:"label"`  // the name people know it by
	Detail string `json:"detail"` // the addresses it stands for right now
	Ref    string `json:"ref"`    // the stored token
	Warn   string `json:"warn,omitempty"`
}

type ruleView struct {
	fw.RuleRow
	Tag      string   `json:"tag"`
	Hits     int64    `json:"hits"`
	Bytes    int64    `json:"bytes"`
	LastHit  int64    `json:"last_hit"`
	When     string   `json:"when"`
	SrcChips []chip   `json:"src_chips"`
	DstChips []chip   `json:"dst_chips"`
	Warnings []string `json:"warnings"`
	Matrix   bool     `json:"matrix"` // made by the matrix, not by hand
}

type nameBook struct {
	devName  map[string]string // mac -> name
	devHost  map[string]string
	leases   map[string][]string
	reserved map[string][]string
	groups   map[string]fw.DevGroup
}

func (a *App) nameBook() *nameBook {
	nb := &nameBook{devName: map[string]string{}, devHost: map[string]string{}, groups: map[string]fw.DevGroup{}}
	nb.leases, nb.reserved = a.addrIndex()
	if rows, err := a.Config.Query(`SELECT lower(mac),name FROM devices`); err == nil {
		for rows.Next() {
			var mac, name string
			rows.Scan(&mac, &name)
			nb.devName[mac] = name
		}
		rows.Close()
	}
	if rows, err := a.Config.Query(`SELECT lower(mac),hostname FROM dhcp_leases WHERE hostname<>''`); err == nil {
		for rows.Next() {
			var mac, h string
			rows.Scan(&mac, &h)
			nb.devHost[mac] = h
		}
		rows.Close()
	}
	if gs, err := fw.DeviceGroups(a.Config); err == nil {
		for _, g := range gs {
			nb.groups[g.Name] = fw.DevGroup{MACs: g.Members, ReservedOnly: g.ReservedOnly}
		}
	}
	return nb
}

func (nb *nameBook) deviceLabel(mac string) string {
	if n := nb.devName[mac]; n != "" {
		return n
	}
	if h := nb.devHost[mac]; h != "" {
		return h
	}
	return mac
}

// chips describes one address field of a rule.
func (nb *nameBook) chips(field string, isSrc bool) ([]chip, []string) {
	anyChip := chip{Kind: "any", Label: "Anywhere"}
	if isSrc {
		anyChip.Label = "Any device"
	}
	plain, macs, refs, err := fw.SplitAddrField(field)
	if err != nil || ((strings.TrimSpace(plain) == "" || strings.EqualFold(strings.TrimSpace(plain), "any")) && len(macs) == 0 && len(refs) == 0) {
		return []chip{anyChip}, nil
	}
	var out []chip
	var warns []string
	if p := strings.TrimSpace(plain); p != "" && !strings.EqualFold(p, "any") {
		for _, x := range strings.Split(p, ",") {
			out = append(out, chip{Kind: "address", Label: x, Ref: x})
		}
	}
	for _, m := range macs {
		out = append(out, chip{Kind: "mac", Label: m, Ref: m, Detail: "Device address"})
	}
	for _, r := range refs {
		c := chip{Ref: r.String()}
		switch r.Kind {
		case fw.RefDevice:
			c.Kind, c.Label = "device", nb.deviceLabel(r.Val)
			ips := deviceAddrs([]string{r.Val}, false, nb.leases, nb.reserved)
			c.Detail = strings.Join(ips, ", ")
			if len(ips) == 0 {
				c.Warn = "no address yet"
				warns = append(warns, c.Label+" has no address yet, so this rule matches nothing for it until it connects.")
			}
		case fw.RefGroup:
			g := nb.groups[r.Val]
			c.Kind, c.Label = "group", r.Val
			ips := deviceAddrs(g.MACs, g.ReservedOnly, nb.leases, nb.reserved)
			c.Detail = istr(len(g.MACs)) + " devices"
			if len(g.MACs) == 0 {
				c.Warn = "empty"
				warns = append(warns, "The group "+r.Val+" has no devices, so this rule matches nothing.")
			} else if g.ReservedOnly && len(ips) < len(g.MACs) {
				c.Warn = "some devices have no reserved address"
				warns = append(warns, "In "+r.Val+", only devices with a reserved address count; "+istr(len(g.MACs)-len(ips))+" do not have one.")
			}
		case fw.RefDomain:
			c.Kind, c.Label, c.Detail = "domain", r.Val, "and its subdomains"
		case fw.RefGeo:
			c.Kind, c.Label = "country", r.Val
		}
		out = append(out, c)
	}
	return out, warns
}

func (a *App) fwRulesView(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	rules, err := fw.Rules(a.Config)
	if err != nil {
		fail(w, err)
		return
	}
	hits := a.hitTable()
	nb := a.nameBook()
	out := make([]ruleView, 0, len(rules))
	for _, ru := range rules {
		v := ruleView{RuleRow: ru, Tag: fwTag(ru), When: ru.Sched.Describe(), Matrix: fw.IsMatrixRule(ru.Name)}
		h := hits[v.Tag]
		v.Hits, v.Bytes, v.LastHit = h.Hits, h.Bytes, h.LastHit
		var w1, w2 []string
		v.SrcChips, w1 = nb.chips(ru.Src, true)
		v.DstChips, w2 = nb.chips(ru.Dst, false)
		v.Warnings = append(w1, w2...)
		out = append(out, v)
	}
	api.JSON(w, 200, out)
}

func (a *App) registerFWObjectRoutes() {
	s := a.API
	// Everything the rule form needs to offer: devices (with their current address), groups,
	// saved port lists and the services Gatehouse knows by name.
	s.Handle("GET /api/fw/pickers", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		devs, _ := a.Devices.List()
		type dev struct {
			MAC    string   `json:"mac"`
			Name   string   `json:"name"`
			IP     string   `json:"ip"`
			Online bool     `json:"online"`
			Groups []string `json:"groups"`
		}
		groups, _ := fw.DeviceGroups(a.Config)
		memberOf := map[string][]string{}
		for _, g := range groups {
			for _, m := range g.Members {
				memberOf[m] = append(memberOf[m], g.Name)
			}
		}
		nb := a.nameBook()
		out := make([]dev, 0, len(devs))
		for _, d := range devs {
			mac := strings.ToLower(d.MAC)
			name := d.Name
			if name == "" {
				name = nb.deviceLabel(mac)
			}
			ip := d.IP
			if ips := deviceAddrs([]string{mac}, false, nb.leases, nb.reserved); len(ips) > 0 {
				ip = ips[0]
			}
			out = append(out, dev{MAC: mac, Name: name, IP: ip, Online: d.Online, Groups: memberOf[mac]})
		}
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
		lists, _ := fw.PortLists(a.Config)
		api.JSON(w, 200, map[string]any{"devices": out, "groups": groups, "port_lists": lists, "services": fw.ServiceNames(), "catalog": fw.BuiltinServices()})
	})

	s.HandleAdmin("PUT /api/fw/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in fw.GroupRow
		if !api.Decode(w, r, &in) {
			return
		}
		var id int64
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) (e error) { id, e = fw.SaveDeviceGroup(tx, in); return }); err != nil {
			fail(w, err)
			return
		}
		a.fwSync()
		a.markDirty()
		out := map[string]any{"id": id}
		if warn := a.vpnFollowGroup(in.Name, c.User.Username); warn != "" {
			out["vpn_warning"] = warn
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("DELETE /api/fw/groups/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.DeleteDeviceGroup(tx, id) }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		ok(w)
	})
	s.Handle("GET /api/fw/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		g, err := fw.DeviceGroups(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, g)
	})
	// Forced DNS: the router-wide switch, each network's treatment, and the device and group exceptions.
	s.Handle("GET /api/fw/dnsforce", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		zones, err := fw.Zones(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		targets := fw.ZoneDNSTargets(a.Config)
		type net struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Internet bool   `json:"internet"`
			Mode     string `json:"mode"` // force, server or off
			Target   string `json:"target"`
		}
		nets := []net{}
		for _, z := range zones {
			n := net{ID: z.ID, Name: z.Name, Internet: z.Internet, Mode: "off"}
			if z.DNSForce {
				n.Mode = "force"
				if t := targets[z.ID]; t != "" {
					n.Mode, n.Target = "server", t
				}
			}
			nets = append(nets, n)
		}
		ex := fw.DNSExceptions(a.Config)
		if ex == nil {
			ex = []fw.DNSException{}
		}
		var redirected, exempt int64
		for k, h := range a.hitTable() {
			switch {
			case strings.HasPrefix(k, "rule:dns:force:"):
				redirected += h.Hits
			case k == "rule:dns:exempt":
				exempt += h.Hits
			}
		}
		api.JSON(w, 200, map[string]any{
			"master": a.Config.Setting("fw.dns_redirect", "1") == "1", "target": strings.TrimSpace(a.Config.Setting("fw.dns_target", "")),
			"block_dot": a.Config.Setting("fw.block_dot", "1") == "1",
			"networks":  nets, "exceptions": ex, "redirected": redirected, "exempt": exempt,
		})
	})
	s.HandleAdmin("PUT /api/fw/dnsforce/network", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			ID     int64  `json:"id"`
			Mode   string `json:"mode"`
			Target string `json:"target"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Mode != "force" && in.Mode != "server" && in.Mode != "off" {
			fail(w, errors.New("choose force, another server, or leave alone"))
			return
		}
		if in.Mode == "server" && strings.TrimSpace(in.Target) == "" {
			fail(w, errors.New("enter the address of the DNS server for this network"))
			return
		}
		if in.Mode == "force" {
			in.Target = ""
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.SetZoneDNS(tx, in.ID, in.Mode != "off", in.Target) }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		ok(w)
	})
	s.HandleAdmin("PUT /api/fw/dnsforce/exception", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in fw.DNSException
		if !api.Decode(w, r, &in) {
			return
		}
		var id int64
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) (e error) { id, e = fw.SaveDNSException(tx, in); return }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/fw/dnsforce/exception/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.DeleteDNSException(tx, id) }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		ok(w)
	})
	// The services page: what Gatehouse knows by name, and the person's own, with where each is used.
	s.Handle("GET /api/fw/services", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		lists, err := fw.PortLists(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		type custom struct {
			fw.PortListRow
			UsedBy []string `json:"used_by"`
		}
		out := make([]custom, 0, len(lists))
		for _, l := range lists {
			used := fw.RulesUsingService(a.Config, l.Name)
			if used == nil {
				used = []string{}
			}
			out = append(out, custom{l, used})
		}
		api.JSON(w, 200, map[string]any{"builtin": fw.BuiltinServices(), "custom": out})
	})
	s.Handle("GET /api/fw/portlists", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		l, err := fw.PortLists(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, l)
	})
	s.HandleAdmin("PUT /api/fw/portlists", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in fw.PortListRow
		if !api.Decode(w, r, &in) {
			return
		}
		var id int64
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) (e error) { id, e = fw.SavePortList(tx, in); return }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/fw/portlists/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.DeletePortList(tx, id) }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		ok(w)
	})

	// Change how one pair of networks is treated, from the matrix.
	s.HandleAdmin("POST /api/fw/matrix", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			From string `json:"from"`
			To   string `json:"to"`
			Mode string `json:"mode"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		m, err := fw.Load(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.SetMatrix(tx, m.Zones, in.From, in.To, in.Mode) }); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Changed how "+in.From+" reaches "+in.To+" ("+in.Mode+")", "")
		a.markDirty()
		ok(w)
	})

	// Whether the country lists are ready.
	s.Handle("GET /api/fw/geo", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.geoEnsure()
		age, have := a.geoAge()
		res := map[string]any{"ready": have, "busy": a.dyn.geoBusy.Load()}
		if have {
			res["updated"] = time.Now().Add(-age).Unix()
		}
		if e, _ := a.dyn.geoErr.Load().(string); e != "" {
			res["error"] = e
		}
		api.JSON(w, 200, res)
	})
	s.Handle("POST /api/fw/geo/refresh", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.geoNow()
		ok(w)
	})
}

package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
	"gatehouse/internal/seed"
)

// Adding a network used to mean three pages: a port or VLAN with an address, a DHCP scope, and a
// firewall network. This does all three in one step, with plain choices and safe defaults.

var leaseChoices = map[string]int{"1h": 3600, "1d": 86400, "1w": 7 * 86400}

type addNetIn struct {
	Name     string `json:"name"`
	Port     string `json:"port"`    // the port the network is on
	VLAN     int    `json:"vlan"`    // 0 uses the whole port; otherwise a tagged network on it
	Address  string `json:"address"` // the router's address and size, for example 192.168.10.1/24; empty picks one
	DHCP     string `json:"dhcp"`    // server, off or relay
	Relay    string `json:"relay"`   // the DHCP server to pass requests to, for relay
	Internet bool   `json:"internet"`
	Isolate  bool   `json:"isolate"`
	Lease    string `json:"lease"` // 1h, 1d or 1w
	IPv6     string `json:"ipv6"`  // off or auto
}

var reAddNetPort = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,14}$`)

// usedNets lists every IPv4 network this machine already uses or is part of: its own ports,
// internet connections, DHCP scopes, VPNs, and whatever the ports have been given by other routers.
func (a *App) usedNets(q querier) []netip.Prefix {
	var out []netip.Prefix
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if p, err := netip.ParsePrefix(s); err == nil && p.Addr().Is4() {
			out = append(out, p.Masked())
		}
	}
	for _, c := range []struct{ table, col string }{
		{"interfaces", "ipv4_cidr"}, {"wan_links", "ipv4_addr"}, {"dhcp_scopes", "subnet"}, {"vpn_wg", "address"}, {"vpn_ovpn", "subnet"},
	} {
		rows, err := q.Query(`SELECT ` + c.col + ` FROM ` + c.table)
		if err != nil {
			continue
		}
		for rows.Next() {
			var s string
			rows.Scan(&s)
			add(s)
		}
		rows.Close()
	}
	if as, err := net.InterfaceAddrs(); err == nil {
		for _, ad := range as {
			add(ad.String())
		}
	}
	return out
}

func overlaps(p netip.Prefix, used []netip.Prefix) (netip.Prefix, bool) {
	for _, u := range used {
		if u.Overlaps(p) {
			return u, true
		}
	}
	return netip.Prefix{}, false
}

// suggestPrefix picks a /24 that nothing else uses: 192.168.10.0, 192.168.20.0 and so on first,
// then the rest of 192.168.x.0, then 10.x.0.0.
func suggestPrefix(used []netip.Prefix) (netip.Prefix, bool) {
	try := func(a, b, c byte) (netip.Prefix, bool) {
		p := netip.PrefixFrom(netip.AddrFrom4([4]byte{a, b, c, 0}), 24)
		_, clash := overlaps(p, used)
		return p, !clash
	}
	for n := 10; n < 255; n += 10 {
		if p, ok := try(192, 168, byte(n)); ok {
			return p, true
		}
	}
	for n := 2; n < 255; n++ {
		if p, ok := try(192, 168, byte(n)); ok {
			return p, true
		}
	}
	for n := 1; n < 255; n++ {
		if p, ok := try(10, byte(n), 0); ok {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// routerOf is the first address of a network, the usual place for the router.
func routerOf(p netip.Prefix) netip.Prefix {
	return netip.PrefixFrom(p.Masked().Addr().Next(), p.Bits())
}

type netSummary struct {
	Usable int    `json:"usable"`
	From   string `json:"range_start"`
	To     string `json:"range_end"`
	Text   string `json:"text"`
}

// summarise says in words how big a network is and which addresses devices will get.
func summarise(router netip.Prefix) netSummary {
	size := 1 << (32 - router.Bits())
	usable := size - 2
	s, e := seed.Range(router)
	return netSummary{Usable: usable, From: s.String(), To: e.String(),
		Text: fmt.Sprintf("%d usable addresses. Devices get %s to %s; the rest are left for devices you set by hand.", usable, s, e)}
}

// freePorts says which ports can carry a network of their own.
func (a *App) portUse(q querier) map[string]string {
	use := map[string]string{}
	if rows, err := q.Query(`SELECT iface FROM wan_links`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			use[n] = "internet"
		}
		rows.Close()
	}
	if rows, err := q.Query(`SELECT name,members,ipv4_cidr,COALESCE(zone_id,0),kind FROM interfaces`); err == nil {
		for rows.Next() {
			var n, mem, cidr, kind string
			var zone int64
			rows.Scan(&n, &mem, &cidr, &zone, &kind)
			for _, m := range strings.Split(mem, ",") {
				if m != "" {
					use[m] = "bridge"
				}
			}
			if kind != "vlan" && (cidr != "" || zone != 0) && use[n] == "" {
				use[n] = "network"
			}
		}
		rows.Close()
	}
	return use
}

func (in *addNetIn) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 40 {
		return errors.New("give the network a name, up to 40 characters")
	}
	if !reAddNetPort.MatchString(in.Port) {
		return errors.New("choose the port this network is on")
	}
	if in.VLAN < 0 || in.VLAN > 4094 {
		return errors.New("a VLAN number must be between 1 and 4094")
	}
	if in.DHCP == "" {
		in.DHCP = "server"
	}
	switch in.DHCP {
	case "server", "off":
	case "relay":
		if ip := net.ParseIP(strings.TrimSpace(in.Relay)).To4(); ip == nil {
			return errors.New("enter the address of the DHCP server to pass requests to")
		}
		in.Relay = strings.TrimSpace(in.Relay)
	default:
		return errors.New("DHCP must be server, off or relay")
	}
	if in.Lease == "" {
		in.Lease = "1d"
	}
	if _, ok := leaseChoices[in.Lease]; !ok {
		return errors.New("choose a lease time of 1 hour, 1 day or 1 week")
	}
	switch in.IPv6 {
	case "", "off", "auto":
	default:
		return errors.New("IPv6 must be off or automatic")
	}
	return nil
}

// addNetwork makes the port or VLAN, its DHCP scope and its firewall network together. It either
// all happens or none of it does.
func (a *App) addNetwork(in addNetIn) (int64, error) {
	if err := in.clean(); err != nil {
		return 0, err
	}
	tx, err := a.Config.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	use := a.portUse(tx)
	if in.VLAN == 0 && use[in.Port] != "" {
		switch use[in.Port] {
		case "internet":
			return 0, errors.New("that port is used for the internet; pick another port, or add a VLAN number to share it")
		case "bridge":
			return 0, errors.New("that port is part of a bridge; pick another port")
		}
		return 0, errors.New("that port already has a network; pick another port, or add a VLAN number to share it")
	}
	name := in.Port
	kind := "phys"
	if in.VLAN > 0 {
		name = in.Port + "." + strconv.Itoa(in.VLAN)
		kind = "vlan"
		if len(name) > 15 {
			return 0, errors.New("the port name is too long to add a VLAN to")
		}
		var n int
		tx.QueryRow(`SELECT COUNT(*) FROM interfaces WHERE name=?`, name).Scan(&n)
		if n > 0 {
			return 0, errors.New("that VLAN number is already used on this port")
		}
	}

	used := a.usedNets(tx)
	var router netip.Prefix
	if strings.TrimSpace(in.Address) == "" {
		p, ok := suggestPrefix(used)
		if !ok {
			return 0, errors.New("no unused address range was found; enter one by hand")
		}
		router = routerOf(p)
	} else {
		p, err := netip.ParsePrefix(strings.TrimSpace(in.Address))
		if err != nil || !p.Addr().Is4() {
			return 0, errors.New("the address must look like 192.168.10.1/24")
		}
		if p.Bits() < 16 || p.Bits() > 29 {
			return 0, errors.New("the network size must be between /16 and /29")
		}
		router = p
		if p.Addr() == p.Masked().Addr() {
			router = routerOf(p)
		}
		if u, clash := overlaps(router, used); clash {
			return 0, fmt.Errorf("%s overlaps %s, which is already in use. Leave the address empty to pick a free one", router.Masked(), u)
		}
	}
	subnet := router.Masked()

	// The firewall network, with the port assigned.
	kindZone := fw.KindPrivate
	if !in.Internet {
		kindZone = fw.KindIsolated
	}
	zid, err := fw.SaveZone(tx, fw.ZoneRow{Name: in.Name, Kind: kindZone, Ifaces: []string{name}, Internet: in.Internet,
		IntraAllow: true, Gateway: true, DNSForce: true})
	if err != nil {
		return 0, err
	}
	// The port or VLAN itself.
	_ = zid
	if _, err := tx.Exec(`UPDATE interfaces SET kind=?,parent=?,vlan_id=?,ipv4_cidr=?,ipv6_pd=?,enabled=1,dhcp=0 WHERE name=?`,
		kind, map[bool]string{true: in.Port}[in.VLAN > 0], in.VLAN, router.String(), b2iN(in.IPv6 == "auto"), name); err != nil {
		return 0, err
	}

	// DHCP.
	sc := scopeIn{Name: in.Name, Iface: name, Subnet: subnet.String(), Router: router.Addr().String(), DNS: router.Addr().String(),
		Domain: "lan", Lease: leaseChoices[in.Lease], Enabled: true}
	sm := summarise(router)
	switch in.DHCP {
	case "server":
		sc.Mode = "serve"
		sc.RangeStart, sc.RangeEnd = sm.From, sm.To
	case "relay":
		sc.Mode, sc.Relay = "relay", in.Relay
		sc.RangeStart, sc.RangeEnd = sm.From, sm.To
	default:
		sc.Mode = "off"
	}
	if err := sc.validate(); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO dhcp_scopes(name,iface,mode,subnet,range_start,range_end,router,dns,domain,ntp,lease_seconds,relay_addr,enabled) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,1)`,
		sc.Name, sc.Iface, sc.Mode, sc.Subnet, sc.RangeStart, sc.RangeEnd, sc.Router, sc.DNS, sc.Domain, "", sc.Lease, sc.Relay); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, errors.New("a DHCP scope with that name already exists")
		}
		return 0, err
	}

	// Not isolated: the other local networks and this one can reach each other.
	if !in.Isolate {
		m, err := fw.Load(tx)
		if err != nil {
			return 0, err
		}
		for _, z := range m.Zones {
			if z.Name == in.Name || z.Kind == fw.KindWan || z.Kind == fw.KindVpn {
				continue
			}
			if err := fw.SetMatrix(tx, m.Zones, in.Name, z.Name, fw.MatrixAllow); err != nil {
				return 0, err
			}
			if err := fw.SetMatrix(tx, m.Zones, z.Name, in.Name, fw.MatrixAllow); err != nil {
				return 0, err
			}
		}
	}

	// Both halves must still be valid together.
	p, err := a.buildPlan(tx)
	if err != nil {
		return 0, err
	}
	if err := a.validatePlan(p); err != nil {
		return 0, err
	}
	m, err := fw.Load(tx)
	if err != nil {
		return 0, err
	}
	if err := m.Validate(); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return zid, nil
}

func (a *App) registerAddNetRoutes() {
	s := a.API
	// Everything the form needs to start: ports that can carry a network, an unused address,
	// and the next free VLAN number.
	s.Handle("GET /api/networks/suggest", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		use := a.portUse(a.Config)
		pl := []map[string]any{}
		for _, p := range ports() {
			n := p["name"].(string)
			if p["wifi"] == true {
				continue // the Wi-Fi page sets up wireless networks
			}
			p["use"] = use[n]
			pl = append(pl, p)
		}
		used := a.usedNets(a.Config)
		out := map[string]any{"ports": pl}
		if p, ok := suggestPrefix(used); ok {
			rt := routerOf(p)
			out["address"] = rt.String()
			out["summary"] = summarise(rt)
		}
		taken := map[int]bool{}
		if rows, err := a.Config.Query(`SELECT vlan_id FROM interfaces WHERE vlan_id>0`); err == nil {
			for rows.Next() {
				var v int
				rows.Scan(&v)
				taken[v] = true
			}
			rows.Close()
		}
		v := 10
		for taken[v] {
			v += 10
		}
		out["vlan"] = v
		api.JSON(w, 200, out)
	})
	// A live summary while the address is being typed.
	s.Handle("GET /api/networks/check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, err := netip.ParsePrefix(strings.TrimSpace(r.URL.Query().Get("address")))
		if err != nil || !p.Addr().Is4() || p.Bits() < 16 || p.Bits() > 29 {
			api.JSON(w, 200, map[string]any{"ok": false, "problem": "The address must look like 192.168.10.1/24."})
			return
		}
		rt := p
		if p.Addr() == p.Masked().Addr() {
			rt = routerOf(p)
		}
		if u, clash := overlaps(rt, a.usedNets(a.Config)); clash {
			api.JSON(w, 200, map[string]any{"ok": false, "problem": fmt.Sprintf("This overlaps %s, which is already in use.", u)})
			return
		}
		api.JSON(w, 200, map[string]any{"ok": true, "router": rt.String(), "summary": summarise(rt)})
	})
	s.HandleAdmin("POST /api/networks/add", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in addNetIn
		if !api.Decode(w, r, &in) {
			return
		}
		id, err := a.addNetwork(in)
		if err != nil {
			fail(w, err)
			return
		}
		if a.DHCP != nil {
			a.DHCP.Reload()
		}
		a.event(c.User.Username, "info", "Added the network "+in.Name, "")
		a.publishNet()
		a.publishFW()
		a.publishLeases()
		select {
		case a.rogue.kick <- struct{}{}: // look for another DHCP server once the network is up
		default:
		}
		api.JSON(w, 200, map[string]int64{"id": id})
	})
}

var _ = sql.ErrNoRows

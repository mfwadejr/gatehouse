package fw

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Queryer is satisfied by *sql.DB, *sql.Tx and the store wrapper, so a model can be loaded
// from inside a transaction and checked before the transaction commits.
type Queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func setting(q Queryer, key, def string) string {
	rows, err := q.Query(`SELECT value FROM settings WHERE key=?`, key)
	if err != nil {
		return def
	}
	defer rows.Close()
	if rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			return v
		}
	}
	return def
}

// WanDevice is the interface a WAN link actually uses on the wire.
func WanDevice(iface, typ string, vlan int, name string) string {
	switch {
	case typ == "pppoe":
		return "ppp-" + sanitizeIf(name)
	case vlan > 0:
		return fmt.Sprintf("%s.%d", iface, vlan)
	}
	return iface
}

func sanitizeIf(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// Load builds the model from the configuration database.
func Load(db Queryer) (*Model, error) {
	m := &Model{Groups: map[string][]string{}, ConsolePorts: ConsolePortsFrom(db, time.Now())}
	m.RescueIface = rescueDevice(db, strings.TrimSpace(setting(db, "mgmt.rescue_iface", "")))
	m.MgmtAllow = MgmtAllowFrom(db).Resolved
	m.DNSRedirect = setting(db, "fw.dns_redirect", "1") == "1"
	m.BlockDoT = setting(db, "fw.block_dot", "1") == "1"
	m.DNSTarget = strings.TrimSpace(setting(db, "fw.dns_target", ""))
	m.LogDrops = setting(db, "fw.log_drops", "1") == "1"
	m.LogAllowed = setting(db, "fw.log_allowed", "1") == "1"

	rows, err := db.Query(`SELECT id,name,kind,gateway_mode,intra_allow,management,upnp,internet,ssh,dns_force,description FROM zones ORDER BY id`)
	if err != nil {
		return nil, err
	}
	byID := map[int64]int{}
	for rows.Next() {
		var z Zone
		var id int64
		var gw string
		var intra, mgmt, upnp, inet, ssh, dnsf int
		if err := rows.Scan(&id, &z.Name, &z.Kind, &gw, &intra, &mgmt, &upnp, &inet, &ssh, &dnsf, &z.Description); err != nil {
			rows.Close()
			return nil, err
		}
		z.Gateway, z.IntraAllow, z.Management, z.UPnP, z.Internet, z.SSH = gw == "gateway", intra == 1, mgmt == 1, upnp == 1, inet == 1, ssh == 1
		z.DNSForce = dnsf == 1
		byID[id] = len(m.Zones)
		m.Zones = append(m.Zones, z)
	}
	rows.Close()

	irows, err := db.Query(`SELECT name,zone_id FROM interfaces WHERE enabled=1 AND zone_id IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for irows.Next() {
		var name string
		var zid int64
		irows.Scan(&name, &zid)
		if i, ok := byID[zid]; ok {
			m.Zones[i].Ifaces = append(m.Zones[i].Ifaces, name)
		}
	}
	irows.Close()

	// VPN tunnels join the zone chosen for them, or the first VPN network.
	vpnZone := func(id sql.NullInt64) (int, bool) {
		if id.Valid {
			if i, ok := byID[id.Int64]; ok {
				return i, true
			}
		}
		for i := range m.Zones {
			if m.Zones[i].Kind == KindVpn {
				return i, true
			}
		}
		return 0, false
	}
	vrows, err := db.Query(`SELECT name,mode,listen_port,zone_id FROM vpn_wg WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for vrows.Next() {
		var name, mode string
		var port int
		var zid sql.NullInt64
		vrows.Scan(&name, &mode, &port, &zid)
		if i, ok := vpnZone(zid); ok {
			m.Zones[i].Ifaces = append(m.Zones[i].Ifaces, name)
		}
		if mode == "server" && port > 0 {
			m.VPNPorts = append(m.VPNPorts, VPNPort{Name: name, Proto: "udp", Port: port})
		}
	}
	vrows.Close()
	orows2, err := db.Query(`SELECT name,mode,proto,port,zone_id FROM vpn_ovpn WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for orows2.Next() {
		var name, mode, proto string
		var port int
		var zid sql.NullInt64
		orows2.Scan(&name, &mode, &proto, &port, &zid)
		if mode != "server" {
			continue
		}
		if i, ok := vpnZone(zid); ok {
			m.Zones[i].Ifaces = append(m.Zones[i].Ifaces, name)
		}
		m.VPNPorts = append(m.VPNPorts, VPNPort{Name: name, Proto: proto, Port: port})
	}
	orows2.Close()

	// Outbound tunnels, and who uses them.
	srcs, err := TunnelSources(db)
	if err != nil {
		return nil, err
	}
	trows, err := db.Query(`SELECT name,label,killswitch FROM vpn_wg WHERE mode='client' AND enabled=1
		UNION ALL SELECT name,label,killswitch FROM vpn_ovpn WHERE mode='client' AND enabled=1 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	for trows.Next() {
		var name, label string
		var ks int
		trows.Scan(&name, &label, &ks)
		if label == "" {
			label = name
		}
		m.Tunnels = append(m.Tunnels, Tunnel{Name: label, Iface: name, Sources: srcs[name], KillSwitch: ks == 1})
	}
	trows.Close()

	// WAN links form the internet-side zone.
	wan := Zone{Name: "Internet", Kind: KindWan, Gateway: true}
	wrows, err := db.Query(`SELECT name,iface,type,vlan_id FROM wan_links WHERE enabled=1 ORDER BY priority,id`)
	if err != nil {
		return nil, err
	}
	for wrows.Next() {
		var name, iface, typ string
		var vlan int
		wrows.Scan(&name, &iface, &typ, &vlan)
		wan.Ifaces = append(wan.Ifaces, WanDevice(iface, typ, vlan, name))
	}
	wrows.Close()
	// A stored zone of kind wan would collide with the generated one; the generated one wins.
	kept := m.Zones[:0]
	for _, z := range m.Zones {
		if z.Kind != KindWan {
			kept = append(kept, z)
		}
	}
	m.Zones = append(kept, wan)
	var upnpWans int
	if r, err := db.Query(`SELECT count(*) FROM wan_links WHERE enabled=1 AND upnp=1`); err == nil {
		if r.Next() {
			r.Scan(&upnpWans)
		}
		r.Close()
	}
	for _, z := range m.Zones {
		if z.UPnP && z.Kind != KindWan && upnpWans > 0 {
			m.UPnP = true
		}
	}

	rrows, err := db.Query(`SELECT name,from_zone,to_zone,src,dst,service,action,limit_rate,log,enabled,ipver,ctstate,sched,descr,also_mac FROM fw_rules ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var r Rule
		var lg, en, am int
		rrows.Scan(&r.Name, &r.From, &r.To, &r.Src, &r.Dst, &r.Service, &r.Action, &r.Limit, &lg, &en, &r.IPVer, &r.CtState, &r.Sched, &r.Descr, &am)
		r.Log, r.Enabled, r.AlsoMAC = lg == 1, en == 1, am == 1
		m.Rules = append(m.Rules, r)
	}
	rrows.Close()

	m.Now = time.Now()
	m.Loc = time.UTC
	if name := strings.TrimSpace(setting(db, "system.timezone", "")); name != "" {
		if l, err := time.LoadLocation(name); err == nil {
			m.Loc = l
		}
	}
	m.Devices = map[string]bool{}
	m.DevGroups = map[string]DevGroup{}
	m.PortLists = map[string][]string{}
	m.Names = map[string]string{}
	if d, err := db.Query(`SELECT lower(mac),name FROM devices`); err == nil {
		for d.Next() {
			var mac, name string
			d.Scan(&mac, &name)
			m.Devices[mac] = true
			if name != "" {
				m.Names[mac] = name
			}
		}
		d.Close()
	}
	if g, err := db.Query(`SELECT g.name,g.reserved_only,COALESCE(lower(x.mac),'') FROM device_groups g LEFT JOIN device_group_members x ON x.group_id=g.id ORDER BY g.id`); err == nil {
		for g.Next() {
			var name, mac string
			var ro int
			g.Scan(&name, &ro, &mac)
			dg := m.DevGroups[name]
			dg.ReservedOnly = ro == 1
			if mac != "" {
				dg.MACs = append(dg.MACs, mac)
			}
			m.DevGroups[name] = dg
		}
		g.Close()
	}
	// The far end of VPN connections is never matched by a region rule.
	if e, err := db.Query(`SELECT endpoint FROM vpn_wg_peers WHERE endpoint<>'' UNION SELECT endpoint_host FROM vpn_wg WHERE endpoint_host<>''`); err == nil {
		for e.Next() {
			var ep string
			e.Scan(&ep)
			host := ep
			if h, _, err := net.SplitHostPort(ep); err == nil {
				host = h
			}
			if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
				m.GeoExempt = append(m.GeoExempt, a.String())
			}
		}
		e.Close()
	}

	frows, err := db.Query(`SELECT name,wan,proto,ext_port,int_addr,int_port,enabled FROM nat_forwards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for frows.Next() {
		var f Forward
		var en int
		frows.Scan(&f.Name, &f.WAN, &f.Proto, &f.ExtPort, &f.IntAddr, &f.IntPort, &en)
		f.Enabled = en == 1
		m.Forwards = append(m.Forwards, f)
	}
	frows.Close()

	orows, err := db.Query(`SELECT name,kind,members FROM fw_objects WHERE kind IN ('address','ports')`)
	if err != nil {
		return nil, err
	}
	for orows.Next() {
		var name, kind, members string
		orows.Scan(&name, &kind, &members)
		var list []string
		if json.Unmarshal([]byte(members), &list) == nil {
			if kind == "ports" {
				m.PortLists[name] = list
			} else {
				m.Groups[name] = list
			}
		}
	}
	orows.Close()
	return m, nil
}

// RecoveryModel is the safest useful model derived from m: every non-internet network may
// manage the device, there are no exceptions or forwards, and the internet side stays closed.
func RecoveryModel(m *Model) *Model {
	r := &Model{Groups: map[string][]string{}, ConsolePorts: m.ConsolePorts, RescueIface: m.RescueIface}
	for _, z := range m.Zones {
		z2 := z
		z2.Ifaces = append([]string(nil), z.Ifaces...)
		if z.Kind != KindWan {
			z2.Management = true
			z2.SSH = true
		}
		r.Zones = append(r.Zones, z2)
	}
	return r
}

// ConsolePortsFrom lists the ports the console answers on: the saved one, plus a new one that is
// being tried and has not yet expired.
func ConsolePortsFrom(db Queryer, now time.Time) []int {
	ports := []int{DefaultConsolePorts[0]}
	if n, err := strconv.Atoi(strings.TrimSpace(setting(db, "console.port", ""))); err == nil && n >= 1 && n <= 65535 {
		ports[0] = n
	}
	if p, d, ok := strings.Cut(setting(db, "console.port.pending", ""), "|"); ok {
		pn, e1 := strconv.Atoi(p)
		dl, e2 := strconv.ParseInt(d, 10, 64)
		if e1 == nil && e2 == nil && pn >= 1 && pn <= 65535 && now.Unix() < dl && pn != ports[0] {
			ports = append(ports, pn)
		}
	}
	return ports
}

// MgmtAllowSpec is what the administrator chose: networks by name and address ranges by hand.
type MgmtAllowSpec struct {
	Networks []string `json:"networks"`
	CIDRs    []string `json:"cidrs"`
	// Resolved is every address range the choice stands for, filled in on load.
	Resolved []string `json:"resolved,omitempty"`
}

// MgmtAllowFrom reads the saved limit on who may manage this device.
func MgmtAllowFrom(db Queryer) MgmtAllowSpec {
	var spec MgmtAllowSpec
	raw := setting(db, "mgmt.allow", "")
	if raw == "" || json.Unmarshal([]byte(raw), &spec) != nil {
		return MgmtAllowSpec{}
	}
	spec.Resolved = nil
	seen := map[string]bool{}
	add := func(c string) {
		c = strings.TrimSpace(c)
		if p, err := netip.ParsePrefix(c); err == nil {
			c = p.Masked().String()
		} else if a, err := netip.ParseAddr(c); err == nil {
			c = netip.PrefixFrom(a, a.BitLen()).String()
		} else {
			return
		}
		if !seen[c] {
			seen[c] = true
			spec.Resolved = append(spec.Resolved, c)
		}
	}
	for _, c := range spec.CIDRs {
		add(c)
	}
	for _, n := range spec.Networks {
		rows, err := db.Query(`SELECT i.ipv4_cidr FROM interfaces i JOIN zones z ON z.id=i.zone_id WHERE i.enabled=1 AND i.ipv4_cidr<>'' AND z.name=?`, n)
		if err != nil {
			continue
		}
		for rows.Next() {
			var c string
			rows.Scan(&c)
			add(c)
		}
		rows.Close()
	}
	return spec
}

// rescueDevice returns the name traffic from the setup port arrives under. A port that has
// joined a bridge no longer shows up in the firewall by its own name, only as the bridge.
func rescueDevice(db Queryer, port string) string {
	if port == "" {
		return ""
	}
	rows, err := db.Query(`SELECT name,members FROM interfaces WHERE kind='bridge' AND enabled=1`)
	if err != nil {
		return port
	}
	defer rows.Close()
	for rows.Next() {
		var name, members string
		if rows.Scan(&name, &members) != nil {
			continue
		}
		for _, m := range strings.Split(members, ",") {
			if strings.TrimSpace(m) == port {
				return name
			}
		}
	}
	return port
}

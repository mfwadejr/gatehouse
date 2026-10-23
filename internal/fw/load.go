package fw

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
	m := &Model{Groups: map[string][]string{}, ConsolePorts: append([]int(nil), DefaultConsolePorts...)}
	m.DNSRedirect = setting(db, "fw.dns_redirect", "1") == "1"
	m.BlockDoT = setting(db, "fw.block_dot", "1") == "1"
	m.DNSTarget = strings.TrimSpace(setting(db, "fw.dns_target", ""))
	m.LogDrops = setting(db, "fw.log_drops", "0") == "1"

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

	rrows, err := db.Query(`SELECT name,from_zone,to_zone,src,dst,service,action,limit_rate,log,enabled FROM fw_rules ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var r Rule
		var lg, en int
		rrows.Scan(&r.Name, &r.From, &r.To, &r.Src, &r.Dst, &r.Service, &r.Action, &r.Limit, &lg, &en)
		r.Log, r.Enabled = lg == 1, en == 1
		m.Rules = append(m.Rules, r)
	}
	rrows.Close()

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

	orows, err := db.Query(`SELECT name,members FROM fw_objects WHERE kind='address'`)
	if err != nil {
		return nil, err
	}
	for orows.Next() {
		var name, members string
		orows.Scan(&name, &members)
		var list []string
		if json.Unmarshal([]byte(members), &list) == nil {
			m.Groups[name] = list
		}
	}
	orows.Close()
	return m, nil
}

// RecoveryModel is the safest useful model derived from m: every non-internet network may
// manage the device, there are no exceptions or forwards, and the internet side stays closed.
func RecoveryModel(m *Model) *Model {
	r := &Model{Groups: map[string][]string{}, ConsolePorts: m.ConsolePorts}
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

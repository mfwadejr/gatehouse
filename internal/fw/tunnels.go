package fw

import (
	"net"
	"net/netip"
	"strings"
)

// TunnelSources works out, for each outbound tunnel, which IPv4 prefixes use it. A network
// contributes the subnets of its ports, a device its reserved (or current) address, and a
// range is taken as typed. The firewall and the routing both read this, so they cannot
// disagree about who is in a tunnel.
func TunnelSources(db Queryer) (map[string][]string, error) {
	rows, err := db.Query(`SELECT tunnel,kind,value FROM vpn_routes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var rs []routeEntry
	for rows.Next() {
		var r routeEntry
		rows.Scan(&r.tunnel, &r.kind, &r.value)
		rs = append(rs, r)
	}
	rows.Close()
	return resolveSources(db, rs), nil
}

type routeEntry struct{ tunnel, kind, value string }

func resolveSources(db Queryer, rs []routeEntry) map[string][]string {
	out := map[string][]string{}
	add := func(t, p string) {
		for _, have := range out[t] {
			if have == p {
				return
			}
		}
		out[t] = append(out[t], p)
	}
	for _, r := range rs {
		switch r.kind {
		case "cidr":
			if p, err := netip.ParsePrefix(r.value); err == nil && p.Addr().Is4() {
				add(r.tunnel, p.Masked().String())
			} else if a, err := netip.ParseAddr(r.value); err == nil && a.Is4() {
				add(r.tunnel, a.String()+"/32")
			}
		case "network":
			ir, err := db.Query(`SELECT ipv4_cidr FROM interfaces WHERE enabled=1 AND ipv4_cidr<>'' AND zone_id=(SELECT id FROM zones WHERE name=?)`, r.value)
			if err != nil {
				continue
			}
			for ir.Next() {
				var c string
				ir.Scan(&c)
				if p, err := netip.ParsePrefix(strings.TrimSpace(c)); err == nil && p.Addr().Is4() {
					add(r.tunnel, p.Masked().String())
				}
			}
			ir.Close()
		case "device":
			if a := deviceIP(db, r.value); a != "" {
				add(r.tunnel, a)
			}
		case "group":
			// every device in a device group, as they are today
			gr, err := db.Query(`SELECT m.mac FROM device_group_members m JOIN device_groups g ON g.id=m.group_id WHERE g.name=?`, r.value)
			if err != nil {
				continue
			}
			var macs []string
			for gr.Next() {
				var m string
				gr.Scan(&m)
				macs = append(macs, m)
			}
			gr.Close()
			for _, m := range macs {
				if a := deviceIP(db, m); a != "" {
					add(r.tunnel, a)
				}
			}
		}
	}
	return out
}

// deviceIP is the address a device has or is promised, as a /32, or "" when it has none.
func deviceIP(db Queryer, macIn string) string {
	mac := strings.ToLower(macIn)
	ip := firstString(db, `SELECT ip FROM dhcp_reservations WHERE lower(mac)=? LIMIT 1`, mac)
	if ip == "" {
		ip = firstString(db, `SELECT ip FROM dhcp_leases WHERE lower(mac)=? AND state='active' ORDER BY expires DESC LIMIT 1`, mac)
	}
	if a := net.ParseIP(ip); a != nil && a.To4() != nil {
		return a.To4().String() + "/32"
	}
	return ""
}

func firstString(db Queryer, query string, arg any) string {
	rows, err := db.Query(query, arg)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var v string
	if rows.Next() {
		rows.Scan(&v)
	}
	return v
}

// TunnelSourcesFor lists the prefixes one entry (a network, device or range) stands for today.
func TunnelSourcesFor(db Queryer, kind, value string) ([]string, error) {
	return resolveSources(db, []routeEntry{{"", kind, value}})[""], nil
}

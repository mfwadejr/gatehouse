package fw

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A route sends chosen devices or networks through a VPN tunnel or one internet connection,
// either for everything or only for chosen destinations. The firewall marks the traffic of a
// route in the first step of its journey; the routing tables then send marked traffic where the
// route says (tunnels through their own table, connections through the table that connection
// already has).

// RouteUse is who uses a route: a network, a device, a device group or an address range.
type RouteUse struct {
	Kind  string `json:"kind"` // network, device, group, cidr
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

// RouteDest is where a route applies. No destinations at all means everything.
type RouteDest struct {
	Kind  string `json:"kind"` // ip, domain, region
	Value string `json:"value"`
}

// RouteRow is a route as the console edits it.
type RouteRow struct {
	ID         int64       `json:"id"`
	Name       string      `json:"name"`
	Enabled    bool        `json:"enabled"`
	TargetKind string      `json:"target_kind"` // tunnel or wan
	Target     string      `json:"target"`      // wg1, tun1 or the connection's name
	Kill       bool        `json:"kill"`
	Uses       []RouteUse  `json:"uses"`
	Dests      []RouteDest `json:"dests"`
}

// RouteSpec is what the firewall and the routing need to know about one working route.
type RouteSpec struct {
	Name        string
	Mark        int
	TunnelIface string   // the tunnel device, empty for a connection
	Srcs        []string // IPv4 prefixes that use the route
	Dst         string   // address field: plain ranges, dom:x and geo:XX
	Kill        bool
}

// Marks for tunnel routes start here. Connections use MarkBase plus their own number.
const RouteMarkBase = 0x1000

// MaxRoutes keeps the policy rules inside their reserved priority range.
const MaxRoutes = 16

// Routes lists the stored routes in the order they are tried.
func Routes(db Queryer) ([]RouteRow, error) {
	rows, err := db.Query(`SELECT id,name,enabled,target_kind,target,kill,srcs,dests FROM routes ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RouteRow{}
	for rows.Next() {
		var r RouteRow
		var en, kl int
		var srcs, dests string
		if err := rows.Scan(&r.ID, &r.Name, &en, &r.TargetKind, &r.Target, &kl, &srcs, &dests); err != nil {
			return nil, err
		}
		r.Enabled, r.Kill = en == 1, kl == 1
		r.Uses, r.Dests = []RouteUse{}, []RouteDest{}
		json.Unmarshal([]byte(srcs), &r.Uses)
		json.Unmarshal([]byte(dests), &r.Dests)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CleanRoute checks and tidies a route before it is saved. The messages are shown as they are.
func CleanRoute(r RouteRow) (RouteRow, error) {
	r.Name = strings.TrimSpace(r.Name)
	if !reName.MatchString(r.Name) {
		return r, errors.New("Give the route a short name, using letters, numbers and spaces")
	}
	if r.TargetKind != "tunnel" && r.TargetKind != "wan" {
		return r, errors.New("Choose a VPN or an internet connection for this route")
	}
	if strings.TrimSpace(r.Target) == "" {
		return r, errors.New("Choose where this route sends traffic")
	}
	if r.TargetKind == "wan" {
		r.Kill = false
	}
	var uses []RouteUse
	for _, u := range r.Uses {
		u.Value = strings.TrimSpace(u.Value)
		switch u.Kind {
		case "network", "device", "group", "cidr":
		default:
			return r, fmt.Errorf("%q is not something a route can cover", u.Kind)
		}
		if u.Value == "" {
			continue
		}
		if u.Kind == "device" {
			u.Value = strings.ToLower(u.Value)
			if !reMAC.MatchString(u.Value) {
				return r, fmt.Errorf("%q is not a device address", u.Value)
			}
		}
		uses = append(uses, u)
	}
	r.Uses = uses
	var dests []RouteDest
	for _, d := range r.Dests {
		d.Value = strings.TrimSpace(d.Value)
		if d.Value == "" {
			continue
		}
		switch d.Kind {
		case "ip":
			if _, _, err := parseAddrs(d.Value, nil, 0); err != nil {
				return r, fmt.Errorf("%q is not an address or range: %v", d.Value, err)
			}
		case "domain":
			v := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(d.Value), "*."), ".")
			if i := strings.Index(v, "://"); i >= 0 {
				v = v[i+3:]
				if j := strings.IndexAny(v, "/?#:"); j >= 0 {
					v = v[:j]
				}
			}
			if !reDomain.MatchString(v) {
				return r, fmt.Errorf("%q is not a website name. Enter something like example.com", d.Value)
			}
			d.Value = v
		case "region":
			d.Value = strings.ToUpper(d.Value)
			if !reCC.MatchString(d.Value) {
				return r, fmt.Errorf("%q is not a country", d.Value)
			}
		default:
			return r, fmt.Errorf("%q is not a destination type", d.Kind)
		}
		dests = append(dests, d)
	}
	r.Dests = dests
	return r, nil
}

// SaveRoute adds or changes a route.
func SaveRoute(tx *sql.Tx, r RouteRow) (int64, error) {
	r, err := CleanRoute(r)
	if err != nil {
		return 0, err
	}
	srcs, _ := json.Marshal(r.Uses)
	dests, _ := json.Marshal(r.Dests)
	if r.ID == 0 {
		var pos, n int
		tx.QueryRow(`SELECT COALESCE(MAX(position),0)+1, COUNT(*) FROM routes`).Scan(&pos, &n)
		if n >= MaxRoutes {
			return 0, fmt.Errorf("Gatehouse supports up to %d routes", MaxRoutes)
		}
		res, err := tx.Exec(`INSERT INTO routes(name,enabled,target_kind,target,kill,srcs,dests,position) VALUES(?,?,?,?,?,?,?,?)`,
			r.Name, b2i(r.Enabled), r.TargetKind, r.Target, b2i(r.Kill), string(srcs), string(dests), pos)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return 0, errors.New("A route with that name already exists")
			}
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err = tx.Exec(`UPDATE routes SET name=?,enabled=?,target_kind=?,target=?,kill=?,srcs=?,dests=? WHERE id=?`,
		r.Name, b2i(r.Enabled), r.TargetKind, r.Target, b2i(r.Kill), string(srcs), string(dests), r.ID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return 0, errors.New("A route with that name already exists")
	}
	return r.ID, err
}

// DeleteRoute removes a route.
func DeleteRoute(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`DELETE FROM routes WHERE id=?`, id)
	return err
}

// MoveRoutes reorders by assigning positions from the given id order.
func MoveRoutes(tx *sql.Tx, order []int64) error {
	for i, id := range order {
		if _, err := tx.Exec(`UPDATE routes SET position=? WHERE id=?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}

// RouteSpecs works out the routes that can work right now: enabled, with someone to carry, and
// with their VPN or connection switched on. Marks follow the order, so the firewall and the
// routing tables always agree.
func RouteSpecs(db Queryer) ([]RouteSpec, error) {
	rows, err := Routes(db)
	if err != nil {
		return nil, err
	}
	tunnels := map[string]bool{}
	if tr, err := db.Query(`SELECT name FROM vpn_wg WHERE mode='client' AND enabled=1 UNION SELECT name FROM vpn_ovpn WHERE mode='client' AND enabled=1`); err == nil {
		for tr.Next() {
			var n string
			tr.Scan(&n)
			tunnels[n] = true
		}
		tr.Close()
	}
	var wans []string
	if wr, err := db.Query(`SELECT name FROM wan_links WHERE enabled=1 ORDER BY priority,id`); err == nil {
		for wr.Next() {
			var n string
			wr.Scan(&n)
			wans = append(wans, n)
		}
		wr.Close()
	}
	var out []RouteSpec
	for i, r := range rows {
		if !r.Enabled || len(r.Uses) == 0 {
			continue
		}
		var entries []routeEntry
		for _, u := range r.Uses {
			entries = append(entries, routeEntry{"r", u.Kind, u.Value})
		}
		srcs := resolveSources(db, entries)["r"]
		if len(srcs) == 0 {
			continue
		}
		spec := RouteSpec{Name: r.Name, Srcs: srcs, Kill: r.Kill}
		switch r.TargetKind {
		case "tunnel":
			if !tunnels[r.Target] {
				continue
			}
			spec.TunnelIface = r.Target
			spec.Mark = RouteMarkBase + i
		case "wan":
			idx := -1
			for j, n := range wans {
				if n == r.Target {
					idx = j
				}
			}
			if idx < 0 || len(wans) < 2 {
				continue // with one connection there is nothing to choose between
			}
			spec.Mark = MarkBase + idx
		}
		var dst []string
		for _, d := range r.Dests {
			switch d.Kind {
			case "ip":
				dst = append(dst, strings.Fields(strings.ReplaceAll(d.Value, ",", " "))...)
			case "domain":
				dst = append(dst, "dom:"+d.Value)
			case "region":
				dst = append(dst, "geo:"+d.Value)
			}
		}
		spec.Dst = strings.Join(dst, ",")
		out = append(out, spec)
	}
	return out, nil
}

// RouteMarksFor lists, for one tunnel device, the marks whose traffic its table carries.
func RouteMarksFor(specs []RouteSpec, dev string) []int {
	var out []int
	for _, s := range specs {
		if s.TunnelIface == dev {
			out = append(out, s.Mark)
		}
	}
	return out
}

func (c *compiler) routeMarks() {
	m := c.m
	if len(m.Routes) == 0 {
		return
	}
	locals := c.clientIfaces()
	if len(locals) == 0 {
		return
	}
	c.line(1, "chain routemark {")
	c.line(2, "type filter hook prerouting priority -151; policy accept;")
	tunnel := false
	for _, r := range m.Routes {
		dstT := []term{{0, ""}}
		if strings.TrimSpace(r.Dst) != "" {
			dstT = nil
			for _, t := range c.sideTerms(r.Dst, false, false) {
				if t.fam != 6 { // routes carry IPv4 only
					dstT = append(dstT, t)
				}
			}
		}
		for _, t := range dstT {
			e := ""
			if t.expr != "" {
				e = " " + t.expr
			}
			c.line(2, "iifname %s ip saddr %s%s ct state new meta mark 0 meta mark set 0x%x ct mark set 0x%x comment %s",
				ifSet(locals), setLit(r.Srcs), e, r.Mark, r.Mark, q("route:"+r.Name))
		}
		if r.TunnelIface != "" {
			tunnel = true
		}
	}
	if tunnel {
		// Later packets of a connection keep the route their first packet got.
		c.line(2, "iifname %s ct mark >= 0x%x meta mark set ct mark", ifSet(locals), RouteMarkBase)
	}
	c.line(1, "}")
}

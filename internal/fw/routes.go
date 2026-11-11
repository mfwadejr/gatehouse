package fw

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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
	// Who, Where and Service are the same words the firewall rule form uses: dev:MAC, grp:Name,
	// dom:site, geo:CC, addresses and ranges, and for Who also net:Name for a whole network. They
	// say the same as Uses and Dests, which are kept so routes made before keep working.
	Who     string `json:"who"`
	Where   string `json:"where"`
	Service string `json:"service"`
	// Mode says how a route that lists several connections (target_kind links, target a comma-
	// separated list in the order to try them) uses them: failover, or balance.
	Mode        string    `json:"mode"`
	Sched       *Schedule `json:"sched"`        // when the route is in force; nil means always
	PausedUntil int64     `json:"paused_until"` // unix seconds; the route is off until then
}

// RouteSpec is what the firewall and the routing need to know about one working route.
type RouteSpec struct {
	Name        string
	Mark        int
	TunnelIface string   // the tunnel device, empty for a connection
	Srcs        []string // IPv4 prefixes that use the route
	Dst         string   // address field: plain ranges, dom:x and geo:XX
	Service     string
	Sched       *Schedule
	PausedUntil int64
	Kill        bool
}

// RouteLinksMark is the mark of the route at a place in the list that keeps its own list of
// connections. These follow the marks of the named groups (up to MaxGroups of them).
func RouteLinksMark(i int) int { return GroupMarkBase + MaxGroups + i }

// MaxGroups is how many named connection groups there can be.
const MaxGroups = 8

// Marks for routes that use a connection group start here (one per group, by its place in the list).
const GroupMarkBase = 0x1100

// Marks for tunnel routes start here. Connections use MarkBase plus their own number.
const RouteMarkBase = 0x1000

// MaxRoutes keeps the policy rules inside their reserved priority range.
const MaxRoutes = 16

// Routes lists the stored routes in the order they are tried.
func Routes(db Queryer) ([]RouteRow, error) {
	rows, err := db.Query(`SELECT id,name,enabled,target_kind,target,kill,srcs,dests,service,sched,paused_until,mode FROM routes ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RouteRow{}
	for rows.Next() {
		var r RouteRow
		var en, kl int
		var srcs, dests, sched string
		if err := rows.Scan(&r.ID, &r.Name, &en, &r.TargetKind, &r.Target, &kl, &srcs, &dests, &r.Service, &sched, &r.PausedUntil, &r.Mode); err != nil {
			return nil, err
		}
		r.Enabled, r.Kill = en == 1, kl == 1
		r.Uses, r.Dests = []RouteUse{}, []RouteDest{}
		json.Unmarshal([]byte(srcs), &r.Uses)
		json.Unmarshal([]byte(dests), &r.Dests)
		r.Sched, _ = ParseSchedule(sched)
		r.Who, r.Where = usesToWho(r.Uses), destsToWhere(r.Dests)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CleanRoute checks and tidies a route before it is saved. The messages are shown as they are.
func CleanRoute(r RouteRow) (RouteRow, error) {
	// The picker's words and the older lists say the same thing; whichever came is used.
	if strings.TrimSpace(r.Who) != "" {
		r.Uses = whoToUses(r.Who)
	}
	if strings.TrimSpace(r.Where) != "" {
		r.Dests = whereToDests(r.Where)
	} else if r.Who != "" && len(r.Dests) > 0 && r.Where == "" {
		// "Anywhere" was chosen on a route that used to have places: clear them.
		r.Dests = nil
	}
	r.Name = strings.TrimSpace(r.Name)
	if !reName.MatchString(r.Name) {
		return r, errors.New("Give the route a short name, using letters, numbers and spaces")
	}
	if r.TargetKind != "tunnel" && r.TargetKind != "wan" && r.TargetKind != "group" && r.TargetKind != "links" {
		return r, errors.New("Choose a VPN, internet connections or a connection group for this route")
	}
	if strings.TrimSpace(r.Target) == "" {
		return r, errors.New("Choose where this route sends traffic")
	}
	if r.TargetKind != "tunnel" {
		r.Kill = false
	}
	if r.TargetKind == "links" {
		seen := map[string]bool{}
		var names []string
		for _, n := range strings.Split(r.Target, ",") {
			n = strings.TrimSpace(n)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			names = append(names, n)
		}
		if len(names) == 0 {
			return r, errors.New("Choose at least one internet connection for this route")
		}
		r.Target = strings.Join(names, ",")
		if r.Mode != "balance" {
			r.Mode = "failover"
		}
	} else {
		r.Mode = "failover"
	}
	r.Service = strings.Join(strings.FieldsFunc(r.Service, func(c rune) bool { return c == ',' || c == ' ' || c == '\n' }), ",")
	for _, part := range strings.Split(r.Service, ",") {
		if part == "" || strings.HasPrefix(part, "list:") {
			continue // the saved lists are checked where they are known
		}
		if _, err := ParseService(part); err != nil {
			return r, fmt.Errorf("%q is not a service or port. Write a port as tcp/563 or udp/5000-5010", part)
		}
	}
	if r.Sched != nil && (r.Sched.Mode == "" || r.Sched.Mode == "always") {
		r.Sched = nil
	}
	if r.Sched != nil {
		if err := r.Sched.Check(time.UTC); err != nil {
			return r, err
		}
	}
	if r.PausedUntil < 0 {
		r.PausedUntil = 0
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
	sched := ""
	if r.Sched != nil {
		b, _ := json.Marshal(r.Sched)
		sched = string(b)
	}
	if r.ID == 0 {
		var pos, n int
		tx.QueryRow(`SELECT COALESCE(MAX(position),0)+1, COUNT(*) FROM routes`).Scan(&pos, &n)
		if n >= MaxRoutes {
			return 0, fmt.Errorf("NopeWarden supports up to %d routes", MaxRoutes)
		}
		res, err := tx.Exec(`INSERT INTO routes(name,enabled,target_kind,target,kill,srcs,dests,service,sched,paused_until,mode,position) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.Name, b2i(r.Enabled), r.TargetKind, r.Target, b2i(r.Kill), string(srcs), string(dests), r.Service, sched, r.PausedUntil, r.Mode, pos)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return 0, errors.New("A route with that name already exists")
			}
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err = tx.Exec(`UPDATE routes SET name=?,enabled=?,target_kind=?,target=?,kill=?,srcs=?,dests=?,service=?,sched=?,paused_until=?,mode=? WHERE id=?`,
		r.Name, b2i(r.Enabled), r.TargetKind, r.Target, b2i(r.Kill), string(srcs), string(dests), r.Service, sched, r.PausedUntil, r.Mode, r.ID)
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
	var groups []string
	if gr, err := db.Query(`SELECT name FROM wan_groups ORDER BY position,id`); err == nil {
		for gr.Next() {
			var n string
			gr.Scan(&n)
			groups = append(groups, n)
		}
		gr.Close()
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
		spec := RouteSpec{Name: r.Name, Srcs: srcs, Kill: r.Kill, Service: r.Service, Sched: r.Sched, PausedUntil: r.PausedUntil}
		switch r.TargetKind {
		case "links":
			have := false
			for _, n := range strings.Split(r.Target, ",") {
				for _, w := range wans {
					if w == n {
						have = true
					}
				}
			}
			if !have {
				continue
			}
			spec.Mark = RouteLinksMark(i)
		case "group":
			gi := -1
			for j, n := range groups {
				if n == r.Target {
					gi = j
				}
			}
			if gi < 0 {
				continue
			}
			spec.Mark = GroupMarkBase + gi
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
	loc, now := m.Loc, m.Now
	if loc == nil {
		loc = time.UTC
	}
	if now.IsZero() {
		now = time.Now()
	}
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
			// A pause is a clock test the firewall makes itself: the route starts again at that
			// moment with nothing to apply. A schedule works the same way.
			hold := ""
			if r.PausedUntil > now.Unix() {
				hold = fmt.Sprintf(" meta time >= %d", r.PausedUntil)
			}
			for _, tc := range r.Sched.Clauses(loc, now) {
				if tc != "" {
					tc = " " + tc
				}
				for _, sv := range c.serviceTerms(r.Service) {
					c.line(2, "iifname %s ip saddr %s%s%s%s%s ct state new meta mark 0 meta mark set 0x%x ct mark set ct mark & 0x%08x | 0x%x comment %s",
						ifSet(locals), setLit(r.Srcs), e, sv, tc, hold, r.Mark, ConnMarkDSCP, r.Mark, q("route:"+r.Name))
				}
			}
		}
		if r.TunnelIface != "" || r.Mark >= GroupMarkBase {
			tunnel = true
		}
	}
	if tunnel {
		// Later packets of a connection keep the route their first packet got.
		c.line(2, "iifname %s ct mark & 0x%06x >= 0x%x meta mark set ct mark & 0x%06x", ifSet(locals), ConnMarkKeep, RouteMarkBase, ConnMarkKeep)
	}
	c.line(1, "}")
}

// serviceTerms turns a route's service words into firewall matches, one per protocol and port
// range. No service words gives a single empty match, which is everything.
func (c *compiler) serviceTerms(service string) []string {
	if len(MissingServiceLists(service, c.m.PortLists)) > 0 {
		// A list that no longer exists must never turn into "everything": the route sends nothing.
		return nil
	}
	specs, _ := ParseServices(service, c.m.PortLists)
	if len(specs) == 0 {
		return []string{""}
	}
	var out []string
	for _, sp := range specs {
		if sp.Proto == "" {
			return []string{""}
		}
		if sp.Proto == "icmp" {
			continue // routes carry IPv4 traffic by address, not echo requests
		}
		out = append(out, " "+sp.matchExpr())
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// usesToWho and whoToUses translate between the stored uses and the words the picker shows.
func usesToWho(us []RouteUse) string {
	var out []string
	for _, u := range us {
		switch u.Kind {
		case "network":
			out = append(out, "net:"+u.Value)
		case "device":
			out = append(out, "dev:"+u.Value)
		case "group":
			out = append(out, "grp:"+u.Value)
		default:
			out = append(out, u.Value)
		}
	}
	return strings.Join(out, ",")
}

func whoToUses(who string) []RouteUse {
	var out []RouteUse
	for _, t := range strings.FieldsFunc(who, func(c rune) bool { return c == ',' || c == ' ' || c == '\n' }) {
		switch {
		case strings.HasPrefix(t, "net:"):
			out = append(out, RouteUse{Kind: "network", Value: t[4:]})
		case strings.HasPrefix(t, "dev:"):
			out = append(out, RouteUse{Kind: "device", Value: strings.ToLower(t[4:])})
		case strings.HasPrefix(t, "grp:"):
			out = append(out, RouteUse{Kind: "group", Value: t[4:]})
		case reMAC.MatchString(t):
			out = append(out, RouteUse{Kind: "device", Value: strings.ToLower(t)})
		case strings.EqualFold(t, "any"):
		default:
			out = append(out, RouteUse{Kind: "cidr", Value: t})
		}
	}
	return out
}

func destsToWhere(ds []RouteDest) string {
	var out []string
	for _, d := range ds {
		switch d.Kind {
		case "domain":
			out = append(out, "dom:"+d.Value)
		case "region":
			out = append(out, "geo:"+d.Value)
		default:
			out = append(out, d.Value)
		}
	}
	return strings.Join(out, ",")
}

func whereToDests(where string) []RouteDest {
	var out []RouteDest
	for _, t := range strings.FieldsFunc(where, func(c rune) bool { return c == ',' || c == ' ' || c == '\n' }) {
		switch {
		case strings.HasPrefix(t, "dom:"):
			out = append(out, RouteDest{Kind: "domain", Value: t[4:]})
		case strings.HasPrefix(t, "geo:"):
			out = append(out, RouteDest{Kind: "region", Value: t[4:]})
		case strings.EqualFold(t, "any"):
		default:
			out = append(out, RouteDest{Kind: "ip", Value: t})
		}
	}
	return out
}

// MissingServiceLists names the saved lists a "what for" field points at that do not exist.
func MissingServiceLists(service string, lists map[string][]string) []string {
	var out []string
	for _, part := range strings.Split(service, ",") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(part), "list:"); ok {
			if _, found := lists[name]; !found {
				out = append(out, name)
			}
		}
	}
	return out
}

package wanmon

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
)

// Group sends chosen sources through a chosen set of connections instead of the default
// choice. It has its own routing table with its own default route, and policy rules that
// point the sources at that table.
type Group struct {
	Name    string
	Table   int // routing table holding the group's default route
	Prio    int // priority of the source rules
	Mode    Mode
	Block   bool // when no member works: cut the sources off instead of using the default route
	Members []Member
	Sources []string // IPv4 prefixes
}

// Member is one connection in a group, in failover order.
type Member struct {
	Link   string
	Weight int
}

// GroupRouter is implemented by routers that can carry groups.
type GroupRouter interface {
	SetTable(table int, nh []Nexthop, block bool) error // empty nh clears the table
	SyncRules(g Group) error
	Prune(keep []Group) error // remove rules and tables of groups that no longer exist
}

// Group table and rule priority bounds. Rules sit after the per-connection mark rules
// (1000+table) and before the main table, so replies to inbound connections keep leaving
// the way they arrived.
const (
	GroupTableMin = 320
	GroupTableMax = 327
	GroupPrioMin  = 1300
	GroupPrioMax  = 1307
)

// GroupPlan chooses the nexthops for a group: working members only, in the order given.
func GroupPlan(g Group, links []Link, st map[string]*State) []Nexthop {
	by := map[string]Link{}
	for _, l := range links {
		by[l.Name] = l
	}
	var up []Nexthop
	for _, m := range g.Members {
		l, ok := by[m.Link]
		s := st[m.Link]
		if !ok || s == nil || !s.Up || s.Gateway == "" {
			continue
		}
		w := m.Weight
		if w < 1 {
			w = 1
		}
		up = append(up, Nexthop{Dev: l.Dev, Gateway: s.Gateway, Weight: w})
	}
	if len(up) == 0 {
		return nil
	}
	if g.Mode != Balance {
		up = up[:1]
		up[0].Weight = 1
	}
	return up
}

// ValidateGroups checks the parts the kernel would otherwise reject.
func ValidateGroups(gs []Group, links []Link) error {
	have := map[string]bool{}
	for _, l := range links {
		have[l.Name] = true
	}
	names, tables, prios := map[string]bool{}, map[int]bool{}, map[int]bool{}
	for _, g := range gs {
		switch {
		case g.Name == "" || names[g.Name]:
			return fmt.Errorf("every group needs a unique name")
		case g.Table < GroupTableMin || g.Table > GroupTableMax || tables[g.Table]:
			return fmt.Errorf("group %s: invalid routing table", g.Name)
		case g.Prio < GroupPrioMin || g.Prio > GroupPrioMax || prios[g.Prio]:
			return fmt.Errorf("group %s: invalid rule priority", g.Name)
		case g.Mode != Failover && g.Mode != Balance:
			return fmt.Errorf("group %s: mode must be failover or balance", g.Name)
		case len(g.Members) == 0:
			return fmt.Errorf("group %s needs at least one connection", g.Name)
		}
		names[g.Name], tables[g.Table], prios[g.Prio] = true, true, true
		seen := map[string]bool{}
		for _, m := range g.Members {
			if !have[m.Link] || seen[m.Link] {
				return fmt.Errorf("group %s: %q is not an available connection", g.Name, m.Link)
			}
			seen[m.Link] = true
			if m.Weight < 0 || m.Weight > 1000 {
				return fmt.Errorf("group %s: share out of range", g.Name)
			}
		}
		for _, s := range g.Sources {
			p, err := netip.ParsePrefix(s)
			if err != nil || !p.Addr().Is4() {
				return fmt.Errorf("group %s: %q is not an IPv4 address range", g.Name, s)
			}
		}
	}
	return nil
}

// ---- IPRouter ---------------------------------------------------------------------

func (r *IPRouter) SetTable(table int, nh []Nexthop, block bool) error {
	t := strconv.Itoa(table)
	if len(nh) == 0 {
		if block {
			_, err := r.ip("route", "replace", "unreachable", "default", "table", t)
			return err
		}
		r.ip("route", "flush", "table", t)
		return nil
	}
	args := []string{"route", "replace", "default", "table", t}
	if len(nh) == 1 {
		args = append(args, "via", nh[0].Gateway, "dev", nh[0].Dev)
	} else {
		for _, n := range nh {
			args = append(args, "nexthop", "via", n.Gateway, "dev", n.Dev, "weight", strconv.Itoa(n.Weight))
		}
	}
	_, err := r.ip(args...)
	return err
}

type ruleJSON struct {
	Priority int    `json:"priority"`
	Src      string `json:"src"`
	SrcLen   int    `json:"srclen"`
	Table    string `json:"table"`
}

func (r *IPRouter) rules() []ruleJSON {
	out, err := r.ip("-j", "-4", "rule", "show")
	if err != nil {
		return nil
	}
	var rs []ruleJSON
	json.Unmarshal(out, &rs)
	return rs
}

func (rj ruleJSON) prefix() string {
	n := rj.SrcLen
	if rj.Src == "" || rj.Src == "all" {
		return ""
	}
	if n == 0 {
		n = 32
	}
	return fmt.Sprintf("%s/%d", rj.Src, n)
}

// SyncRules makes the group's source rules match exactly the wanted list.
func (r *IPRouter) SyncRules(g Group) error {
	t := strconv.Itoa(g.Table)
	want := map[string]bool{}
	for _, s := range g.Sources {
		if p, err := netip.ParsePrefix(s); err == nil {
			want[p.Masked().String()] = true
		}
	}
	have := map[string]bool{}
	for _, ru := range r.rules() {
		if ru.Priority != g.Prio {
			continue
		}
		p := ru.prefix()
		if want[p] && !have[p] {
			have[p] = true
			continue
		}
		r.ip("rule", "del", "priority", strconv.Itoa(g.Prio), "from", p, "lookup", t)
	}
	var first error
	for p := range want {
		if have[p] {
			continue
		}
		if _, err := r.ip("rule", "add", "from", p, "lookup", t, "priority", strconv.Itoa(g.Prio)); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Prune removes rules and routes left by groups that are gone.
func (r *IPRouter) Prune(keep []Group) error {
	prios, tables := map[int]bool{}, map[int]bool{}
	for _, g := range keep {
		prios[g.Prio], tables[g.Table] = true, true
	}
	for _, ru := range r.rules() {
		if ru.Priority >= GroupPrioMin && ru.Priority <= GroupPrioMax && !prios[ru.Priority] {
			if p := ru.prefix(); p != "" {
				r.ip("rule", "del", "priority", strconv.Itoa(ru.Priority), "from", p, "lookup", ru.Table)
			}
		}
	}
	for t := GroupTableMin; t <= GroupTableMax; t++ {
		if !tables[t] {
			r.ip("route", "flush", "table", strconv.Itoa(t))
		}
	}
	return nil
}

var _ GroupRouter = (*IPRouter)(nil)

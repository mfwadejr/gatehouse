package wanmon

import (
	"errors"
	"fmt"
	"testing"
)

type groupRouter struct {
	fakeRouter
	tables map[int]string
	rules  map[string][]string
	pruned int
}

func (g *groupRouter) SetTable(t int, nh []Nexthop, block bool) error {
	s := ""
	for _, n := range nh {
		s += fmt.Sprintf("%s/%d ", n.Dev, n.Weight)
	}
	if len(nh) == 0 && block {
		s = "unreachable"
	}
	g.tables[t] = s
	return nil
}
func (g *groupRouter) SyncRules(gr Group) error { g.rules[gr.Name] = gr.Sources; return nil }
func (g *groupRouter) Prune([]Group) error      { g.pruned++; return nil }

func three() []Link {
	return append(links(), Link{Name: "Cable", Dev: "eth2", Priority: 3, Weight: 1, Table: 102, Mark: 0x102, Failures: 3, Recover: 2})
}

func newGroupMon(gs ...Group) (*Monitor, *groupRouter) {
	r := &groupRouter{fakeRouter: fakeRouter{gws: map[string]string{"eth0": "203.0.113.1", "wwan0": "198.51.100.1", "eth2": "192.0.2.1"}},
		tables: map[int]string{}, rules: map[string][]string{}}
	m := New(three(), Failover, nil, r)
	m.Groups = gs
	m.Refresh()
	return m, r
}

func TestGroupFailoverOrderFollowsMembers(t *testing.T) {
	g := Group{Name: "Cameras", Table: 320, Prio: 1300, Mode: Failover, Sources: []string{"192.168.5.0/24"},
		Members: []Member{{"LTE", 1}, {"Cable", 1}}}
	m, r := newGroupMon(g)
	if r.tables[320] != "wwan0/1 " {
		t.Fatalf("want LTE first, got %q", r.tables[320])
	}
	if len(r.rules["Cameras"]) != 1 || r.pruned == 0 {
		t.Fatalf("rules not synced: %v", r.rules)
	}
	boom := errors.New("x")
	for i := 0; i < 3; i++ {
		m.Observe(m.Links[1], 0, boom)
	}
	if r.tables[320] != "eth2/1 " {
		t.Fatalf("want Cable after LTE fails, got %q", r.tables[320])
	}
	for i := 0; i < 3; i++ {
		m.Observe(m.Links[2], 0, boom)
	}
	if r.tables[320] != "" {
		t.Fatalf("all down with fallback to default should clear the table, got %q", r.tables[320])
	}
	st := m.Snapshot()["group:Cameras"]
	if st.Up || st.Error == "" {
		t.Fatalf("group should report down: %+v", st)
	}
}

func TestGroupBlockWhenAllDown(t *testing.T) {
	g := Group{Name: "Work", Table: 321, Prio: 1301, Mode: Balance, Block: true, Sources: []string{"10.0.0.0/24"},
		Members: []Member{{"Fibre", 70}, {"Cable", 30}}}
	m, r := newGroupMon(g)
	if r.tables[321] != "eth0/70 eth2/30 " {
		t.Fatalf("balance: %q", r.tables[321])
	}
	boom := errors.New("x")
	for _, i := range []int{0, 2} {
		for k := 0; k < 3; k++ {
			m.Observe(m.Links[i], 0, boom)
		}
	}
	if r.tables[321] != "unreachable" {
		t.Fatalf("block: %q", r.tables[321])
	}
	if via := m.Snapshot()["group:Work"].Via; len(via) != 0 {
		t.Fatalf("via %v", via)
	}
}

func TestMainRouteUnaffectedByGroups(t *testing.T) {
	g := Group{Name: "G", Table: 322, Prio: 1302, Mode: Failover, Members: []Member{{"Cable", 1}}}
	_, r := newGroupMon(g)
	if got := lastDefault(&r.fakeRouter); got != "eth0/1 " {
		t.Fatalf("main default %q", got)
	}
}

func TestValidateGroups(t *testing.T) {
	ok := Group{Name: "A", Table: 320, Prio: 1300, Mode: Failover, Members: []Member{{"Fibre", 1}}, Sources: []string{"10.0.0.0/8"}}
	if err := ValidateGroups([]Group{ok}, links()); err != nil {
		t.Fatal(err)
	}
	bad := []Group{
		{Name: "", Table: 320, Prio: 1300, Mode: Failover, Members: ok.Members},
		{Name: "B", Table: 100, Prio: 1300, Mode: Failover, Members: ok.Members},
		{Name: "B", Table: 321, Prio: 5, Mode: Failover, Members: ok.Members},
		{Name: "B", Table: 321, Prio: 1301, Mode: "x", Members: ok.Members},
		{Name: "B", Table: 321, Prio: 1301, Mode: Failover},
		{Name: "B", Table: 321, Prio: 1301, Mode: Failover, Members: []Member{{"Nope", 1}}},
		{Name: "B", Table: 321, Prio: 1301, Mode: Failover, Members: ok.Members, Sources: []string{"fe80::/10"}},
		{Name: "A", Table: 321, Prio: 1301, Mode: Failover, Members: ok.Members},
		{Name: "B", Table: 320, Prio: 1301, Mode: Failover, Members: ok.Members},
	}
	for i, b := range bad {
		if err := ValidateGroups([]Group{ok, b}, links()); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"gatehouse/internal/fw"
)

func TestRouteByPortThroughAConnectionGroup(t *testing.T) {
	c, fn, ff := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Cable", "iface": "eth2", "type": "dhcp", "priority": 2, "enabled": true})
	if rc != 200 {
		t.Fatalf("wan: %d %s", rc, raw)
	}
	_ = m
	// Primary and backup connection, in that order.
	var members []map[string]any
	for _, n := range []string{"Fibre", "Cable"} {
		members = append(members, map[string]any{"wan_id": wanIDByName(t, c, n), "weight": 1})
	}
	rc, _, raw = c.call("PUT", "/api/net/groups", map[string]any{"name": "Usenet", "mode": "failover", "members": members})
	if rc != 200 {
		t.Fatalf("group: %d %s", rc, raw)
	}
	uses := []map[string]string{{"kind": "network", "value": "Home"}, {"kind": "cidr", "value": "192.168.1.77"}}
	who := "net:Home,192.168.1.77"

	// Several websites, addresses and ports at once.
	rc, _, raw = c.call("PUT", "/api/routes", map[string]any{"name": "News", "enabled": true, "target_kind": "group", "target": "Usenet", "who": who,
		"where":   "dom:news.example.com,203.0.113.5,198.51.100.0/24",
		"service": "tcp/563,tcp/5000-5010,ntp"})
	if rc != 200 {
		t.Fatalf("route: %d %s", rc, raw)
	}
	for _, want := range []string{"tcp dport 563", "tcp dport 5000-5010", "udp dport 123", "203.0.113.5", "198.51.100.0/24", "meta mark set 0x1100", "ct mark >= 0x1000"} {
		if !strings.Contains(ff.script, want) {
			t.Errorf("firewall lacks %q\n%s", want, ff.script)
		}
	}
	if len(fn.plan.Groups) != 1 || fn.plan.Groups[0].Mark != fw.GroupMarkBase {
		t.Fatalf("the group did not get its mark: %+v", fn.plan.Groups)
	}
	_, _, view := c.call("GET", "/api/routes", nil)
	if !strings.Contains(string(view), "tcp/563, tcp/5000-5010, ntp") || !strings.Contains(string(view), `"state":"working"`) {
		t.Fatalf("view: %s", view)
	}
	// Ports only: every destination, one service. And bad input is refused in plain words.
	for _, bad := range []string{"tcp/99999", "abc", "tcp/20-10", "list:NoSuchList"} {
		if rc, _, _ := c.call("PUT", "/api/routes", map[string]any{"name": "Bad", "enabled": true, "target_kind": "group", "target": "Usenet", "who": who, "service": bad}); rc == 200 {
			t.Fatalf("accepted %v", bad)
		}
	}
	// Pause for a while, then resume; and a weekly schedule.
	var rv struct {
		Routes []struct {
			ID int64 `json:"id"`
		} `json:"routes"`
	}
	json.Unmarshal(view, &rv)
	id := itoa(rv.Routes[0].ID)
	rc, _, raw = c.call("POST", "/api/routes/"+id+"/pause", map[string]any{"minutes": 60})
	if rc != 200 || !strings.Contains(string(raw), `"state":"paused"`) || !strings.Contains(ff.script, "meta time >= ") {
		t.Fatalf("pause: %d %s\n%s", rc, raw, ff.script)
	}
	rc, _, raw = c.call("POST", "/api/routes/"+id+"/pause", map[string]any{"minutes": 0})
	if rc != 200 || !strings.Contains(string(raw), `"state":"working"`) || strings.Contains(ff.script, "meta time >= ") {
		t.Fatalf("resume: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/routes/"+id+"/pause", map[string]any{"minutes": -5}); rc == 200 {
		t.Fatal("negative pause accepted")
	}
	if rc, _, raw := c.call("POST", "/api/routes/"+id+"/pause", map[string]any{"minutes": -1}); rc != 200 || !strings.Contains(string(raw), `"state":"paused"`) {
		t.Fatalf("pause until resumed: %d %s", rc, raw)
	}
	c.call("POST", "/api/routes/"+id+"/pause", map[string]any{"minutes": 0})
	if rc, _, _ := c.call("POST", "/api/routes/9999/pause", map[string]any{"minutes": 5}); rc == 200 {
		t.Fatal("pause of a missing route")
	}
	rc, _, raw = c.call("PUT", "/api/routes", map[string]any{"id": rv.Routes[0].ID, "name": "News", "enabled": true, "target_kind": "group", "target": "Usenet", "uses": uses,
		"sched": map[string]any{"mode": "weekly", "days": []int{1, 2, 3, 4, 5}, "from": "08:00", "to": "17:00"}})
	if rc != 200 || !strings.Contains(string(raw), `"mode":"weekly"`) || !strings.Contains(ff.script, "meta day") {
		t.Fatalf("schedule: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/routes", map[string]any{"id": rv.Routes[0].ID, "name": "News", "enabled": true, "target_kind": "group", "target": "Usenet", "uses": uses,
		"sched": map[string]any{"mode": "weekly", "days": []int{1}, "from": "25:00", "to": "17:00"}}); rc == 200 {
		t.Fatal("a schedule with no such time was accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/routes", map[string]any{"name": "Ghost", "enabled": true, "target_kind": "group", "target": "Nope", "uses": uses}); rc == 200 {
		t.Fatal("route to a group that does not exist")
	}
}

func wanIDByName(t *testing.T, c *client, name string) int64 {
	t.Helper()
	_, _, raw := c.call("GET", "/api/net/wans", nil)
	var got struct {
		Wans []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"wans"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("wans: %v %s", err, raw)
	}
	for _, w := range got.Wans {
		if w.Name == name {
			return w.ID
		}
	}
	t.Fatalf("no connection %s in %s", name, raw)
	return 0
}

func TestRouteWithItsOwnConnectionsInOrder(t *testing.T) {
	c, fn, ff := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Cable", "iface": "eth2", "type": "dhcp", "priority": 2, "enabled": true}); rc != 200 {
		t.Fatalf("wan: %d %s", rc, raw)
	}
	// The new connection is applied on the Internet page first, as it would be in real use.
	if rc, _, raw := c.call("POST", "/api/net/apply", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("apply: %d %s", rc, raw)
	}
	c.call("POST", "/api/net/confirm", map[string]string{"token": "ntok"})
	who := "net:Home"
	save := func(name, target, mode string) (int, []byte) {
		rc, _, raw := c.call("PUT", "/api/routes", map[string]any{"name": name, "enabled": true, "target_kind": "links", "target": target, "mode": mode, "who": who, "service": "ntp"})
		return rc, raw
	}
	// Backup line first, main line second.
	if rc, raw := save("Time", "Cable,Fibre", "failover"); rc != 200 {
		t.Fatalf("route: %d %s", rc, raw)
	}
	find := func(name string) (g struct {
		Name    string
		Mark    int
		Mode    string
		Members []struct{ Link string }
	}, ok bool) {
		b, _ := json.Marshal(fn.plan.Groups)
		var gs []struct {
			Name    string
			Mark    int
			Mode    string
			Members []struct{ Link string }
		}
		json.Unmarshal(b, &gs)
		for _, x := range gs {
			if x.Name == name {
				return x, true
			}
		}
		return g, false
	}
	g, ok := find("route:Time")
	if !ok || len(g.Members) != 2 || g.Members[0].Link != "Cable" || g.Members[1].Link != "Fibre" || g.Mode != "failover" {
		t.Fatalf("the route's own group: %+v (found %v)", g, ok)
	}
	if g.Mark != fw.RouteLinksMark(0) || !strings.Contains(ff.script, "meta mark set 0x1108") || !strings.Contains(ff.script, "udp dport 123") {
		t.Fatalf("mark %x\n%s", g.Mark, ff.script)
	}
	// The failover-only line can be used on its own, and a route can share across several.
	if rc, raw := save("Second only", "Cable", "failover"); rc != 200 {
		t.Fatalf("single: %d %s", rc, raw)
	}
	if rc, raw := save("Spread", "Fibre,Cable", "balance"); rc != 200 {
		t.Fatalf("balance: %d %s", rc, raw)
	}
	if g, ok := find("route:Spread"); !ok || g.Mode != "balance" || len(g.Members) != 2 {
		t.Fatalf("spread: %+v", g)
	}
	if g, ok := find("route:Second only"); !ok || len(g.Members) != 1 || g.Members[0].Link != "Cable" {
		t.Fatalf("second only: %+v", g)
	}
	// Nothing invented: unknown or empty choices are refused, and the view says what it does.
	if rc, _ := save("Ghost", "Nowhere", "failover"); rc == 200 {
		t.Fatal("a connection that does not exist was accepted")
	}
	if rc, _ := save("Empty", " , ", "failover"); rc == 200 {
		t.Fatal("no connection was accepted")
	}
	_, _, view := c.call("GET", "/api/routes", nil)
	if !strings.Contains(string(view), "Cable, then Fibre") || !strings.Contains(string(view), "shared across Fibre, Cable") {
		t.Fatalf("view: %s", view)
	}
	// Turning the route off takes its table away; removing the last route does too.
	var rv struct{ Routes []struct{ ID int64 } }
	json.Unmarshal(view, &rv)
	for _, r := range rv.Routes {
		c.call("DELETE", "/api/routes/"+itoa(r.ID), nil)
	}
	if _, ok := find("route:Time"); ok {
		t.Fatal("a removed route left its group behind")
	}
}

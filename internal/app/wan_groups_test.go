package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/auth"
	"gatehouse/internal/wanmon"
)

func TestWanGroups(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn := &fakeNet{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Auth.CreateUser("admin", "correct horse battery", "admin")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: "http://" + web, hc: &http.Client{Jar: jar}}
	for i := 0; i < 300; i++ {
		if resp, err := c.hc.Get(c.base + "/api/health"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, m, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	c.csrf = m["csrf"].(string)
	_, m, _ = c.call("POST", "/api/auth/enrol/begin", nil)
	secret := m["secret"].(string)
	c.call("POST", "/api/auth/enrol/confirm", map[string]string{"code": auth.TOTPCodeAt(secret, time.Now())})
	step := 0
	code := func() map[string]string {
		step++
		return map[string]string{"code": auth.TOTPCodeAt(secret, time.Now().Add(time.Duration(30*step)*time.Second))}
	}

	rc, m, raw := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Home", "kind": "trusted", "ifaces": []string{"eth1"}, "management": true, "internet": true, "gateway": true, "intra_allow": true})
	if rc != 200 {
		t.Fatalf("zone: %d %s", rc, raw)
	}
	_, _, ifraw := c.call("GET", "/api/net/interfaces", nil)
	var ifs []map[string]any
	json.Unmarshal(ifraw, &ifs)
	c.call("PUT", "/api/net/interfaces", map[string]any{"id": ifs[0]["id"], "name": "eth1", "kind": "phys", "ipv4": "192.168.1.1/24", "zone_id": int64(m["id"].(float64)), "enabled": true})
	wanID := map[string]int64{}
	for i, n := range []string{"Fibre", "Cable", "LTE"} {
		rc, m, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": n, "iface": []string{"eth0", "eth2", "eth3"}[i], "type": "dhcp", "priority": i + 1, "enabled": true})
		if rc != 200 {
			t.Fatalf("wan %s: %d %s", n, rc, raw)
		}
		wanID[n] = int64(m["id"].(float64))
	}
	mem := func(names ...any) []map[string]any {
		var out []map[string]any
		for i := 0; i < len(names); i += 2 {
			out = append(out, map[string]any{"wan_id": wanID[names[i].(string)], "weight": names[i+1]})
		}
		return out
	}

	// Shares must add up to 100.
	rc, _, raw = c.call("PUT", "/api/net/groups", map[string]any{"name": "Work", "mode": "balance", "members": mem("Fibre", 60, "Cable", 30)})
	if rc != 400 || !strings.Contains(string(raw), "100%") {
		t.Fatalf("shares: %d %s", rc, raw)
	}
	// The default group's name is reserved.
	if rc, _, _ = c.call("PUT", "/api/net/groups", map[string]any{"name": "default", "mode": "failover", "members": mem("LTE", 1)}); rc != 400 {
		t.Fatalf("reserved name: %d", rc)
	}
	rc, m, raw = c.call("PUT", "/api/net/groups", map[string]any{"name": "Work", "mode": "balance", "fallback": "block",
		"members": mem("Fibre", 70, "Cable", 30), "uses": []map[string]any{{"kind": "network", "value": "Home"}, {"kind": "cidr", "value": "10.5.0.7"}}})
	if rc != 200 {
		t.Fatalf("group: %d %s", rc, raw)
	}
	work := int64(m["id"].(float64))
	if rc, _, raw = c.call("PUT", "/api/net/groups", map[string]any{"name": "Cameras", "mode": "failover",
		"members": mem("LTE", 1, "Cable", 1), "uses": []map[string]any{{"kind": "cidr", "value": "10.5.0.7"}, {"kind": "cidr", "value": "bogus"}}}); rc != 400 {
		t.Fatalf("bad range accepted: %d %s", rc, raw)
	}
	rc, m, raw = c.call("PUT", "/api/net/groups", map[string]any{"name": "Cameras", "mode": "failover",
		"members": mem("LTE", 1, "Cable", 1), "uses": []map[string]any{{"kind": "cidr", "value": "10.5.0.7"}}})
	if rc != 200 {
		t.Fatalf("cameras: %d %s", rc, raw)
	}
	cams := int64(m["id"].(float64))

	// 10.5.0.7 moved to Cameras; Work keeps the Home network.
	_, _, raw = c.call("GET", "/api/net/groups", nil)
	var got struct {
		Default struct {
			Label string `json:"label"`
			Mode  string `json:"mode"`
		} `json:"default"`
		Groups []groupRow `json:"groups"`
	}
	json.Unmarshal(raw, &got)
	if got.Default.Label != "Default" || len(got.Groups) != 2 {
		t.Fatalf("listing: %s", raw)
	}
	if len(got.Groups[0].Uses) != 1 || got.Groups[0].Uses[0].Kind != "network" || got.Groups[0].Resolved[0] != "192.168.1.0/24" {
		t.Fatalf("work uses: %+v", got.Groups[0])
	}
	if got.Groups[1].Members[0].Name != "LTE" {
		t.Fatalf("member order: %+v", got.Groups[1].Members)
	}

	// Rename the default group and give it shares of its own.
	if rc, _, raw = c.call("PUT", "/api/net/default-group", map[string]any{"label": "Everyone else", "mode": "balance", "members": mem("Cable", 25, "Fibre", 75)}); rc != 200 {
		t.Fatalf("default group: %d %s", rc, raw)
	}
	_, _, raw = c.call("GET", "/api/net/groups", nil)
	if !strings.Contains(string(raw), `"label":"Everyone else"`) {
		t.Fatalf("label: %s", raw)
	}
	if rc, _, _ = c.call("PUT", "/api/net/groups", map[string]any{"name": "everyone else", "mode": "failover", "members": mem("LTE", 1)}); rc != 400 {
		t.Fatalf("renamed default's name should be reserved: %d", rc)
	}

	if rc, _, raw = c.call("POST", "/api/net/apply", code()); rc != 200 {
		t.Fatalf("apply: %d %s", rc, raw)
	}
	p := fn.plan
	if p.Mode != "balance" || len(p.Links) != 3 || p.Links[0].Name != "Cable" || p.Links[0].Weight != 25 {
		t.Fatalf("default plan: %+v", p.Links)
	}
	if len(p.Groups) != 2 {
		t.Fatalf("groups in plan: %+v", p.Groups)
	}
	w := p.Groups[0]
	if w.Name != "Work" || w.Table != wanmon.GroupTableMin || w.Prio != wanmon.GroupPrioMin || !w.Block || w.Members[0].Weight != 70 || w.Sources[0] != "192.168.1.0/24" {
		t.Fatalf("work spec: %+v", w)
	}
	cm := p.Groups[1]
	if cm.Table != wanmon.GroupTableMin+1 || cm.Sources[0] != "10.5.0.7/32" || cm.Members[0].Link != "LTE" {
		t.Fatalf("cameras spec: %+v", cm)
	}

	// Turning a connection off drops it from the group; a group with nothing left is skipped.
	c.call("POST", "/api/net/confirm", map[string]string{"token": "ntok"})
	c.call("PUT", "/api/net/wans", map[string]any{"id": wanID["LTE"], "name": "LTE", "iface": "eth3", "type": "dhcp", "priority": 3, "enabled": false})
	plan, err := a.buildPlan(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups) != 2 || len(plan.Groups[1].Members) != 1 || plan.Groups[1].Members[0].Link != "Cable" {
		t.Fatalf("after disabling LTE: %+v", plan.Groups)
	}

	// Deleting a group removes its rules from the plan.
	c.call("DELETE", "/api/net/groups/"+itoa(cams), nil)
	c.call("DELETE", "/api/net/groups/"+itoa(work), nil)
	plan, _ = a.buildPlan(a.Config)
	if len(plan.Groups) != 0 {
		t.Fatalf("groups after delete: %+v", plan.Groups)
	}
}

package app

import (
	"encoding/json"
	"github.com/miekg/dns"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/fw"
)

func TestDeviceRulesFollowAddresses(t *testing.T) {
	c, _, ff := siteBox(t, "192.168.1.1/24")
	a := c.app
	mac := "aa:bb:cc:dd:ee:01"

	// A device made by hand, a group, and a list of ports.
	if rc, _, raw := c.call("POST", "/api/devices", map[string]any{"mac": mac, "name": "Kitchen TV"}); rc != 200 {
		t.Fatalf("device: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/devices", map[string]any{"mac": mac, "name": "again"}); rc == 200 {
		t.Fatal("device created twice")
	}
	if rc, _, _ := c.call("POST", "/api/devices", map[string]any{"mac": "nonsense"}); rc == 200 {
		t.Fatal("bad MAC accepted")
	}
	rc, m, raw := c.call("PUT", "/api/fw/groups", map[string]any{"name": "Kids", "members": []string{mac}})
	if rc != 200 {
		t.Fatalf("group: %d %s", rc, raw)
	}
	gid := int64(m["id"].(float64))
	if rc, _, raw := c.call("PUT", "/api/fw/portlists", map[string]any{"name": "Games", "ports": []string{"tcp/3074", "udp/27000-27050"}}); rc != 200 {
		t.Fatalf("ports: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/fw/portlists", map[string]any{"name": "Bad", "ports": []string{"tcp/99999"}}); rc == 200 {
		t.Fatal("bad port list accepted")
	}
	rule := map[string]any{"name": "Kids no games", "from": "Home", "to": "Internet", "src": "grp:Kids", "service": "list:Games", "action": "drop", "enabled": true,
		"ctstate": "new", "descr": "after bedtime", "sched": map[string]any{"mode": "daily", "from": "21:00", "to": "07:00"}}
	if rc, _, raw := c.call("PUT", "/api/fw/rules", rule); rc != 200 {
		t.Fatalf("rule: %d %s", rc, raw)
	}
	// Things that cannot be saved.
	for name, mut := range map[string]map[string]any{
		"unknown group":      {"name": "X1", "src": "grp:Nobody"},
		"mac as destination": {"name": "X2", "dst": "aa:bb:cc:dd:ee:09"},
		"domain as source":   {"name": "X3", "src": "dom:example.com"},
		"bad schedule":       {"name": "X4", "sched": map[string]any{"mode": "daily", "from": "10:00", "to": "10:00"}},
	} {
		r := map[string]any{"from": "Home", "to": "Internet", "action": "drop", "enabled": true}
		for k, v := range mut {
			r[k] = v
		}
		if rc, _, _ := c.call("PUT", "/api/fw/rules", r); rc == 200 {
			t.Errorf("%s was saved", name)
		}
	}

	// The device has no address yet: the list says so, and the rule is still valid.
	var view []struct {
		Name     string                                 `json:"name"`
		Warnings []string                               `json:"warnings"`
		When     string                                 `json:"when"`
		SrcChips []struct{ Kind, Label, Detail string } `json:"src_chips"`
	}
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	json.Unmarshal(raw, &view)
	if len(view) != 1 || view[0].When != "Every day, 21:00 to 07:00" || view[0].SrcChips[0].Kind != "group" || view[0].SrcChips[0].Label != "Kids" {
		t.Fatalf("view: %s", raw)
	}

	// Applying puts the ruleset and the set contents in one step.
	if rc, _, raw := c.call("POST", "/api/fw/apply", map[string]any{}); rc != 200 {
		t.Fatalf("apply: %d %s", rc, raw)
	}
	ff.mu.Lock()
	script := ff.script
	ff.mu.Unlock()
	set := fw.SetNameFor(fw.Ref{Kind: fw.RefGroup, Val: "Kids"}, 4)
	if !strings.Contains(script, "set "+set+" { type ipv4_addr; }") || !strings.Contains(script, "ip saddr @"+set) || !strings.Contains(script, "ct state new") {
		t.Fatalf("script:\n%s", script)
	}

	// A lease appears: only the set changes, and nothing is waiting to be applied.
	a.Config.Exec(`INSERT INTO dhcp_scopes(id,name,iface,subnet,range_start,range_end,router) VALUES(1,'lan','eth1','192.168.1.0/24','192.168.1.100','192.168.1.200','192.168.1.1')`)
	a.Config.Exec(`INSERT INTO dhcp_leases(scope_id,ip,mac,start,expires,state) VALUES(1,'192.168.1.150',?,?,?, 'active')`, strings.ToUpper(mac), time.Now().Unix(), time.Now().Add(time.Hour).Unix())
	a.syncSets(false)
	ff.mu.Lock()
	fill := ff.sets[set]
	ff.mu.Unlock()
	if len(fill.Elems) != 1 || fill.Elems[0] != "192.168.1.150" || !fill.Replace {
		t.Fatalf("set after lease: %+v", fill)
	}
	// The lease moves to another address: the old one is dropped.
	a.Config.Exec(`UPDATE dhcp_leases SET ip='192.168.1.151'`)
	a.syncSets(false)
	ff.mu.Lock()
	fill = ff.sets[set]
	ff.mu.Unlock()
	if len(fill.Elems) != 1 || fill.Elems[0] != "192.168.1.151" {
		t.Fatalf("set after move: %+v", fill)
	}
	// A reservation counts too, and "reserved only" groups use nothing else.
	a.Config.Exec(`INSERT INTO dhcp_reservations(scope_id,mac,ip) VALUES(1,?,'192.168.1.60')`, mac)
	c.call("PUT", "/api/fw/groups", map[string]any{"id": gid, "name": "Kids", "members": []string{mac}, "reserved_only": true})
	a.syncSets(false)
	ff.mu.Lock()
	fill = ff.sets[set]
	ff.mu.Unlock()
	if len(fill.Elems) != 1 || fill.Elems[0] != "192.168.1.60" {
		t.Fatalf("reserved-only set: %+v", fill)
	}

	// Things in use cannot be removed.
	_, _, raw = c.call("GET", "/api/devices", nil)
	var devsOut struct {
		Devices []struct {
			ID  int64
			MAC string
		}
	}
	json.Unmarshal(raw, &devsOut)
	devs := devsOut.Devices
	for _, tc := range []struct{ path, what string }{{"/api/fw/groups/" + istr(int(gid)), "Kids no games"}, {"/api/fw/portlists/1", "Kids no games"}} {
		rc, _, body := c.call("DELETE", tc.path, nil)
		if rc == 200 || !strings.Contains(string(body), tc.what) {
			t.Errorf("DELETE %s: %d %s", tc.path, rc, body)
		}
	}
	// A device named directly by a rule is protected as well.
	c.call("PUT", "/api/fw/rules", map[string]any{"name": "TV direct", "from": "Home", "to": "Internet", "src": "dev:" + mac, "action": "accept", "enabled": true})
	if rc, _, body := c.call("DELETE", "/api/devices/"+istr(int(devs[0].ID)), nil); rc == 200 || !strings.Contains(string(body), "TV direct") {
		t.Fatalf("device in use was forgotten: %d %s", rc, body)
	}

	// Renaming a group keeps the rules pointing at it.
	c.call("PUT", "/api/fw/groups", map[string]any{"id": gid, "name": "Children", "members": []string{mac}})
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	if !strings.Contains(string(raw), `"grp:Children"`) || strings.Contains(string(raw), `"grp:Kids"`) {
		t.Fatalf("rename did not follow: %s", raw)
	}

	// Hit counts accumulate across firewall reloads.
	ff.mu.Lock()
	ff.counters = map[string]fw.Counter{"rule:Home:Internet:Kids no games": {Packets: 5, Bytes: 500}}
	ff.mu.Unlock()
	a.sampleHits()
	ff.mu.Lock()
	ff.counters = map[string]fw.Counter{"rule:Home:Internet:Kids no games": {Packets: 2, Bytes: 100}} // reloaded: counting restarted
	ff.mu.Unlock()
	a.sampleHits()
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	var hv []struct {
		Name    string
		Hits    int64
		LastHit int64 `json:"last_hit"`
	}
	json.Unmarshal(raw, &hv)
	if hv[0].Hits != 7 || hv[0].LastHit == 0 {
		t.Fatalf("hits: %s", raw)
	}
}

func TestDomainAnswersFillSets(t *testing.T) {
	c, _, ff := siteBox(t, "192.168.1.1/24")
	a := c.app
	if rc, _, raw := c.call("PUT", "/api/fw/rules", map[string]any{"name": "No video", "from": "Home", "to": "Internet", "dst": "dom:video.example", "action": "drop", "enabled": true}); rc != 200 {
		t.Fatalf("rule: %d %s", rc, raw)
	}
	a.syncSets(false) // learns which domains to watch
	rr, _ := dnsAnswer("cdn.video.example.", "203.0.113.9", 60)
	a.onDNSAnswer("cdn.video.example", rr)
	other, _ := dnsAnswer("other.example.", "203.0.113.10", 60)
	a.onDNSAnswer("other.example", other)
	a.flushDomains()
	set := fw.SetNameFor(fw.Ref{Kind: fw.RefDomain, Val: "video.example"}, 4)
	ff.mu.Lock()
	fill := ff.sets[set]
	ff.mu.Unlock()
	if len(fill.Elems) != 1 || !strings.HasPrefix(fill.Elems[0], "203.0.113.9 timeout ") {
		t.Fatalf("domain set: %+v", fill)
	}
}

func dnsAnswer(name, ip string, ttl uint32) ([]dns.RR, error) {
	rr, err := dns.NewRR(name + " " + istr(int(ttl)) + " IN A " + ip)
	return []dns.RR{rr}, err
}

func TestMatrixChangesAndKeepsHandRules(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	if rc, _, raw := c.call("PUT", "/api/fw/zones", map[string]any{"name": "Guests", "kind": "guest", "ifaces": []string{"eth2"}, "internet": true, "gateway": true}); rc != 200 {
		t.Fatalf("guests: %d %s", rc, raw)
	}
	c.call("PUT", "/api/fw/rules", map[string]any{"name": "Print", "from": "Guests", "to": "Home", "dst": "192.168.1.50", "service": "print", "action": "accept", "enabled": true})
	set := func(from, to, mode string) int {
		rc, _, raw := c.call("POST", "/api/fw/matrix", map[string]any{"from": from, "to": to, "mode": mode})
		if rc != 200 {
			t.Logf("%s %s %s: %s", from, to, mode, raw)
		}
		return rc
	}
	// Replies only: Guests cannot start connections to Home; Home (trusted) already reaches Guests,
	// so no extra rule is needed there.
	if set("Guests", "Home", "replies") != 200 {
		t.Fatal("replies refused")
	}
	var rules []struct {
		Name, From, To, Action string
		Matrix                 bool
	}
	_, _, raw := c.call("GET", "/api/fw/rules", nil)
	json.Unmarshal(raw, &rules)
	if len(rules) != 2 || !rules[1].Matrix || rules[1].Action != "drop" || rules[0].Name != "Print" {
		t.Fatalf("rules: %s", raw)
	}
	// Changing again replaces its own rule, never piles up.
	set("Guests", "Home", "block")
	set("Guests", "Home", "block")
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	json.Unmarshal(raw, &rules)
	if len(rules) != 2 {
		t.Fatalf("rules piled up: %s", raw)
	}
	// Two guest networks: replies-only from Home to Guests needs a way back, which the matrix adds.
	set("Guests", "Home", "default")
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	json.Unmarshal(raw, &rules)
	if len(rules) != 1 {
		t.Fatalf("default did not remove it: %s", raw)
	}
	set("Home", "Guests", "replies")
	_, _, raw = c.call("GET", "/api/fw/rules", nil)
	if !strings.Contains(string(raw), `"from":"Guests","to":"Home"`) || !strings.Contains(string(raw), `"action":"accept"`) {
		t.Fatalf("no way back added: %s", raw)
	}
	// Refusals.
	if set("Internet", "Home", "allow") == 200 {
		t.Error("opened the internet to the home network")
	}
	if set("Home", "Home", "block") == 200 {
		t.Error("changed a network onto itself")
	}
	if set("Home", "Internet", "replies") == 200 {
		t.Error("replies only toward the internet")
	}
	if set("Home", "Nowhere", "block") == 200 {
		t.Error("unknown network")
	}
	_, _, raw = c.call("GET", "/api/fw/overview", nil)
	if !strings.Contains(string(raw), `"pair_open"`) || !strings.Contains(string(raw), `"Home|Internet":true`) {
		t.Fatalf("overview: %s", raw)
	}
}

package app

import (
	"strings"
	"testing"
	"time"

	"gatehouse/internal/ndpi"
)

func TestTrafficLabels(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app

	// Off by default, and nothing is labelled.
	if _, _, ok := a.ndpiLabel("tcp", "192.168.1.5", 50000, "203.0.113.9", 443); ok {
		t.Fatal("off means no labels")
	}
	_, m, _ := c.call("GET", "/api/ndpi", nil)
	if m["enabled"] != false || m["installed"] != false {
		t.Fatalf("status: %v", m)
	}
	// Without the program on this system, turning it on is refused in plain words and stays off.
	rc, _, raw := c.call("PUT", "/api/ndpi", map[string]any{"enabled": true})
	if rc != 409 || !strings.Contains(string(raw), "fresh install image") || a.ndpiOn() {
		t.Fatalf("want a plain refusal: %d %s", rc, raw)
	}

	fn.mu.Lock()
	fn.ndpiHave = true
	fn.mu.Unlock()
	if rc, _, raw := c.call("PUT", "/api/ndpi", map[string]any{"enabled": true}); rc != 200 {
		t.Fatalf("enable: %d %s", rc, raw)
	}
	fn.mu.Lock()
	last := fn.ndpiPlans[len(fn.ndpiPlans)-1]
	fn.mu.Unlock()
	if !last.Enabled || len(last.Ifaces) == 0 {
		t.Fatalf("the helper should be told to watch the inside ports: %+v", last)
	}
	wans := map[string]bool{}
	if rows, err := a.Config.Query(`SELECT iface FROM wan_links`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			wans[n] = true
		}
		rows.Close()
	}
	for _, n := range last.Ifaces {
		if wans[n] {
			t.Fatalf("an internet port must never be watched: %v", last.Ifaces)
		}
	}

	a.ndpiTable().Add(ndpi.Flow{Proto: "tcp", Src: "192.168.1.5", SP: 50000, Dst: "203.0.113.9", DP: 443, App: "TLS.YouTube", Cat: "Media", Seen: time.Now().Unix()})
	if nm, cat, ok := a.ndpiLabel("tcp", "192.168.1.5", 50000, "203.0.113.9", 443); !ok || nm != "YouTube" || cat != "Media" {
		t.Fatalf("label: %q %q %v", nm, cat, ok)
	}
	// Plain protocols are not applications.
	a.ndpiTable().Add(ndpi.Flow{Proto: "udp", Src: "192.168.1.5", SP: 5353, Dst: "192.168.1.1", DP: 53, App: "DNS", Seen: time.Now().Unix()})
	if _, _, ok := a.ndpiLabel("udp", "192.168.1.5", 5353, "192.168.1.1", 53); ok {
		t.Fatal("DNS is not an application")
	}

	if rc, _, _ := c.call("PUT", "/api/ndpi", map[string]any{"enabled": false}); rc != 200 {
		t.Fatal("disable")
	}
	fn.mu.Lock()
	last = fn.ndpiPlans[len(fn.ndpiPlans)-1]
	fn.mu.Unlock()
	if last.Enabled {
		t.Fatal("turning it off must stop the program")
	}
}

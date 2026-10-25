package app

import (
	"encoding/json"
	"testing"
)

func upstreamIDs(t *testing.T, c *client) []int {
	t.Helper()
	rc, _, raw := c.call("GET", "/api/dns/upstreams", nil)
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	var list []struct {
		ID       int `json:"id"`
		Position int `json:"position"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	var ids []int
	for i, u := range list {
		if u.Position != i+1 {
			t.Fatalf("position %d at place %d", u.Position, i+1)
		}
		ids = append(ids, u.ID)
	}
	return ids
}

func TestDNSServersCanBeOrderedAndTheModeChosen(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	ids := upstreamIDs(t, c)
	if len(ids) < 2 {
		t.Fatalf("expected the two default servers, got %v", ids)
	}
	rev := []int{ids[1], ids[0]}
	if rc, _, raw := c.call("PUT", "/api/dns/upstreams/order", map[string]any{"ids": rev}); rc != 200 {
		t.Fatalf("order: %d %s", rc, raw)
	}
	got := upstreamIDs(t, c)
	if got[0] != rev[0] || got[1] != rev[1] {
		t.Fatalf("order not kept: %v want %v", got, rev)
	}
	// A list that leaves one out or repeats one is refused, so a stale page cannot lose a server.
	if rc, _, _ := c.call("PUT", "/api/dns/upstreams/order", map[string]any{"ids": []int{ids[0]}}); rc != 400 {
		t.Fatalf("incomplete list accepted: %d", rc)
	}
	if rc, _, _ := c.call("PUT", "/api/dns/upstreams/order", map[string]any{"ids": []int{ids[0], ids[0]}}); rc != 400 {
		t.Fatalf("duplicate accepted: %d", rc)
	}
	// Mode.
	_, m, _ := c.call("GET", "/api/dns/upstream-mode", nil)
	if m["mode"] != "fastest" {
		t.Fatalf("default mode: %v", m)
	}
	if rc, _, _ := c.call("PUT", "/api/dns/upstream-mode", map[string]any{"mode": "chaos"}); rc != 400 {
		t.Fatalf("bad mode accepted: %d", rc)
	}
	if rc, _, _ := c.call("PUT", "/api/dns/upstream-mode", map[string]any{"mode": "ordered"}); rc != 200 {
		t.Fatal("could not choose in-my-order")
	}
	_, m, _ = c.call("GET", "/api/dns/upstream-mode", nil)
	if m["mode"] != "ordered" {
		t.Fatalf("mode not kept: %v", m)
	}
	// Testing a server that is not in use says so.
	if rc, _, _ := c.call("POST", "/api/dns/upstreams/99999/test", map[string]any{}); rc != 400 {
		t.Fatalf("unknown server: %d", rc)
	}
	// A server added later goes to the end.
	if rc, _, raw := c.call("POST", "/api/dns/upstreams", map[string]any{"name": "Mine", "address": "192.0.2.53:53", "proto": "udp"}); rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	if n := len(upstreamIDs(t, c)); n != 3 {
		t.Fatalf("expected 3 servers, got %d", n)
	}
}

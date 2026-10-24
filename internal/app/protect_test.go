package app

import (
	"strings"
	"testing"
)

func TestLimitCovers(t *testing.T) {
	lim := []string{"192.168.1.0/24"}
	for _, tc := range []struct {
		ip, iface, rescue string
		want              bool
	}{
		{"192.168.1.50", "eth1", "", true},
		{"192.168.2.50", "eth1", "", false},
		{"192.168.2.50", "eth1", "eth1", true}, // the setup port is never cut off
		{"127.0.0.1", "lo", "", true},
		{"192.168.2.50", "", "", true}, // unknown: do not block the change
	} {
		if got := limitCovers(lim, tc.ip, tc.iface, tc.rescue); got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
	if !limitCovers(nil, "10.0.0.1", "eth1", "") {
		t.Error("no limit must cover everyone")
	}
}

func TestProtectedBlockAndLimit(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, _, raw := c.call("GET", "/api/fw/protected", nil)
	s := string(raw)
	if !strings.Contains(s, "Always reach this console") || !strings.Contains(s, "Safe starting rules") || !strings.Contains(s, `"subnets":["192.168.1.0/24"]`) {
		t.Fatalf("protected: %s", s)
	}
	// a limit that names a bad range is refused; a good one is saved and then cleared
	if rc, _, _ := c.call("PUT", "/api/fw/management-access", map[string]any{"cidrs": []string{"nonsense"}}); rc == 200 {
		t.Fatal("bad range accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/fw/management-access", map[string]any{"networks": []string{"Home"}, "cidrs": []string{"10.9.9.0/24"}}); rc != 200 {
		t.Fatal("limit not saved")
	}
	_, _, raw = c.call("GET", "/api/fw/protected", nil)
	if !strings.Contains(string(raw), "192.168.1.0/24") || !strings.Contains(string(raw), "Only these addresses may manage") {
		t.Fatalf("limit not shown: %s", raw)
	}
	if rc, _, _ := c.call("PUT", "/api/fw/management-access", map[string]any{"networks": []string{}, "cidrs": []string{}}); rc != 200 {
		t.Fatal("limit not cleared")
	}
	_, _, raw = c.call("GET", "/api/fw/protected", nil)
	if strings.Contains(string(raw), "Only these addresses may manage") {
		t.Fatal("limit still shown")
	}
	rc, _, raw := c.call("POST", "/api/fw/access-check", map[string]any{})
	if rc != 200 || !strings.Contains(string(raw), "can manage this device") {
		t.Fatalf("check: %d %s", rc, raw)
	}
}

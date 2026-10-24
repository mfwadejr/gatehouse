package app

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConsolePortChange(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	// refused: in use by the system, or invalid
	for _, p := range []int{22, 53, 0, 70000} {
		if rc, _, _ := c.call("POST", "/api/system/console-port", map[string]any{"port": p}); rc == 200 {
			t.Fatalf("port %d accepted", p)
		}
	}
	// refused: already taken on this machine
	busy, _ := net.Listen("tcp", ":0")
	defer busy.Close()
	bp := busy.Addr().(*net.TCPAddr).Port
	if rc, _, raw := c.call("POST", "/api/system/console-port", map[string]any{"port": bp}); rc == 200 || !strings.Contains(string(raw), "in use") {
		t.Fatalf("busy port accepted: %d %s", rc, raw)
	}
	free, _ := net.Listen("tcp", ":0")
	np := free.Addr().(*net.TCPAddr).Port
	free.Close()
	rc, _, raw := c.call("POST", "/api/system/console-port", map[string]any{"port": np})
	if rc != 200 {
		t.Fatalf("change: %d %s", rc, raw)
	}
	nc := &client{t: t, base: "http://127.0.0.1:" + strconv.Itoa(np), hc: c.hc, csrf: c.csrf}
	if rc, _, _ := nc.call("GET", "/api/health", nil); rc != 200 {
		t.Fatal("the new port does not answer")
	}
	// confirming from the old port proves nothing
	if rc, _, _ := c.call("POST", "/api/system/console-port/confirm", map[string]any{}); rc != 409 {
		t.Fatalf("confirmed from the old port: %d", rc)
	}
	if rc, _, raw := nc.call("POST", "/api/system/console-port/confirm", map[string]any{}); rc != 200 {
		t.Fatalf("confirm: %d %s", rc, raw)
	}
	if got := siteBoxSetting(t, nc, np); got != np {
		t.Fatalf("saved port %d", got)
	}
	// the old port closes shortly after
	for i := 0; i < 40; i++ {
		if conn, err := net.DialTimeout("tcp", strings.TrimPrefix(c.base, "http://"), 200*time.Millisecond); err != nil {
			return
		} else {
			conn.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the old port is still open")
}

func siteBoxSetting(t *testing.T, c *client, want int) int {
	t.Helper()
	_, m, _ := c.call("GET", "/api/system/console-port", nil)
	return int(m["port"].(float64))
}

func TestConsolePortCancel(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	free, _ := net.Listen("tcp", ":0")
	np := free.Addr().(*net.TCPAddr).Port
	free.Close()
	if rc, _, raw := c.call("POST", "/api/system/console-port", map[string]any{"port": np}); rc != 200 {
		t.Fatalf("change: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("POST", "/api/system/console-port/cancel", map[string]any{}); rc != 200 {
		t.Fatal("cancel failed")
	}
	for i := 0; i < 40; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(np), 200*time.Millisecond)
		if err != nil {
			if rc, _, _ := c.call("GET", "/api/health", nil); rc != 200 {
				t.Fatal("the original port stopped working")
			}
			return
		}
		conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the trial port is still open")
}

package app

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestSyslogLineFormat(t *testing.T) {
	l := syslogLine("warning", "gate", "a\nb", time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC))
	if l != "<132>1 2026-10-30T10:00:00Z gate gatehouse - - - a b" {
		t.Fatal(l)
	}
}

func TestSyslogSendsEventsAndRefusesBadConfig(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no udp")
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	if rc, _, _ := c.call("PUT", "/api/syslog", map[string]any{"on": true, "host": "", "port": 514, "proto": "udp"}); rc == 200 {
		t.Fatal("empty host accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/syslog", map[string]any{"on": true, "host": "127.0.0.1", "port": 0, "proto": "udp"}); rc == 200 {
		t.Fatal("port 0 accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/syslog", map[string]any{"on": true, "host": "127.0.0.1", "port": port, "proto": "udp", "firewall": false}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	buf := make([]byte, 2048)
	pc.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "now sending its logs") {
		t.Fatalf("no first message: %v %s", err, buf[:n])
	}
	a.event("tester", "warning", "Hello from a test", "detail")
	pc.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err = pc.ReadFrom(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "Hello from a test") {
		t.Fatalf("no event: %v %s", err, buf[:n])
	}
	// Off means nothing is sent.
	c.call("PUT", "/api/syslog", map[string]any{"on": false, "host": "127.0.0.1", "port": port, "proto": "udp"})
	a.event("tester", "info", "Silent", "")
	pc.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if n, _, err = pc.ReadFrom(buf); err == nil {
		t.Fatalf("sent while off: %s", buf[:n])
	}
}

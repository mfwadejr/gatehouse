package app

import (
	"net"
	"testing"
	"time"
)

func TestFlowExportSendsFinishedConnections(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
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
	if rc, _, _ := c.call("PUT", "/api/flows", map[string]any{"on": true, "host": "", "port": 4739, "format": "ipfix"}); rc == 200 {
		t.Fatal("empty host accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/flows", map[string]any{"on": true, "host": "127.0.0.1", "port": port, "format": "bogus"}); rc == 200 {
		t.Fatal("bad format accepted")
	}
	if rc, _, raw := c.call("PUT", "/api/flows", map[string]any{"on": true, "host": "127.0.0.1", "port": port, "format": "ipfix"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	line := "ipv4 2 tcp 6 100 ESTABLISHED src=192.168.1.20 dst=93.184.216.34 sport=50000 dport=443 packets=10 bytes=1500 src=93.184.216.34 dst=203.0.113.5 sport=443 dport=50000 packets=12 bytes=9000\n"
	fn.mu.Lock()
	fn.flowDump = line
	fn.mu.Unlock()
	t0 := time.Now()
	a.flowStep(t0)
	fn.mu.Lock()
	fn.flowDump = ""
	fn.mu.Unlock()
	a.flowStep(t0.Add(30 * time.Second))
	buf := make([]byte, 2048)
	pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil || n < 16 || buf[0] != 0 || buf[1] != 10 {
		t.Fatalf("no IPFIX packet: %v n=%d", err, n)
	}
	fn.mu.Lock()
	acc := append([]bool(nil), fn.flowAcct...)
	fn.mu.Unlock()
	if len(acc) == 0 || !acc[0] {
		t.Fatal("traffic counting was not switched on")
	}
	// Off switches counting back off and sends nothing more.
	c.call("PUT", "/api/flows", map[string]any{"on": false, "host": "127.0.0.1", "port": port, "format": "ipfix"})
	a.flowStep(t0.Add(time.Minute))
	fn.mu.Lock()
	last := fn.flowAcct[len(fn.flowAcct)-1]
	fn.mu.Unlock()
	if last {
		t.Fatal("counting should be off again")
	}
}

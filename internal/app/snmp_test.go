package app

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestSNMPSwitchOnAndOffAndFirewall(t *testing.T) {
	c, _, ff := siteBoxWith(t, "192.168.1.1/24", func(cfg *Config) { cfg.SNMPAddr = "127.0.0.1:0" })
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	rc, m, _ := c.call("GET", "/api/snmp", nil)
	if rc != 200 || m["listening"] == true {
		t.Fatalf("should start off: %d %v", rc, m)
	}
	if rc, _, raw := c.call("PUT", "/api/snmp", map[string]any{"on": true, "contact": "a\nb"}); rc == 200 {
		t.Fatalf("multi-line contact accepted: %s", raw)
	}
	if rc, _, raw := c.call("PUT", "/api/snmp", map[string]any{"on": true, "contact": "Marvin", "location": "Closet"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	_, m, _ = c.call("GET", "/api/snmp", nil)
	cfg := m["config"].(map[string]any)
	comm, _ := cfg["community"].(string)
	if len(comm) < 16 || strings.EqualFold(comm, "public") || m["listening"] != true {
		t.Fatalf("bad state: %v", m)
	}
	if !strings.Contains(ff.script, "udp dport 161 accept comment \"protected: management snmp\"") {
		t.Fatal("the firewall was not opened for management networks")
	}
	// Ask it something over UDP, like a monitoring tool would.
	a.snmp.mu.Lock()
	addr := a.snmp.pc.LocalAddr().String()
	a.snmp.mu.Unlock()
	cn, err := net.Dial("udp", addr)
	if err != nil {
		t.Skip("no udp")
	}
	defer cn.Close()
	req := snmpGet(comm, "1.3.6.1.2.1.1.1.0")
	cn.Write(req)
	buf := make([]byte, 2048)
	cn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := cn.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "Gatehouse") {
		t.Fatalf("no answer: %v %q", err, buf[:n])
	}
	cn.Write(snmpGet("public", "1.3.6.1.2.1.1.1.0"))
	cn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	if _, err := cn.Read(buf); err == nil {
		t.Fatal("answered the wrong community")
	}
	// A new community replaces the old one.
	c.call("PUT", "/api/snmp", map[string]any{"on": true, "new_community": true})
	_, m, _ = c.call("GET", "/api/snmp", nil)
	if m["config"].(map[string]any)["community"] == comm {
		t.Fatal("community did not change")
	}
	// Off closes it again and takes the firewall rule away.
	c.call("PUT", "/api/snmp", map[string]any{"on": false})
	_, m, _ = c.call("GET", "/api/snmp", nil)
	if m["listening"] == true || strings.Contains(ff.script, "management snmp") {
		t.Fatalf("still open: %v", m)
	}
}

// snmpGet builds a v2c GET by hand.
func snmpGet(comm, oid string) []byte {
	enc := []byte{0x2b, 6, 1, 2, 1, 1, 1, 0} // 1.3.6.1.2.1.1.1.0
	vb := append([]byte{0x30, byte(len(enc) + 4), 0x06, byte(len(enc))}, enc...)
	vb = append(vb, 0x05, 0x00)
	vbs := append([]byte{0x30, byte(len(vb))}, vb...)
	pdu := append([]byte{0x02, 1, 7, 0x02, 1, 0, 0x02, 1, 0}, vbs...)
	pduT := append([]byte{0xa0, byte(len(pdu))}, pdu...)
	body := append([]byte{0x02, 1, 1, 0x04, byte(len(comm))}, []byte(comm)...)
	body = append(body, pduT...)
	return append([]byte{0x30, byte(len(body))}, body...)
}

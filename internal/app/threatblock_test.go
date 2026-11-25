package app

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func sevLine(src, dst string, sev int) []byte {
	return []byte(fmt.Sprintf(`{"event_type":"alert","src_ip":%q,"dest_ip":%q,"dest_port":443,"proto":"TCP","alert":{"signature_id":%d,"signature":"ET TEST bad","category":"A Network Trojan was detected","severity":%d}}`, src, dst, 4000+sev, sev))
}

func TestThreatBlockingChoosesCarefully(t *testing.T) {
	a := newScopeApp(t)
	now := time.Now()
	a.Config.SetSetting(threatModeSetting, "watch")
	// Not switched on: nothing is blocked, however serious.
	a.threatIngest(sevLine("192.168.1.20", "203.0.113.9", 1), now)
	if n := len(a.threatBlockList(now)); n != 0 {
		t.Fatalf("blocked while blocking was off: %d", n)
	}
	a.Config.SetSetting(threatBlockSetting, "1")
	// Only a serious alert leads to a block.
	a.threatIngest(sevLine("192.168.1.20", "203.0.113.10", 2), now)
	if n := len(a.threatBlockList(now)); n != 0 {
		t.Fatalf("blocked a less serious alert: %d", n)
	}
	a.threatIngest(sevLine("192.168.1.20", "203.0.113.9", 1), now)
	rows := a.threatBlockList(now)
	if len(rows) != 1 || rows[0].IP != "203.0.113.9" {
		t.Fatalf("blocks: %+v", rows)
	}
	// The same address again is one block, with its time renewed.
	a.threatIngest(sevLine("192.168.1.20", "203.0.113.9", 1), now.Add(time.Minute))
	if n := len(a.threatBlockList(now)); n != 1 {
		t.Fatalf("duplicate block: %d", n)
	}
	// Our own side and ordinary inside addresses are never blocked, and an attack coming in blocks the sender.
	for _, ip := range []string{"192.168.1.1", "10.0.0.5", "127.0.0.1", "100.64.1.1", "169.254.1.1", "224.0.0.1", "::1", "2001:db8::1"} {
		if a.threatBlockable(ip) {
			t.Fatalf("%s should never be blocked", ip)
		}
	}
	a.threatIngest(sevLine("198.51.100.7", "192.168.1.20", 1), now)
	if n := len(a.threatBlockList(now)); n != 2 {
		t.Fatalf("inbound sender not blocked: %d", n)
	}
	// A DNS server in use is never blocked.
	a.Config.Exec(`INSERT INTO dns_upstreams(name,address,proto) VALUES('x','198.51.100.53:53','udp')`)
	if a.threatBlockable("198.51.100.53") {
		t.Fatal("an upstream DNS server was blockable")
	}
	// A block ends by itself.
	if n := len(a.threatBlockList(now.Add(2 * time.Hour))); n != 0 {
		t.Fatalf("blocks did not expire: %d", n)
	}
	// The firewall list carries the time left.
	f := a.threatBlockFill(now)
	if len(f.Elems) != 2 || !strings.Contains(f.Elems[0], " timeout ") {
		t.Fatalf("fill: %v", f.Elems)
	}
}

func TestThreatBlockingFirewallRules(t *testing.T) {
	c, _, ff := siteBoxWith(t, "192.168.1.1/24", nil)
	a := c.app
	if a == nil {
		t.Skip("no app")
	}
	a.Config.SetSetting(threatModeSetting, "watch")
	a.Config.SetSetting(threatBlockSetting, "1")
	if err := a.refreshFirewallQuiet("test"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ff.script, "set threat_block") || strings.Count(ff.script, `comment "threat: blocked address"`) != 3 {
		t.Fatalf("rules missing:\n%s", ff.script)
	}
	a.Config.SetSetting(threatBlockSetting, "")
	a.refreshFirewallQuiet("test")
	if strings.Contains(ff.script, "threat_block") {
		t.Fatal("block list stayed after blocking was switched off")
	}
	// Watching off also means no blocking, even if the switch was left on.
	a.Config.SetSetting(threatBlockSetting, "1")
	a.Config.SetSetting(threatModeSetting, "off")
	a.refreshFirewallQuiet("test")
	if strings.Contains(ff.script, "threat_block") {
		t.Fatal("blocking stayed while watching was off")
	}
}

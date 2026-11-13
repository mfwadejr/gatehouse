package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"nopewarden/internal/fw"
)

func mgmtFakes(t *testing.T, addr string, carrier bool) {
	oldPorts, oldAddr, oldArp := portsLister, mgmtAddrOf, arpTable
	portsLister = func() []map[string]any {
		return []map[string]any{{"name": "tp1"}, {"name": "tp2"}, {"name": "tp3"}}
	}
	mgmtAddrOf = func(dev string) (netip.Addr, bool) {
		if addr == "" {
			return netip.Addr{}, carrier
		}
		return netip.MustParseAddr(addr), carrier
	}
	arpTable = func() map[string]arpEntry { return map[string]arpEntry{} }
	t.Cleanup(func() { portsLister, mgmtAddrOf, arpTable = oldPorts, oldAddr, oldArp })
}

func TestMgmtPortValidationAndApply(t *testing.T) {
	mgmtFakes(t, "10.99.0.5", true)
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app

	for name, body := range map[string]map[string]any{
		"a port that does not exist":       {"iface": "nope9", "mode": "dhcp"},
		"an address that overlaps the LAN": {"iface": "tp3", "mode": "static", "cidr": "192.168.1.9/24"},
		"no mode":                          {"iface": "tp3", "mode": ""},
		"a gateway off the subnet":         {"iface": "tp3", "mode": "static", "cidr": "10.99.0.5/24", "gateway": "10.1.1.1"},
	} {
		if rc, _, raw := c.call("PUT", "/api/mgmt/port", body); rc == 200 {
			t.Fatalf("%s must be refused: %s", name, raw)
		}
	}
	if a.mgmtCfg().Iface != "" || len(fn.mgmtPlans) != 0 {
		t.Fatal("a refused setup must change nothing")
	}

	rc, m, raw := c.call("PUT", "/api/mgmt/port", map[string]any{"iface": "tp3", "mode": "static", "cidr": "10.99.0.5/24", "gateway": "10.99.0.1"})
	if rc != 200 {
		t.Fatalf("save: %d %s", rc, raw)
	}
	if len(fn.mgmtPlans) != 1 || fn.mgmtPlans[0].Dev != "tp3" || fn.mgmtPlans[0].CIDR != "10.99.0.5/24" {
		t.Fatalf("plan: %+v", fn.mgmtPlans)
	}
	if m["pending"] == nil {
		t.Fatalf("a changed port waits to be confirmed: %v", m)
	}
	// The fill carries the port name.
	mod, _ := fw.Load(a.Config)
	if f := a.setContents(mod, false)[fw.MgmtIfSet]; len(f.Elems) != 1 || f.Elems[0] != "tp3" {
		t.Fatalf("mgmt_if fill: %+v", f)
	}
	// A second change while one waits is refused.
	if rc, _, _ := c.call("PUT", "/api/mgmt/port", map[string]any{"iface": "tp3", "mode": "dhcp"}); rc == 200 {
		t.Fatal("a second change while one is pending must be refused")
	}
}

func TestMgmtPortConfirmAndRevert(t *testing.T) {
	mgmtFakes(t, "10.99.0.5", true)
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	c.call("PUT", "/api/mgmt/port", map[string]any{"iface": "tp3", "mode": "static", "cidr": "10.99.0.5/24"})

	// A request on some other address does not confirm.
	req := httptest.NewRequest("GET", "/api/x", nil)
	other := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 8443}
	a.mgmtSeen(req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, net.Addr(other))))
	if a.mgmtPendingGet() == nil {
		t.Fatal("a request on the LAN address must not confirm")
	}
	// One on the management address does.
	mine := &net.TCPAddr{IP: net.ParseIP("10.99.0.5"), Port: 8443}
	a.mgmtSeen(req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, net.Addr(mine))))
	if a.mgmtPendingGet() != nil {
		t.Fatal("a request on the management address must confirm")
	}

	// A fresh change that nobody confirms is undone after the window.
	if rc, _, raw := c.call("PUT", "/api/mgmt/port", map[string]any{"iface": "tp2", "mode": "dhcp"}); rc != 200 {
		t.Fatalf("second change: %d %s", rc, raw)
	}
	a.mgmtTick(time.Now().Add(2 * time.Minute))
	if a.mgmtPendingGet() != nil || a.mgmtCfg().Iface != "tp3" {
		t.Fatalf("an unconfirmed change must go back: %+v", a.mgmtCfg())
	}
	if last := fn.mgmtPlans[len(fn.mgmtPlans)-1]; last.Dev != "tp3" {
		t.Fatalf("the port must be set up as before: %+v", last)
	}
	// Removing the port clears it.
	c.call("PUT", "/api/mgmt/port", map[string]any{"iface": ""})
	if a.mgmtCfg().Iface != "" || fn.mgmtPlans[len(fn.mgmtPlans)-1].Dev != "" {
		t.Fatal("removing the port must clear it")
	}
}

func TestMgmtPortGateHysteresis(t *testing.T) {
	mgmtFakes(t, "10.99.0.5", true)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	a.Config.SetSetting(mgmtSetting, `{"iface":"tp3","mode":"static","cidr":"10.99.0.5/24","gateway":"10.99.0.1","close_lan":true}`)
	mgmtState.Lock()
	mgmtState.init, mgmtState.lanOpen, mgmtState.healthy = false, true, false
	mgmtState.healthySince, mgmtState.unhealthySince = time.Time{}, time.Time{}
	mgmtState.Unlock()

	t0 := time.Now()
	if !a.mgmtLanOpen() {
		t.Fatal("the LAN console starts open")
	}
	// No neighbour seen: unhealthy, so it stays open.
	a.mgmtTick(t0)
	a.mgmtTick(t0.Add(5 * time.Minute))
	if !a.mgmtLanOpen() {
		t.Fatal("an unproven port must never close the LAN")
	}
	arpTable = func() map[string]arpEntry { return map[string]arpEntry{"10.99.0.1": {MAC: "aa:bb", Dev: "tp3"}} }
	a.mgmtTick(t0.Add(6 * time.Minute))
	a.mgmtTick(t0.Add(6*time.Minute + 20*time.Second))
	if !a.mgmtLanOpen() {
		t.Fatal("it must wait half a minute of health before closing")
	}
	a.mgmtTick(t0.Add(6*time.Minute + 40*time.Second))
	if a.mgmtLanOpen() {
		t.Fatal("healthy long enough: the LAN console should close")
	}
	// A short blip does not reopen it.
	arpTable = func() map[string]arpEntry { return map[string]arpEntry{} }
	a.mgmtTick(t0.Add(7 * time.Minute))
	a.mgmtTick(t0.Add(7*time.Minute + 30*time.Second))
	if a.mgmtLanOpen() {
		t.Fatal("a short outage must not reopen the LAN")
	}
	a.mgmtTick(t0.Add(8*time.Minute + 5*time.Second))
	if !a.mgmtLanOpen() {
		t.Fatal("a minute of silence must open the LAN console again")
	}
	// The gate set follows.
	mod, _ := fw.Load(a.Config)
	if f := a.setContents(mod, false)[fw.LanGateSet]; len(f.Elems) == 0 {
		t.Fatalf("an open LAN console lists the console ports: %+v", f)
	}
}

func TestMgmtPortCloseLANNeedsTheManagementAddress(t *testing.T) {
	mgmtFakes(t, "10.99.0.5", true)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	c.call("PUT", "/api/mgmt/port", map[string]any{"iface": "tp3", "mode": "static", "cidr": "10.99.0.5/24"})
	c.app.Config.Exec(`DELETE FROM settings WHERE key=?`, mgmtPendingKV)
	rc, _, raw := c.call("PUT", "/api/mgmt/port", map[string]any{"iface": "tp3", "mode": "static", "cidr": "10.99.0.5/24", "close_lan": true})
	if rc == 200 || !strings.Contains(string(raw), "through the management port") {
		t.Fatalf("closing the LAN from the LAN must be refused: %d %s", rc, raw)
	}
}

// A connection through the management port is not part of any network, yet it must not be
// reported as "would no longer be allowed to manage" when the firewall is checked.
func TestSessionThroughManagementPortStaysSafe(t *testing.T) {
	a, _ := newSpeedApp(t)
	a.Config.SetSetting(mgmtSetting, `{"iface":"enp2s0","mode":"dhcp"}`)
	m := fw.DefaultModel([]string{"eth0"}, []string{"eth1"})
	if _, safe, _ := a.sessionSafeVia(m, "enp2s0", "10.9.0.5"); !safe {
		t.Fatal("the management port must stay able to manage")
	}
	if _, safe, _ := a.sessionSafeVia(m, "enp7s0", "10.7.0.5"); safe {
		t.Fatal("an interface in no management network must still be refused")
	}
	m.MgmtAllow = []string{"192.168.1.0/24"}
	if _, safe, _ := a.sessionSafeVia(m, "enp2s0", "10.9.0.5"); safe {
		t.Fatal("an address outside the limit on who may manage must still be refused")
	}
}

// The access check names the port the administrator is connected through; it used to come back empty.
func TestSessionCheckNamesThePort(t *testing.T) {
	a, _ := newSpeedApp(t)
	m := fw.DefaultModel([]string{"eth0"}, []string{"eth1"})
	checked, safe, via := a.sessionSafeVia(m, "eth1", "192.168.1.20")
	if !checked || !safe || via != "eth1" {
		t.Fatalf("checked=%v safe=%v via=%q", checked, safe, via)
	}
}

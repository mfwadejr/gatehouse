package app

import (
	"net"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/ha"
)

func waitHA(t *testing.T, what string, d time.Duration, f func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func applyBoth(t *testing.T, c *client) {
	t.Helper()
	if rc, _, raw := c.call("POST", "/api/net/apply", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("net apply: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/net/confirm", map[string]string{"token": "ntok"}); rc != 200 {
		t.Fatalf("net confirm: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/fw/apply", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("fw apply: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/fw/confirm", map[string]string{"token": "tok123"}); rc != 200 {
		t.Fatalf("fw confirm: %d %s", rc, raw)
	}
}

func TestHighAvailabilityPairing(t *testing.T) {
	fakePorts(t)
	pc, pfn, _ := siteBox(t, "192.168.1.1/24")
	sc, sfn, _ := siteBox(t, "192.168.1.1/24")
	P, S := pc.app, sc.app
	P.haListen, S.haListen = "127.0.0.1:0", "127.0.0.1:0"
	P.haActiveFn = func() bool { return true }
	S.haActiveFn = func() bool { return false }
	S.haOwnsFn = func(a string) bool { return a == "192.168.1.3" }
	ha.DialMap = func(addr string) string {
		h, port, _ := net.SplitHostPort(addr)
		switch h {
		case "192.168.1.2":
			if b, ok := P.haBound.Load().(string); ok {
				return b
			}
			return "127.0.0.1:1"
		case "192.168.1.3":
			if b, ok := S.haBound.Load().(string); ok {
				return b
			}
			return "127.0.0.1:1"
		}
		_ = port
		return addr
	}
	t.Cleanup(func() { ha.DialMap = nil })

	// The listeners start once a pairing exists; the primary starts one first.
	rc, m, raw := pc.call("POST", "/api/ha/start", map[string]string{"own_addr": "192.168.1.2", "standby_addr": "192.168.1.3"})
	if rc != 200 {
		t.Fatalf("start: %d %s", rc, raw)
	}
	code := m["code"].(string)
	waitHA(t, "the primary's link", 8*time.Second, func() bool { _, ok := P.haBound.Load().(string); return ok })
	// The primary's networks are staged: its own address, and the network's address becomes shared.
	var own string
	P.Config.QueryRow(`SELECT ipv4_cidr FROM interfaces WHERE name='eth1'`).Scan(&own)
	if own != "192.168.1.2/24" {
		t.Fatalf("the primary's own address should be staged: %q", own)
	}
	if P.Config.Setting("ha.peer", "") != "192.168.1.3" {
		t.Fatal("the firewall must be told about the partner")
	}
	// A second start is refused, and bridge mode cannot be combined.
	if rc, _, _ := pc.call("POST", "/api/ha/start", map[string]string{"own_addr": "192.168.1.2", "standby_addr": "192.168.1.3"}); rc == 200 {
		t.Fatal("a second start must be refused")
	}

	// The backup looks at the primary, then joins.
	if rc, _, raw := sc.call("POST", "/api/ha/discover", map[string]string{"code": "GATEHOUSE-PAIR-1:nonsense"}); rc == 200 {
		t.Fatalf("a bad code must be refused: %s", raw)
	}
	rc, m, raw = sc.call("POST", "/api/ha/discover", map[string]string{"code": code})
	if rc != 200 {
		t.Fatalf("discover: %d %s", rc, raw)
	}
	if m["mapping"] == nil || m["refused"] != nil {
		t.Fatalf("mapping: %v", m)
	}
	waitHA(t, "the backup's link", 8*time.Second, func() bool { _, ok := S.haBound.Load().(string); return ok })
	rc, _, raw = sc.call("POST", "/api/ha/join", map[string]any{"code": code, "keep": map[string]string{}})
	if rc != 200 {
		t.Fatalf("join: %d %s", rc, raw)
	}
	if st := P.haLoad(); st == nil || st.PeerFP == "" {
		t.Fatal("the primary did not pin the backup")
	}
	if P.haLoad().PeerPort != S.haPort() {
		t.Fatal("the primary must know the backup's link port")
	}
	// The primary sends its settings; the backup takes them with its own address.
	waitHA(t, "the settings to arrive", 25*time.Second, func() bool { s := S.haLoad(); return s != nil && s.SyncedAt > 0 })
	// The accounts came across with the settings, so the backup asks for the same sign-in again.
	_, lm, _ := sc.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	if lm["csrf"] == nil {
		t.Fatalf("the same account must work on the backup: %v", lm)
	}
	sc.csrf = lm["csrf"].(string)
	var sOwn string
	S.Config.QueryRow(`SELECT ipv4_cidr FROM interfaces WHERE name=?`, S.haLoad().MgmtIface).Scan(&sOwn)
	if sOwn != "192.168.1.3/24" {
		t.Fatalf("the backup must keep its own address after the copy: %q", sOwn)
	}
	if S.Config.Setting("ha.peer", "") != "192.168.1.2" {
		t.Fatalf("a copy of the settings must not overwrite which unit is which: %q", S.Config.Setting("ha.peer", ""))
	}
	// Starting the shared-address service is refused until the changes are applied.
	if rc, _, _ := pc.call("POST", "/api/ha/activate", map[string]any{}); rc == 200 {
		t.Fatal("activate must wait for the network and firewall changes")
	}
	applyBoth(t, pc)
	applyBoth(t, sc)
	if rc, _, raw := pc.call("POST", "/api/ha/activate", map[string]any{}); rc != 200 {
		t.Fatalf("primary activate: %d %s", rc, raw)
	}
	if rc, _, raw := sc.call("POST", "/api/ha/activate", map[string]any{}); rc != 200 {
		t.Fatalf("backup activate: %d %s", rc, raw)
	}
	pfn.mu.Lock()
	pp := pfn.haPlans
	pfn.mu.Unlock()
	sfn.mu.Lock()
	sp := sfn.haPlans
	sfn.mu.Unlock()
	if len(pp) != 1 || !strings.Contains(pp[0].Keepalived, "priority 150") || !strings.Contains(pp[0].Keepalived, "192.168.1.1/24 dev eth1") || !pp[0].Enabled {
		t.Fatalf("primary services: %+v", pp)
	}
	if len(sp) != 1 || !strings.Contains(sp[0].Keepalived, "priority 100") || !strings.Contains(sp[0].Keepalived, "unicast_src_ip 192.168.1.3") || strings.Contains(sp[0].Keepalived, "dev eth1") || len(sp[0].WanDevs) != 1 {
		t.Fatalf("backup services: %+v", sp)
	}

	// The standby is read-only; the active unit is not.
	if !S.haStandbyNow() || P.haStandbyNow() {
		t.Fatal("roles")
	}
	if rc, _, raw := sc.call("PUT", "/api/fw/rules", map[string]any{}); rc != 409 || !strings.Contains(string(raw), "standby") {
		t.Fatalf("a change on the standby must be refused in words: %d %s", rc, raw)
	}
	if rc, _, _ := sc.call("GET", "/api/ha", nil); rc != 200 {
		t.Fatal("the standby must show status")
	}
	// Handing over: the standby asks the active unit to step aside by restarting its service.
	if rc, _, raw := sc.call("POST", "/api/ha/failover", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("handover: %d %s", rc, raw)
	}
	pfn.mu.Lock()
	n := len(pfn.haPlans)
	pfn.mu.Unlock()
	if n != 2 {
		t.Fatalf("the active unit should have restarted its service once: %d", n)
	}
	if rc, _, _ := pc.call("POST", "/api/ha/failover", map[string]string{"code": ""}); rc == 200 {
		t.Fatal("the active unit cannot take over from itself")
	}
	// Status shows the partner.
	_, st, _ := pc.call("GET", "/api/ha", nil)
	if peer, _ := st["peer"].(map[string]any); peer == nil || peer["reachable"] != true {
		t.Fatalf("status: %v", st)
	}

	// Removing the pairing returns both units to being single.
	if rc, _, raw := pc.call("POST", "/api/ha/unpair", map[string]any{}); rc != 200 {
		t.Fatalf("unpair: %d %s", rc, raw)
	}
	if P.haLoad() != nil || P.Config.Setting("ha.peer", "") != "" {
		t.Fatal("the primary should be single again")
	}
	waitHA(t, "the backup to leave too", 5*time.Second, func() bool { return S.haLoad() == nil })
	sfn.mu.Lock()
	last := sfn.haPlans[len(sfn.haPlans)-1]
	sfn.mu.Unlock()
	if last.Enabled {
		t.Fatal("leaving must switch the services off")
	}
	if S.haStandbyNow() {
		t.Fatal("a unit that left is not a standby")
	}
}

func TestHARefusesBackupWithFewerPorts(t *testing.T) {
	fakePorts(t)
	pc, _, _ := siteBox(t, "192.168.1.1/24")
	sc, _, _ := siteBox(t, "192.168.1.1/24")
	P, S := pc.app, sc.app
	P.haListen, S.haListen = "127.0.0.1:0", "127.0.0.1:0"
	S.haOwnsFn = func(a string) bool { return true }
	ha.DialMap = func(addr string) string {
		if strings.HasPrefix(addr, "192.168.1.2:") {
			if b, ok := P.haBound.Load().(string); ok {
				return b
			}
			return "127.0.0.1:1"
		}
		return addr
	}
	t.Cleanup(func() { ha.DialMap = nil })
	_, m, _ := pc.call("POST", "/api/ha/start", map[string]string{"own_addr": "192.168.1.2", "standby_addr": "192.168.1.3"})
	code := m["code"].(string)
	waitHA(t, "link", 8*time.Second, func() bool { _, ok := P.haBound.Load().(string); return ok })
	// The backup has a single wired port.
	old := portsLister
	portsLister = func() []map[string]any {
		return []map[string]any{{"name": "only", "wifi": false, "link": true, "speed": "1000"}}
	}
	t.Cleanup(func() { portsLister = old })
	_, m, _ = sc.call("POST", "/api/ha/discover", map[string]string{"code": code})
	if r, _ := m["refused"].(string); !strings.Contains(r, "at least as many") {
		t.Fatalf("a backup with too few ports must be refused in words: %v", m)
	}
	if rc, _, _ := sc.call("POST", "/api/ha/join", map[string]any{"code": code}); rc == 200 {
		t.Fatal("joining with too few ports must be refused")
	}
	if P.haLoad().PeerFP != "" {
		t.Fatal("the primary must not pin a refused backup")
	}
}

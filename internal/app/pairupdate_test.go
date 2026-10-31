package app

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/ha"
	"gatehouse/internal/update"
)

// pairedUnits sets up two running, activated units that can talk to each other, both on 0.1.5.
func pairedUnits(t *testing.T) (pc, sc *client, pfn, sfn *fakeNet) {
	t.Helper()
	fakePorts(t)
	key, _ := update.ParsePublicKey(updTestPub)
	tune := func(cfg *Config) { cfg.Version, cfg.UpdateKey, cfg.UpdateState = "0.1.5", key, t.TempDir() }
	pc, pfn, _ = siteBoxWith(t, "192.168.1.1/24", tune)
	sc, sfn, _ = siteBoxWith(t, "192.168.1.1/24", tune)
	P, S := pc.app, sc.app
	P.haListen, S.haListen = "127.0.0.1:0", "127.0.0.1:0"
	P.haActiveFn = func() bool { return true }
	S.haActiveFn = func() bool { return false }
	S.haOwnsFn = func(a string) bool { return a == "192.168.1.3" }
	ha.DialMap = func(addr string) string {
		h, _, _ := net.SplitHostPort(addr)
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
		return addr
	}
	t.Cleanup(func() { ha.DialMap = nil })
	rc, m, raw := pc.call("POST", "/api/ha/start", map[string]string{"own_addr": "192.168.1.2", "standby_addr": "192.168.1.3"})
	if rc != 200 {
		t.Fatalf("start: %d %s", rc, raw)
	}
	code := m["code"].(string)
	waitHA(t, "primary link", 8*time.Second, func() bool { _, ok := P.haBound.Load().(string); return ok })
	if rc, _, raw := sc.call("POST", "/api/ha/discover", map[string]string{"code": code}); rc != 200 {
		t.Fatalf("discover: %d %s", rc, raw)
	}
	waitHA(t, "backup link", 8*time.Second, func() bool { _, ok := S.haBound.Load().(string); return ok })
	if rc, _, raw := sc.call("POST", "/api/ha/join", map[string]any{"code": code, "keep": map[string]string{}}); rc != 200 {
		t.Fatalf("join: %d %s", rc, raw)
	}
	waitHA(t, "settings", 25*time.Second, func() bool { s := S.haLoad(); return s != nil && s.SyncedAt > 0 })
	_, lm, _ := sc.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	sc.csrf = lm["csrf"].(string)
	applyBoth(t, pc)
	applyBoth(t, sc)
	for _, c := range []*client{pc, sc} {
		if rc, _, raw := c.call("POST", "/api/ha/activate", map[string]any{}); rc != 200 {
			t.Fatalf("activate: %d %s", rc, raw)
		}
	}
	return
}

func fastHooks(pkg func() ([]byte, string)) pairHooks {
	return pairHooks{pollEvery: 20 * time.Millisecond, spareWait: 6 * time.Second, takeWait: 4 * time.Second,
		pkg: func(ctx context.Context) ([]byte, string, string, error) { d, s := pkg(); return d, s, "0.2.0", nil }}
}

func TestPairUpdateUpdatesTheSpareFirstThenHandsOverThenTheOtherUnit(t *testing.T) {
	pc, sc, pfn, sfn := pairedUnits(t)
	P, S := pc.app, sc.app
	data, sig := updPackage(t, "0.2.0", true)
	P.pairHooks = fastHooks(func() ([]byte, string) { return data, sig })

	// The spare cannot start it; the active unit says where to go.
	if _, err := S.startPairUpdate("test"); err == nil || !strings.Contains(err.Error(), "active unit") {
		t.Fatalf("the spare must send you to the active unit: %v", err)
	}
	if _, err := P.startPairUpdate("test"); err != nil {
		t.Fatal(err)
	}
	// Step 1: the spare receives and starts installing; the active unit is untouched so far.
	waitFor(t, "the spare to be given the package", func() bool { return strings.Contains(sfn.updateGiven(), "0.2.0") })
	if pfn.updateGiven() != "" {
		t.Fatal("the active unit must not update before the spare works")
	}
	pfn.mu.Lock()
	before := len(pfn.haPlans)
	pfn.mu.Unlock()
	// The spare restarts on the new version.
	S.Cfg.Version = "0.2.0"
	// The active unit steps aside; the spare takes over.
	waitFor(t, "the active unit to hand over", func() bool { pfn.mu.Lock(); defer pfn.mu.Unlock(); return len(pfn.haPlans) > before })
	P.haActiveFn, S.haActiveFn = func() bool { return false }, func() bool { return true }
	// Last: the unit that was active updates itself.
	waitFor(t, "the old active unit to start its own update", func() bool { return strings.Contains(pfn.updateGiven(), "0.2.0") })
	if p := P.pairLoad(); p == nil || p.Phase != "self" {
		t.Fatalf("phase before restart: %+v", p)
	}
	if steps, _ := P.pairSteps(); len(steps) == 0 || steps[len(steps)-2].State != "active" {
		t.Fatalf("progress list: %+v", steps)
	}
	// After its restart on the new version the run finishes.
	P.Cfg.Version = "0.2.0"
	P.pairResume()
	if p := P.pairLoad(); p.Phase != "done" {
		t.Fatalf("not finished: %+v", p)
	}
}

func TestPairUpdateStopsWithoutTouchingTheActiveUnitWhenTheSpareRefuses(t *testing.T) {
	pc, _, pfn, _ := pairedUnits(t)
	P := pc.app
	data, sig := updPackage(t, "0.2.0", false) // wrong signature: the spare must refuse it
	P.pairHooks = fastHooks(func() ([]byte, string) { return data, sig })
	pfn.mu.Lock()
	before := len(pfn.haPlans)
	pfn.mu.Unlock()
	if _, err := P.startPairUpdate("test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the run to stop", func() bool { p := P.pairLoad(); return p != nil && p.Phase == "failed" })
	p := P.pairLoad()
	if !strings.Contains(p.Error, "nothing was changed on this unit") {
		t.Fatalf("plain reason expected: %s", p.Error)
	}
	pfn.mu.Lock()
	after := len(pfn.haPlans)
	pfn.mu.Unlock()
	if after != before || pfn.updateGiven() != "" {
		t.Fatal("the active unit was touched")
	}
}

func TestHeartbeatCableIsOptionalAndUsedOnlyWhileConnected(t *testing.T) {
	pc, sc, pfn, sfn := pairedUnits(t)
	old := portsLister
	portsLister = func() []map[string]any {
		return []map[string]any{{"name": "tp1", "wifi": false}, {"name": "tp2", "wifi": false}, {"name": "tp3", "wifi": false}, {"name": "tpw", "wifi": true}}
	}
	t.Cleanup(func() { portsLister = old })
	P, S := pc.app, sc.app
	if st := P.haLoad(); st.HeartDev != "" || strings.Contains(st.peerHost(), "10.255.") {
		t.Fatal("the cable must be off until chosen")
	}
	// A Wi-Fi card and a port that is already in use cannot be the cable.
	for _, bad := range []string{"tpw", "eth1", "nonsense"} {
		if rc, _, _ := pc.call("PUT", "/api/ha/heart", map[string]string{"dev": bad}); rc == 200 {
			t.Fatalf("%s accepted as the cable", bad)
		}
	}
	connected := false
	old2 := heartCarrier
	heartCarrier = func(string) bool { return connected }
	t.Cleanup(func() { heartCarrier = old2 })
	first := func(c *client) string {
		_, m, _ := c.call("GET", "/api/ha", nil)
		ch, _ := m["heart_choices"].([]any)
		if len(ch) == 0 {
			t.Fatalf("no free port offered for the cable: %v", m)
		}
		return ch[0].(string)
	}
	pd, sd := first(pc), first(sc)
	if rc, _, raw := pc.call("PUT", "/api/ha/heart", map[string]string{"dev": pd}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, raw := sc.call("PUT", "/api/ha/heart", map[string]string{"dev": sd}); rc != 200 {
		t.Fatalf("standby: %d %s", rc, raw)
	}
	pfn.mu.Lock()
	plan := pfn.haPlans[len(pfn.haPlans)-1]
	pfn.mu.Unlock()
	sfn.mu.Lock()
	splan := sfn.haPlans[len(sfn.haPlans)-1]
	sfn.mu.Unlock()
	if plan.HeartDev != pd || !strings.HasSuffix(plan.HeartCIDR, "/30") || plan.HeartCIDR == splan.HeartCIDR {
		t.Fatalf("each unit needs its own cable address: %+v / %+v", plan, splan)
	}
	if !strings.Contains(plan.Conntrackd, "Interface "+pd) {
		t.Fatal("the connection copy should use the cable")
	}
	// The firewall lets the partner in on the cable only.
	if m := P.Config.Setting("ha.heart", ""); !strings.HasPrefix(m, pd+"|10.255.") {
		t.Fatalf("firewall setting: %q", m)
	}
	// Used while connected, and the shared network is the fallback when it is not.
	st := P.haLoad()
	if strings.Contains(st.peerHost(), "10.255.") {
		t.Fatal("an unplugged cable must not be used")
	}
	connected = true
	if !strings.HasPrefix(st.peerHost(), "10.255.") {
		t.Fatalf("a connected cable should be used: %s", st.peerHost())
	}
	_, status, _ := pc.call("GET", "/api/ha", nil)
	if h, _ := status["heart"].(map[string]any); h == nil || h["connected"] != true {
		t.Fatalf("status: %v", status["heart"])
	}
	// Removing it puts everything back on the shared network.
	if rc, _, _ := pc.call("PUT", "/api/ha/heart", map[string]string{"dev": ""}); rc != 200 {
		t.Fatal("remove failed")
	}
	if P.Config.Setting("ha.heart", "") != "" || strings.Contains(P.haLoad().peerHost(), "10.255.") {
		t.Fatal("the cable was not removed")
	}
	_ = S
}

func TestWarmStandbyKeepsTheInternetPortReadyAndCopiesTheAddress(t *testing.T) {
	pc, sc, pfn, sfn := pairedUnits(t)
	P, S := pc.app, sc.app
	old := haReadMAC
	haReadMAC = func(dev string) string { return "aa:bb:cc:00:11:22" }
	t.Cleanup(func() { haReadMAC = old })
	// Off: the spare's internet port is taken down, exactly as before.
	sfn.mu.Lock()
	first := sfn.haPlans[len(sfn.haPlans)-1]
	sfn.mu.Unlock()
	if len(first.WarmDevs) != 0 || len(first.WanDevs) != 1 {
		t.Fatalf("off should be the earlier behaviour: %+v", first)
	}
	// Only the active unit can switch it; the spare is told to use the active unit.
	if rc, _, _ := sc.call("PUT", "/api/ha/warm", map[string]bool{"on": true}); rc == 200 {
		t.Fatal("the spare accepted the change")
	}
	if rc, _, raw := pc.call("PUT", "/api/ha/warm", map[string]bool{"on": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	pfn.mu.Lock()
	plan := pfn.haPlans[len(pfn.haPlans)-1]
	pfn.mu.Unlock()
	if len(plan.WarmDevs) != 1 || len(plan.WanDevs) != 0 {
		t.Fatalf("a plain connection should be kept silent, not down: %+v", plan)
	}
	// The setting reaches the other unit with the next copy, then it learns the address to copy.
	waitHA(t, "the setting to reach the spare", 25*time.Second, func() bool { return S.haWarmOn() })
	S.haWarmRefresh(S.haLoad())
	sfn.mu.Lock()
	sp := sfn.haPlans[len(sfn.haPlans)-1]
	sfn.mu.Unlock()
	if len(sp.WarmDevs) != 1 || len(sp.WanMACs) != 1 || !strings.HasSuffix(sp.WanMACs[0], " aa:bb:cc:00:11:22") {
		t.Fatalf("the spare should copy the active unit's address: %+v", sp)
	}
	if !strings.HasPrefix(sp.WanMACs[0], sp.WarmDevs[0]+" ") {
		t.Fatalf("the spare must use its own port name: %v", sp.WanMACs)
	}
	// Static connections announce their address; PPPoE and wireless ones are left as before.
	P.Config.Exec(`INSERT INTO wan_links(name,iface,type,enabled,priority,ipv4_addr,ipv4_gw) VALUES('Fixed','eth5','static',1,2,'203.0.113.9/29','203.0.113.1'),('Login','eth6','pppoe',1,3,'','')`)
	sp2 := P.haWanSplit(true)
	if len(sp2.Static) != 1 || sp2.Static[0] != "eth5 203.0.113.9" {
		t.Fatalf("static: %+v", sp2)
	}
	down := strings.Join(sp2.Down, ",")
	if !strings.Contains(down, "ppp-login") || strings.Contains(down, "eth5") {
		t.Fatalf("a login connection keeps the earlier behaviour: %+v", sp2)
	}
}

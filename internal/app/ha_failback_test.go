package app

import (
	"strings"
	"testing"
	"time"

	"gatehouse/internal/ha"
)

func fastFailback(t *testing.T) {
	ow, op, os := failbackWait, failbackPoll, failbackSettle
	failbackWait, failbackPoll, failbackSettle = 4*time.Second, 20*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { failbackWait, failbackPoll, failbackSettle = ow, op, os })
}

func eventSeen(a *App, title string) bool {
	var n int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE title=?`, title).Scan(&n)
	return n > 0
}

// failedOver makes the standby the unit that runs the network, as after the Primary failed, and
// brings the Primary back as the standby with the same settings.
func failedOver(t *testing.T) (pc, sc *client, pfn, sfn *fakeNet) {
	pc, sc, pfn, sfn = pairedUnits(t)
	P, S := pc.app, sc.app
	P.haActiveFn = func() bool { return false }
	S.haActiveFn = func() bool { return true }
	if rc, _, raw := sc.call("POST", "/api/ha/sync", map[string]any{}); rc != 200 {
		t.Fatalf("sync from the unit now running the network: %d %s", rc, raw)
	}
	waitHA(t, "the Primary to have the same settings", 10*time.Second, func() bool {
		return P.haLoad().Seq == S.haLoad().Seq && S.haLoad().Seq > 1
	})
	return
}

func TestFailbackIsOfferedOnlyOnceThePrimaryIsBackAndHasTheSettings(t *testing.T) {
	pc, sc, _, _ := pairedUnits(t)
	// Normal running: the Primary runs the network, so there is nothing to offer anywhere.
	for _, c := range []*client{pc, sc} {
		if _, st, _ := c.call("GET", "/api/ha", nil); st["failback"] != nil {
			t.Fatalf("no prompt while the Primary is running the network: %v", st["failback"])
		}
	}
	pc, sc, _, _ = failedOver(t)
	_, st, _ := sc.call("GET", "/api/ha", nil)
	fb, _ := st["failback"].(map[string]any)
	if fb == nil || fb["ready"] != true || st["primary"] != false {
		t.Fatalf("the unit running the network should offer going back: %v", st)
	}
	// The returning Primary, which is now the standby, offers nothing and keeps its settings read-only.
	if _, pst, _ := pc.call("GET", "/api/ha", nil); pst["failback"] != nil || pst["primary"] != true {
		t.Fatalf("the standby Primary has nothing to offer: %v", pst)
	}
	// Still catching up: the choice is greyed out with the reason in words, and a request is refused.
	pc.app.haUpdate(func(x *haLocal) { x.Seq = 0 })
	_, st, _ = sc.call("GET", "/api/ha", nil)
	fb, _ = st["failback"].(map[string]any)
	if fb == nil || fb["ready"] != false || !strings.Contains(fb["why"].(string), "copying") {
		t.Fatalf("expected a plain reason: %v", fb)
	}
	rc, _, raw := sc.call("POST", "/api/ha/failback", map[string]string{"code": ""})
	if rc == 200 || !strings.Contains(string(raw), "copying") {
		t.Fatalf("an unready failback must be refused in words: %d %s", rc, raw)
	}
}

func TestFailbackSendsTheLatestSettingsThenStepsAsideAndNothingElseMoves(t *testing.T) {
	fastFailback(t)
	pc, sc, _, sfn := failedOver(t)
	P, S := pc.app, sc.app
	sfn.mu.Lock()
	before := len(sfn.haPlans)
	sfn.mu.Unlock()
	// A setting changed on the unit that is running the network must reach the Primary first.
	S.Config.SetSetting("system.note", "changed while the spare ran")
	if rc, _, raw := sc.call("POST", "/api/ha/failback", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("failback: %d %s", rc, raw)
	}
	waitHA(t, "this unit to step aside", 5*time.Second, func() bool {
		sfn.mu.Lock()
		defer sfn.mu.Unlock()
		return len(sfn.haPlans) == before+1
	})
	if got := P.Config.Setting("system.note", ""); got != "changed while the spare ran" {
		t.Fatalf("the Primary must have the latest settings before taking over: %q", got)
	}
	// The Primary takes over; the unit that stepped aside is told so in the log and nothing else moved.
	P.haActiveFn = func() bool { return true }
	S.haActiveFn = func() bool { return false }
	waitHA(t, "the log entry", 5*time.Second, func() bool { return eventSeen(S, "The Primary unit is running the network again") })
	// Going back again from the Primary is refused: it is the Primary and is running the network.
	if rc, _, _ := pc.call("POST", "/api/ha/failback", map[string]string{"code": ""}); rc == 200 {
		t.Fatal("the Primary has nothing to give back")
	}
}

func TestFailbackThatTheOtherUnitDoesNotTakeLeavesThisUnitRunningAndSaysSo(t *testing.T) {
	fastFailback(t)
	failbackWait = 600 * time.Millisecond
	_, sc, _, _ := failedOver(t)
	if rc, _, raw := sc.call("POST", "/api/ha/failback", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("failback: %d %s", rc, raw)
	}
	waitHA(t, "the plain warning", 5*time.Second, func() bool { return eventSeen(sc.app, "The Primary unit did not take over") })
	// Still the unit running the network, and still offered the same choice.
	if _, st, _ := sc.call("GET", "/api/ha", nil); st["active"] != true {
		t.Fatalf("this unit must carry on: %v", st)
	}
}

func TestPromotingTheUnitRunningTheNetworkSwapsThePrimaryOnBothWithoutRestartingAnything(t *testing.T) {
	pc, sc, pfn, sfn := failedOver(t)
	P, S := pc.app, sc.app
	if rc, _, _ := pc.call("POST", "/api/ha/promote", map[string]string{"code": ""}); rc == 200 {
		t.Fatal("the Primary cannot be promoted to Primary")
	}
	sfn.mu.Lock()
	sBefore := len(sfn.haPlans)
	sfn.mu.Unlock()
	pfn.mu.Lock()
	pBefore := len(pfn.haPlans)
	pfn.mu.Unlock()
	if rc, _, raw := sc.call("POST", "/api/ha/promote", map[string]string{"code": ""}); rc != 200 {
		t.Fatalf("promote: %d %s", rc, raw)
	}
	if !S.haLoad().isPreferred() || P.haLoad().isPreferred() {
		t.Fatal("the spare is now the Primary on both units")
	}
	if S.haLoad().Prefer != ha.Standby || P.haLoad().Prefer != ha.Standby {
		t.Fatal("both units must agree")
	}
	sfn.mu.Lock()
	sp := sfn.haPlans[sBefore:]
	sfn.mu.Unlock()
	pfn.mu.Lock()
	pp := pfn.haPlans[pBefore:]
	pfn.mu.Unlock()
	if len(sp) != 1 || !sp[0].ConfOnly || !strings.Contains(sp[0].Keepalived, "priority 150") {
		t.Fatalf("the new Primary takes the higher priority, without a restart: %+v", sp)
	}
	if len(pp) != 1 || !pp[0].ConfOnly || !strings.Contains(pp[0].Keepalived, "priority 100") {
		t.Fatalf("the old Primary takes the lower one, without a restart: %+v", pp)
	}
	// Nothing is offered any more: the unit running the network is the Primary.
	if _, st, _ := sc.call("GET", "/api/ha", nil); st["failback"] != nil || st["primary"] != true {
		t.Fatalf("no prompt after promoting: %v", st)
	}
	// Later keepalived writes keep the choice.
	if err := S.haInstall(S.haLoad()); err != nil {
		t.Fatal(err)
	}
	sfn.mu.Lock()
	last := sfn.haPlans[len(sfn.haPlans)-1]
	sfn.mu.Unlock()
	if last.ConfOnly || !strings.Contains(last.Keepalived, "priority 150") {
		t.Fatalf("a full install must keep the choice: %+v", last)
	}
}

func TestPromoteChangesNothingWhenTheOtherUnitCannotBeTold(t *testing.T) {
	_, sc, _, sfn := failedOver(t)
	S := sc.app
	// Break the link to the other unit.
	old := ha.DialMap
	ha.DialMap = func(addr string) string { return "127.0.0.1:1" }
	t.Cleanup(func() { ha.DialMap = old })
	sfn.mu.Lock()
	before := len(sfn.haPlans)
	sfn.mu.Unlock()
	rc, _, raw := sc.call("POST", "/api/ha/promote", map[string]string{"code": ""})
	if rc == 200 || !strings.Contains(string(raw), "nothing was changed") {
		t.Fatalf("expected a plain refusal: %d %s", rc, raw)
	}
	if S.haLoad().isPreferred() {
		t.Fatal("nothing may change on this unit either")
	}
	sfn.mu.Lock()
	after := len(sfn.haPlans)
	sfn.mu.Unlock()
	if after != before {
		t.Fatal("nothing may be written")
	}
}

func TestSettingsFromTheSpareFitThePrimarysPorts(t *testing.T) {
	ms := []ha.Match{{Role: "network:a", Label: "Network a", Primary: "eth0", Backup: "enp3s0", Warning: "slower"}, {Primary: "eth1", Backup: "enp4s0"}}
	got := decodeMap(encodeMap(ms))
	if len(got) != 2 || got[0].Label != "" || got[0].Warning != "" {
		t.Fatalf("only the port names travel: %+v", got)
	}
	back := swapMap(got)
	if ha.Rename("enp3s0", back) != "eth0" || ha.Rename("enp4s0", back) != "eth1" || ha.Rename("other", back) != "other" {
		t.Fatalf("the spare's port names must turn back into the Primary's: %+v", back)
	}
	if decodeMap("not base64 !!") != nil || decodeMap("") != nil {
		t.Fatal("damaged input means no renaming")
	}
}

package app

import (
	"strings"
	"testing"
	"time"

	dnssrv "gatehouse/internal/dns"
)

func newScopeApp(t *testing.T) *App {
	t.Helper()
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: freeAddr(t), DNSAddrs: []string{freeAddr(t)}, Net: &fakeNet{}, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestBlockAnAppForOneDeviceWithAnException(t *testing.T) {
	a := newScopeApp(t)
	kid := dnssrv.ClientInfo{MAC: "aa:aa:aa:aa:aa:01", GroupID: 2, Filtering: true}
	dad := dnssrv.ClientInfo{MAC: "aa:aa:aa:aa:aa:02", GroupID: 3, Filtering: true}
	// nothing set: nothing blocked
	if r := a.scopedBlock("192.168.1.5", kid, "r4.googlevideo.com"); r != "" {
		t.Fatalf("blocked with no rules: %q", r)
	}
	// block YouTube for everyone, except one device
	if err := a.saveScopeRule(blockIn{Kind: "app", Value: "youtube", Scope: "everyone", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.saveScopeRule(blockIn{Kind: "app", Value: "YouTube", Scope: "device", Target: dad.MAC, Allow: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if r := a.scopedBlock("192.168.1.5", kid, "r4.googlevideo.com"); !strings.Contains(r, "YouTube") {
		t.Fatalf("kid not blocked: %q", r)
	}
	if r := a.scopedBlock("192.168.1.6", dad, "r4.googlevideo.com"); r != "" {
		t.Fatalf("exception ignored: %q", r)
	}
	if r := a.scopedBlock("192.168.1.5", kid, "example.com"); r != "" {
		t.Fatalf("unrelated name blocked: %q", r)
	}
}

func TestBlockScopesPauseAndHours(t *testing.T) {
	a := newScopeApp(t)
	c2 := dnssrv.ClientInfo{MAC: "aa:aa:aa:aa:aa:01", GroupID: 2, Filtering: true}
	c3 := dnssrv.ClientInfo{MAC: "aa:aa:aa:aa:aa:02", GroupID: 3, Filtering: true}
	// a name, for profile 2 only
	if err := a.saveScopeRule(blockIn{Kind: "name", Value: "Games.Example.com", Scope: "profile", Target: "2", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if a.scopedBlock("1.1.1.1", c2, "cdn.games.example.com") == "" {
		t.Fatal("profile 2 should be blocked (subdomain)")
	}
	if a.scopedBlock("1.1.1.1", c3, "games.example.com") != "" {
		t.Fatal("profile 3 must not be blocked")
	}
	// pause it
	rules, _ := a.loadScopeRules()
	a.Config.Exec(`UPDATE scope_rules SET paused_until=? WHERE id=?`, time.Now().Add(time.Hour).Unix(), rules[0].ID)
	a.dropScopeCache()
	if a.scopedBlock("1.1.1.1", c2, "games.example.com") != "" {
		t.Fatal("paused rule still blocks")
	}
	// hours: a window that is never now blocks nothing; one that is always now blocks
	a.Config.Exec(`UPDATE scope_rules SET paused_until=0 WHERE id=?`, rules[0].ID)
	now := time.Now().In(a.scopeLoc())
	in := now.Add(-time.Hour).Format("15:04")
	out := now.Add(time.Hour).Format("15:04")
	a.Config.Exec(`UPDATE scope_rules SET schedule=? WHERE id=?`, `{"mode":"daily","from":"`+in+`","to":"`+out+`"}`, rules[0].ID)
	a.dropScopeCache()
	if a.scopedBlock("1.1.1.1", c2, "games.example.com") == "" {
		t.Fatal("inside its hours it should block")
	}
	far1 := now.Add(2 * time.Hour).Format("15:04")
	far2 := now.Add(3 * time.Hour).Format("15:04")
	a.Config.Exec(`UPDATE scope_rules SET schedule=? WHERE id=?`, `{"mode":"daily","from":"`+far1+`","to":"`+far2+`"}`, rules[0].ID)
	a.dropScopeCache()
	if a.scopedBlock("1.1.1.1", c2, "games.example.com") != "" {
		t.Fatal("outside its hours it should not block")
	}
	// an unreadable schedule fails open
	a.Config.Exec(`UPDATE scope_rules SET schedule='garbage' WHERE id=?`, rules[0].ID)
	a.dropScopeCache()
	if a.scopedBlock("1.1.1.1", c2, "games.example.com") != "" {
		t.Fatal("a broken schedule must fail open")
	}
}

func TestBlockRefusesBadInputAndThoroughForNow(t *testing.T) {
	a := newScopeApp(t)
	for _, in := range []blockIn{
		{Kind: "app", Value: "NoSuchApp", Scope: "everyone", Enabled: true},
		{Kind: "name", Value: "not a domain!", Scope: "everyone", Enabled: true},
		{Kind: "app", Value: "YouTube", Scope: "device", Enabled: true},
		{Kind: "app", Value: "YouTube", Scope: "everyone", Method: "thorough", Enabled: true},
		{Kind: "app", Value: "YouTube", Scope: "everyone", Schedule: `{"mode":"daily","from":"08:00","to":"08:00"}`, Enabled: true},
	} {
		if err := a.saveScopeRule(in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
}

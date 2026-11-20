package app

import "testing"

func TestSecurityCheckVerdictAndFixes(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("GET", "/api/security/check", nil)
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	items, _ := m["items"].([]any)
	if len(items) < 6 || m["summary"] == "" {
		t.Fatalf("%s", raw)
	}
	for _, it := range items {
		x := it.(map[string]any)
		if x["level"] != "ok" && x["fix"] == nil {
			t.Errorf("%v has no fix button", x["id"])
		}
	}
}

func TestSshWarningOnlyWhenRemoteLoginIsOn(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if _, err := a.Config.Exec(`UPDATE zones SET ssh=1 WHERE kind<>'wan'`); err != nil {
		t.Fatal(err)
	}
	level := func() string {
		for _, it := range a.securityCheck().Items {
			if it.ID == "ssh" {
				return it.Level
			}
		}
		return "missing"
	}
	a.Config.SetSetting("ssh.enabled", "0")
	if l := level(); l != "ok" {
		t.Fatalf("remote login is off, but the check said %q", l)
	}
	a.Config.SetSetting("ssh.enabled", "1")
	if l := level(); l == "ok" || l == "missing" {
		t.Fatalf("remote login is on for a network that may use it, but the check said %q", l)
	}
}

// Only recommendations can be dismissed. A real fault is never marked as advice, so it cannot be hidden.
func TestOnlyAdviceCanBeDismissed(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, m, raw := c.call("GET", "/api/security/check", nil)
	items, _ := m["items"].([]any)
	seen := map[string]bool{}
	for _, it := range items {
		x := it.(map[string]any)
		id, _ := x["id"].(string)
		advice, _ := x["advice"].(bool)
		seen[id] = advice
		if advice && id != "twostep" && id != "alerts" {
			t.Errorf("%s is marked as advice but is a fault: %s", id, raw)
		}
	}
	for _, id := range []string{"console", "ssh", "upnp", "forwards", "dot", "updates", "backup"} {
		if seen[id] {
			t.Errorf("%s must not be dismissable", id)
		}
	}
	if !seen["twostep"] {
		t.Errorf("two-factor advice should be dismissable while it is off: %s", raw)
	}
}

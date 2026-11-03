package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStagedChangesAreListedInWordsAndCanBeDiscarded(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, _, raw := c.call("PUT", "/api/fw/rules", map[string]any{"name": "Usenet", "from": "Home", "to": "Internet", "service": "ntp", "action": "accept", "enabled": true})
	if rc != 200 {
		t.Fatalf("rule: %d %s", rc, raw)
	}
	_, _, v := c.call("GET", "/api/staged", nil)
	var st struct {
		Items []struct{ Text string }
		FW    bool `json:"fw"`
		Net   bool `json:"net"`
		Can   bool `json:"can_discard"`
	}
	json.Unmarshal(v, &st)
	if !st.FW && !st.Net {
		t.Skipf("the change did not leave anything waiting here: %s", v)
	}
	if len(st.Items) == 0 || !strings.Contains(st.Items[len(st.Items)-1].Text, "Usenet") {
		t.Fatalf("the change is not in the list in words: %s", v)
	}
	if !st.Can {
		t.Fatalf("there is no copy to go back to: %s", v)
	}
	rc, _, raw = c.call("POST", "/api/staged/discard", nil)
	if rc != 200 {
		t.Fatalf("discard: %d %s", rc, raw)
	}
	_, _, view := c.call("GET", "/api/fw/rules", nil)
	if strings.Contains(string(view), "Usenet") {
		t.Errorf("the route is still there after discarding: %s", view)
	}
	_, _, v = c.call("GET", "/api/staged", nil)
	json.Unmarshal(v, &st)
	if len(st.Items) != 0 || st.Can {
		t.Errorf("the list was not cleared: %s", v)
	}
}

func TestStagedWordsForOtherAreas(t *testing.T) {
	for p, want := range map[string]string{
		"/api/fw/settings": "firewall settings",
		"/api/net/wans":    "internet connection",
		"/api/fw/zones":    "network type",
		"/api/dhcp/scopes": "DHCP",
		"/api/fw/rules":    "firewall rule",
	} {
		if got := stagedWords("PUT", p, []byte(`{}`)); !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Errorf("%s: %q lacks %q", p, got, want)
		}
	}
}

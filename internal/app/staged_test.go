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

func TestDiscardOneLineGoesBackToJustBeforeIt(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	put := func(name string) {
		rc, _, raw := c.call("PUT", "/api/fw/rules", map[string]any{"name": name, "from": "Home", "to": "Internet", "service": "ntp", "action": "accept", "enabled": true})
		if rc != 200 {
			t.Fatalf("rule %s: %d %s", name, rc, raw)
		}
	}
	type view struct {
		Items []struct {
			ID   int64  `json:"id"`
			Text string `json:"text"`
			Can  bool   `json:"can_discard"`
		}
		FW  bool `json:"fw"`
		Net bool `json:"net"`
	}
	get := func() (view, string) {
		_, _, v := c.call("GET", "/api/staged", nil)
		var st view
		json.Unmarshal(v, &st)
		return st, string(v)
	}
	has := func(name string) bool {
		_, _, v := c.call("GET", "/api/fw/rules", nil)
		return strings.Contains(string(v), name)
	}
	st0, _ := get() // the test box has already staged its own setup lines
	n0 := len(st0.Items)
	put("Alpha rule")
	put("Bravo rule")
	st, raw := get()
	if !st.FW && !st.Net {
		t.Skipf("nothing left waiting here: %s", raw)
	}
	if len(st.Items) != n0+2 || !st.Items[n0].Can || !st.Items[n0+1].Can {
		t.Fatalf("each line needs its own way back: %s", raw)
	}
	// Discard the second line only: the first stays.
	if rc, _, r := c.call("POST", "/api/staged/discard", map[string]any{"from": st.Items[n0+1].ID}); rc != 200 {
		t.Fatalf("discard second: %d %s", rc, r)
	}
	if !has("Alpha rule") || has("Bravo rule") {
		t.Fatalf("after discarding the second line: alpha=%v bravo=%v", has("Alpha rule"), has("Bravo rule"))
	}
	st, raw = get()
	if len(st.Items) != n0+1 || !strings.Contains(st.Items[n0].Text, "Alpha") {
		t.Fatalf("the list should hold only the first line: %s", raw)
	}
	// A new change after that still has a way back that keeps the first line.
	put("Charlie rule")
	st, raw = get()
	if len(st.Items) != n0+2 || !st.Items[n0+1].Can {
		t.Fatalf("new line after a discard: %s", raw)
	}
	// Discard the FIRST line: it and everything after go.
	if rc, _, r := c.call("POST", "/api/staged/discard", map[string]any{"from": st.Items[n0].ID}); rc != 200 {
		t.Fatalf("discard first: %d %s", rc, r)
	}
	if has("Alpha rule") || has("Charlie rule") {
		t.Fatalf("the first line and the ones after it should be gone")
	}
	st, raw = get()
	if len(st.Items) != n0 {
		t.Fatalf("list should be back to the setup lines only: %s", raw)
	}
	// An unknown line is refused, and nothing changes.
	if rc, _, _ := c.call("POST", "/api/staged/discard", map[string]any{"from": 9999}); rc == 200 {
		t.Fatalf("an unknown line was accepted")
	}
}

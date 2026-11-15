package app

import (
	"strconv"
	"strings"
	"testing"
)

func TestSetsAndTagsLabelAndGroupWithoutChangingPolicies(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("POST", "/api/sets", map[string]any{"name": "Guest Wi-Fi"})
	if rc != 200 {
		t.Fatalf("make set: %d %s", rc, raw)
	}
	id := int64(m["id"].(float64))
	if rc, _, _ := c.call("POST", "/api/sets", map[string]any{"name": "guest wi-fi"}); rc == 200 {
		t.Fatal("two sets may not share a name, whatever the capitals")
	}
	if rc, _, _ := c.call("PUT", "/api/sets/member", map[string]any{"kind": "device", "ref": "aa:bb", "set_id": id}); rc == 200 {
		t.Fatal("a device cannot be in a set")
	}
	for _, p := range []string{"7", "8"} {
		if rc, _, raw := c.call("PUT", "/api/sets/member", map[string]any{"kind": "rule", "ref": p, "set_id": id}); rc != 200 {
			t.Fatalf("add: %d %s", rc, raw)
		}
	}
	// Moving a policy to another set takes it out of the first: at most one set each.
	_, m, _ = c.call("POST", "/api/sets", map[string]any{"name": "Work"})
	id2 := int64(m["id"].(float64))
	c.call("PUT", "/api/sets/member", map[string]any{"kind": "rule", "ref": "8", "set_id": id2})
	_, v, _ := c.call("GET", "/api/sets", nil)
	sets := v["sets"].([]any)
	count := map[string]int{}
	for _, s := range sets {
		sm := s.(map[string]any)
		count[sm["name"].(string)] = len(sm["members"].([]any))
	}
	if count["Guest Wi-Fi"] != 1 || count["Work"] != 1 {
		t.Fatalf("one set each: %v", count)
	}
	// Tags: cleaned, shared, at most five.
	rc, m, raw = c.call("PUT", "/api/tags", map[string]any{"kind": "device", "ref": "AA:BB:CC:00:00:01", "tags": []string{"  Media ", "LIVING room!", "media", "x"}})
	if rc != 200 || len(m["tags"].([]any)) != 3 {
		t.Fatalf("tags: %d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/tags", map[string]any{"kind": "rule", "ref": "7", "tags": []string{"a", "b", "c", "d", "e", "f"}}); rc != 400 {
		t.Fatalf("a sixth tag must be refused, got %d", rc)
	}
	if rc, _, _ := c.call("PUT", "/api/tags", map[string]any{"kind": "rule", "ref": "7", "tags": []string{strings.Repeat("x", 40)}}); rc != 200 {
		t.Fatal("a long tag is cut, not refused")
	}
	_, v, _ = c.call("GET", "/api/sets", nil)
	if v["tags"].(map[string]any)["device:aa:bb:cc:00:00:01"] == nil {
		t.Fatalf("device tags are kept under the lower-case address: %v", v["tags"])
	}
	// Deleting a set keeps nothing behind; a deleted policy forgets its tags and set.
	c.call("DELETE", "/api/sets/"+strconv.FormatInt(id2, 10), nil)
	_, v, _ = c.call("GET", "/api/sets", nil)
	if len(v["sets"].([]any)) != 1 {
		t.Fatal("the set should be gone")
	}
	c.app.dropItem("rule", "7")
	_, v, _ = c.call("GET", "/api/sets", nil)
	if v["tags"].(map[string]any)["rule:7"] != nil || len(v["sets"].([]any)[0].(map[string]any)["members"].([]any)) != 0 {
		t.Fatalf("a deleted policy must leave its set and tags: %v", v)
	}
}

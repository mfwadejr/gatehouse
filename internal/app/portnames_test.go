package app

import "testing"

func TestPortNamesStoreOnlyALabel(t *testing.T) {
	fakePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	list := ports()
	if len(list) == 0 {
		t.Fatal("the test setup has no ports")
	}
	name, _ := list[0]["name"].(string)
	if rc, _, raw := c.call("PUT", "/api/net/ports/label", map[string]string{"name": name, "label": "  T-Mobile   gateway "}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if got := c.app.portLabels()[name]; got != "T-Mobile gateway" {
		t.Fatalf("label = %q", got)
	}
	found := false
	for _, p := range c.app.labelledPorts() {
		if p["name"] == name && p["label"] == "T-Mobile gateway" {
			found = true
		}
	}
	if !found {
		t.Fatal("the port list does not carry the label")
	}
	if n, _ := ports()[0]["name"].(string); n != name {
		t.Fatal("the port name changed")
	}
	// too long and unknown ports are refused; an empty name puts the original back
	if rc, _, _ := c.call("PUT", "/api/net/ports/label", map[string]string{"name": name, "label": "a name that is far too long to be useful"}); rc == 200 {
		t.Fatal("a very long name was accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/net/ports/label", map[string]string{"name": "nope9", "label": "x"}); rc == 200 {
		t.Fatal("an unknown port was accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/net/ports/label", map[string]string{"name": name, "label": ""}); rc != 200 || c.app.portLabels()[name] != "" {
		t.Fatalf("clearing failed: %v", c.app.portLabels())
	}
}

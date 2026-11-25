package app

import "testing"

func TestPortNamesStoreOnlyALabel(t *testing.T) {
	fakePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	list := portsLister()
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
	if n, _ := portsLister()[0]["name"].(string); n != name {
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

// A port and the one internet connection on it share a name: the latest rename wins from either place.
func TestAPortAndItsConnectionShareOneName(t *testing.T) {
	fakePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	list := portsLister()
	if len(list) < 2 {
		t.Skip("needs two ports")
	}
	port, _ := list[0]["name"].(string)
	other, _ := list[1]["name"].(string)
	wans := func() []WanRow {
		v, err := c.app.listWans(c.app.Config)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	find := func(iface string) WanRow {
		for _, w := range wans() {
			if w.Iface == iface {
				return w
			}
		}
		t.Fatalf("no connection on %s", iface)
		return WanRow{}
	}
	// naming a connection names its port
	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "WAN", "iface": port, "type": "dhcp", "enabled": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if got := c.app.portLabels()[port]; got != "WAN" {
		t.Fatalf("port label = %q, want WAN", got)
	}
	// naming the port names its connection
	if rc, _, raw := c.call("PUT", "/api/net/ports/label", map[string]string{"name": port, "label": "T-Mobile"}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	w := find(port)
	if w.Name != "T-Mobile" {
		t.Fatalf("connection name = %q, want T-Mobile", w.Name)
	}
	// renaming the connection again carries back to the port
	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"id": w.ID, "name": "Cable", "iface": port, "type": "dhcp", "enabled": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if got := c.app.portLabels()[port]; got != "Cable" {
		t.Fatalf("port label = %q, want Cable", got)
	}
	// putting the original port name back leaves the connection's name alone
	if rc, _, _ := c.call("PUT", "/api/net/ports/label", map[string]string{"name": port, "label": ""}); rc != 200 {
		t.Fatal("clearing failed")
	}
	if find(port).Name != "Cable" {
		t.Fatal("clearing the port name renamed the connection")
	}
	// a second connection cannot take a name the first already has, and nothing is half done
	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Backup", "iface": other, "type": "dhcp", "enabled": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/net/ports/label", map[string]string{"name": other, "label": "Cable"}); rc == 200 {
		t.Fatal("a second connection was given the same name")
	}
	if got := c.app.portLabels()[other]; got != "Backup" {
		t.Fatalf("a refused rename changed the port label to %q", got)
	}
	// a port that carries two connections (one per VLAN) keeps its own name and theirs
	if rc, _, raw := c.call("PUT", "/api/net/wans", map[string]any{"name": "Voice", "iface": other, "vlan": 30, "type": "dhcp", "enabled": true}); rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, _ := c.call("PUT", "/api/net/ports/label", map[string]string{"name": other, "label": "Shared port"}); rc != 200 {
		t.Fatal("naming a shared port failed")
	}
	for _, w := range wans() {
		if w.Iface == other && (w.Name == "Shared port") {
			t.Fatalf("a port with two connections renamed %q", w.Name)
		}
	}
}

// After an update, a port with no name takes its connection's name, and where the two disagree the
// connection's name is used. It is recorded once and does not run again.
func TestPortNamesAreBroughtTogetherOnce(t *testing.T) {
	fakePorts(t)
	c, _, _ := siteBox(t, "192.168.1.1/24")
	port, _ := portsLister()[0]["name"].(string)
	if _, err := c.app.Config.Exec(`INSERT INTO wan_links(name,iface,type) VALUES('Fibre two',?,'dhcp')`, port); err != nil {
		t.Fatal(err)
	}
	c.app.storeLabel(port, "Old name")
	c.app.Config.SetSetting("net.names_synced", "")
	c.app.syncPortNames()
	if got := c.app.portLabels()[port]; got != "Fibre two" {
		t.Fatalf("label = %q, want Fibre two", got)
	}
	// it does not run twice
	c.app.storeLabel(port, "Mine")
	c.app.syncPortNames()
	if got := c.app.portLabels()[port]; got != "Mine" {
		t.Fatalf("the sync ran a second time and set %q", got)
	}
}

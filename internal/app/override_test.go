package app

import (
	"errors"
	"strings"
	"testing"
)

// addVLAN puts a VLAN network (with an address service) into the settings, the way the Networks
// page would, so the override has something to choose.
func addVLAN(t *testing.T, a *App, name, zone string, vid int, parent string, dhcp bool) {
	t.Helper()
	res, err := a.Config.Exec(`INSERT INTO zones(name,kind) VALUES(?,?)`, zone, "isolated")
	if err != nil {
		t.Fatal(err)
	}
	zid, _ := res.LastInsertId()
	if _, err := a.Config.Exec(`INSERT INTO interfaces(name,kind,parent,vlan_id,zone_id,ipv4_cidr) VALUES(?,?,?,?,?,?)`, name, "vlan", parent, vid, zid, "192.168.20.1/24"); err != nil {
		t.Fatal(err)
	}
	if dhcp {
		if _, err := a.Config.Exec(`INSERT INTO dhcp_scopes(name,iface,mode,subnet,range_start,range_end,router) VALUES(?,?,?,?,?,?,?)`, zone, name, "serve", "192.168.20.0/24", "192.168.20.100", "192.168.20.200", "192.168.20.1"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVirtualNetworkOverride(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	addVLAN(t, a, "eth1.20", "Cameras", 20, "eth1", true)
	id, err := a.Devices.Create("aa:bb:cc:00:11:22", "camera")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/devices/" + itoa(id) + "/override"

	if rc, _, raw := c.call("PUT", path, map[string]any{"network": "nope"}); rc != 409 || !strings.Contains(string(raw), "VLAN") {
		t.Fatalf("an unknown network must be refused in words: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("PUT", path, map[string]any{"network": "eth1.20"}); rc != 200 {
		t.Fatalf("set: %d %s", rc, raw)
	}
	fn.mu.Lock()
	last := fn.ovPlans[len(fn.ovPlans)-1]
	fn.mu.Unlock()
	if len(last.Rules) != 1 || last.Rules[0].MAC != "aa:bb:cc:00:11:22" || last.Rules[0].VID != 20 || last.Rules[0].Parent != "eth1" {
		t.Fatalf("plan: %+v", last)
	}
	_, m, _ := c.call("GET", "/api/devices", nil)
	found := false
	if list, ok := m["devices"].([]any); ok {
		for _, d := range list {
			if dm := d.(map[string]any); dm["mac"] == "aa:bb:cc:00:11:22" {
				found = dm["override"] == "eth1.20" && dm["override_network"] == "Cameras"
			}
		}
	}
	if !found {
		t.Fatalf("the device list must show the move: %v", m)
	}

	// A failure leaves the device where it was.
	fn.mu.Lock()
	fn.ovErr = errors.New("this system's kernel cannot tag traffic by device address")
	fn.mu.Unlock()
	id2, _ := a.Devices.Create("aa:bb:cc:00:11:33", "tv")
	if rc, _, raw := c.call("PUT", "/api/devices/"+itoa(id2)+"/override", map[string]any{"network": "eth1.20"}); rc != 409 || !strings.Contains(string(raw), "kernel") {
		t.Fatalf("failure must be plain: %d %s", rc, raw)
	}
	var ov string
	a.Config.QueryRow(`SELECT override_iface FROM devices WHERE id=?`, id2).Scan(&ov)
	if ov != "" {
		t.Fatalf("a failed move must not be saved, got %q", ov)
	}
	fn.mu.Lock()
	fn.ovErr = nil
	fn.mu.Unlock()

	// Removing the override clears the rule.
	if rc, _, _ := c.call("PUT", path, map[string]any{"network": ""}); rc != 200 {
		t.Fatal("clear")
	}
	fn.mu.Lock()
	last = fn.ovPlans[len(fn.ovPlans)-1]
	fn.mu.Unlock()
	if len(last.Rules) != 0 {
		t.Fatalf("rule should be gone: %+v", last)
	}
}

func TestNewDeviceApproval(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	addVLAN(t, a, "eth1.30", "Waiting", 30, "eth1", true)
	addVLAN(t, a, "eth1.20", "Cameras", 20, "eth1", true)

	// Turning it on needs a holding network that actually hands out addresses.
	if rc, _, raw := c.call("PUT", "/api/approval", map[string]any{"enabled": true, "holding": ""}); rc != 409 || !strings.Contains(string(raw), "holding network") {
		t.Fatalf("must ask for a holding network: %d %s", rc, raw)
	}
	a.Config.Exec(`UPDATE dhcp_scopes SET enabled=0 WHERE iface='eth1.30'`)
	if rc, _, raw := c.call("PUT", "/api/approval", map[string]any{"enabled": true, "holding": "eth1.30"}); rc != 409 || !strings.Contains(string(raw), "address service") {
		t.Fatalf("holding network without DHCP: %d %s", rc, raw)
	}
	a.Config.Exec(`UPDATE dhcp_scopes SET enabled=1 WHERE iface='eth1.30'`)
	if rc, _, raw := c.call("PUT", "/api/approval", map[string]any{"enabled": true, "holding": "eth1.30"}); rc != 200 {
		t.Fatalf("enable: %d %s", rc, raw)
	}

	// An existing device is never held.
	a.Devices.Create("aa:bb:cc:00:00:01", "laptop")
	if a.approvalHold("aa:bb:cc:00:00:01", "eth1") {
		t.Fatal("a known device must not be held")
	}
	// A new device is held, recorded as waiting, and steered to the holding network.
	if !a.approvalHold("aa:bb:cc:00:00:02", "eth1") {
		t.Fatal("a new device should be held")
	}
	var ap, ov string
	a.Config.QueryRow(`SELECT approval,override_iface FROM devices WHERE mac='aa:bb:cc:00:00:02'`).Scan(&ap, &ov)
	if ap != "pending" || ov != "eth1.30" {
		t.Fatalf("pending device: %q %q", ap, ov)
	}
	fn.mu.Lock()
	n := len(fn.ovPlans)
	last := fn.ovPlans[n-1]
	fn.mu.Unlock()
	if len(last.Rules) != 1 || last.Rules[0].VID != 30 {
		t.Fatalf("plan: %+v", last)
	}
	// Once it arrives on the holding network it is answered.
	if a.approvalHold("aa:bb:cc:00:00:02", "eth1.30") {
		t.Fatal("on the holding network it must be answered")
	}
	// If the steering cannot be applied, the device is let in rather than left without a network.
	fn.mu.Lock()
	fn.ovErr = errors.New("kernel cannot tag")
	fn.mu.Unlock()
	if a.approvalHold("aa:bb:cc:00:00:03", "eth1") {
		t.Fatal("when steering fails the device must be let on its own network")
	}
	fn.mu.Lock()
	fn.ovErr = nil
	fn.mu.Unlock()
	// A device that keeps arriving on its own network (steering not working) is let in after a few tries.
	got := false
	for i := 0; i < 8; i++ {
		if !a.approvalHold("aa:bb:cc:00:00:02", "eth1") {
			got = true
		}
	}
	if !got {
		t.Fatal("a device must never be held for ever")
	}

	// The person decides: place it on Cameras.
	var id int64
	a.Config.QueryRow(`SELECT id FROM devices WHERE mac='aa:bb:cc:00:00:02'`).Scan(&id)
	_, st, _ := c.call("GET", "/api/approval", nil)
	if p, _ := st["pending"].([]any); len(p) == 0 {
		t.Fatalf("it should be listed as waiting: %v", st)
	}
	if rc, _, raw := c.call("POST", "/api/approval/"+itoa(id)+"/place", map[string]any{"network": "eth1.20"}); rc != 200 {
		t.Fatalf("place: %d %s", rc, raw)
	}
	a.Config.QueryRow(`SELECT approval,override_iface FROM devices WHERE id=?`, id).Scan(&ap, &ov)
	if ap != "ok" || ov != "eth1.20" {
		t.Fatalf("after placing: %q %q", ap, ov)
	}
	// Allow returns a device from the holding network to its own.
	if rc, _, _ := c.call("POST", "/api/approval/"+itoa(id)+"/block", map[string]any{}); rc != 200 {
		t.Fatal("block")
	}
	a.Config.QueryRow(`SELECT approval,override_iface FROM devices WHERE id=?`, id).Scan(&ap, &ov)
	if ap != "blocked" || ov != "eth1.30" {
		t.Fatalf("blocked devices stay on the holding network: %q %q", ap, ov)
	}
	if rc, _, _ := c.call("POST", "/api/approval/"+itoa(id)+"/allow", map[string]any{}); rc != 200 {
		t.Fatal("allow")
	}
	a.Config.QueryRow(`SELECT approval,override_iface FROM devices WHERE id=?`, id).Scan(&ap, &ov)
	if ap != "ok" || ov != "" {
		t.Fatalf("after allow: %q %q", ap, ov)
	}
}

// Serious actions ask for the password when two-factor sign-in is off, and everyday ones ask for nothing.
func TestSeriousActionsAskForThePasswordWithoutTwoFactor(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	_, st, _ := c.call("GET", "/api/stepup", nil)
	if st["serious"] != "password" || st["everyday"] != false {
		t.Fatalf("stepup: %v", st)
	}
	// A reset with no password, and with a wrong one, is refused in words.
	if rc, _, raw := c.call("POST", "/api/system/reset", map[string]any{"level": "network"}); rc != 403 || !strings.Contains(string(raw), "password") {
		t.Fatalf("no password: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/system/reset", map[string]any{"level": "network", "password": "wrong"}); rc != 403 || !strings.Contains(string(raw), "not right") {
		t.Fatalf("wrong password: %d %s", rc, raw)
	}
	// Everyday changes (applying) never ask.
	if rc, _, raw := c.call("POST", "/api/net/apply", map[string]any{}); rc == 403 {
		t.Fatalf("applying must not ask for anything: %d %s", rc, raw)
	}
	// Turning remote login on asks; turning it off does not.
	if rc, _, _ := c.call("PUT", "/api/system/ssh", map[string]any{"enabled": true}); rc != 403 {
		t.Fatal("turning SSH on must ask")
	}
	if rc, _, _ := c.call("PUT", "/api/system/ssh", map[string]any{"enabled": false}); rc == 403 {
		t.Fatal("turning SSH off must not ask")
	}
}

func TestAddPortsToAPlainNetwork(t *testing.T) {
	fakePorts(t) // tp1, tp2 wired; tpw Wi-Fi
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	var id int64
	var name string
	a.Config.QueryRow(`SELECT id,name FROM interfaces WHERE zone_id IS NOT NULL AND kind='phys' LIMIT 1`).Scan(&id, &name)
	if id == 0 {
		t.Skip("the test box has no plain network")
	}
	// The network's own port must be one the machine has; pretend it is tp1.
	a.Config.Exec(`UPDATE interfaces SET name='tp1' WHERE id=?`, id)
	a.Config.Exec(`INSERT INTO dhcp_scopes(name,iface,mode,subnet,range_start,range_end,router) VALUES('Home','tp1','serve','192.168.1.0/24','192.168.1.100','192.168.1.200','192.168.1.1')`)
	addVLAN(t, a, "tp1.20", "Cameras", 20, "tp1", true)
	a.Config.Exec(`UPDATE devices SET override_iface='' `)

	if rc, _, raw := c.call("POST", "/api/networks/add-ports", map[string]any{"id": id, "ports": []string{}}); rc == 200 || !strings.Contains(string(raw), "at least one") {
		t.Fatalf("empty: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/networks/add-ports", map[string]any{"id": id, "ports": []string{"tpw"}}); rc == 200 || !strings.Contains(string(raw), "wired") {
		t.Fatalf("a Wi-Fi card is not a wired port: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/networks/add-ports", map[string]any{"id": id, "ports": []string{"tp2; reboot"}}); rc == 200 {
		t.Fatalf("a bad name must be refused: %s", raw)
	}
	rc, m, raw := c.call("POST", "/api/networks/add-ports", map[string]any{"id": id, "ports": []string{"tp2"}})
	if rc != 200 {
		t.Fatalf("add: %d %s", rc, raw)
	}
	br, _ := m["name"].(string)
	var kind, members, zone string
	a.Config.QueryRow(`SELECT i.kind,i.members,COALESCE(z.name,'') FROM interfaces i LEFT JOIN zones z ON z.id=i.zone_id WHERE i.id=?`, id).Scan(&kind, &members, &zone)
	if kind != "bridge" || members != "tp1,tp2" || zone != "Home" || br == "" {
		t.Fatalf("converted network: %q %q %q %q", kind, members, zone, br)
	}
	var scope, vlan, parent string
	a.Config.QueryRow(`SELECT iface FROM dhcp_scopes WHERE name='Home'`).Scan(&scope)
	a.Config.QueryRow(`SELECT name,parent FROM interfaces WHERE kind='vlan'`).Scan(&vlan, &parent)
	if scope != br || vlan != br+".20" || parent != br {
		t.Fatalf("what pointed at the old port must follow: scope %q vlan %q parent %q", scope, vlan, parent)
	}
	var vs string
	a.Config.QueryRow(`SELECT iface FROM dhcp_scopes WHERE name='Cameras'`).Scan(&vs)
	if vs != br+".20" {
		t.Fatalf("the VLAN's address service must follow: %q", vs)
	}
	// Doing it again is refused: it is no longer a plain network.
	if rc, _, _ := c.call("POST", "/api/networks/add-ports", map[string]any{"id": id, "ports": []string{"tp2"}}); rc == 200 {
		t.Fatal("a shared-port network cannot be converted again")
	}
}

package app

import (
	"strings"
	"testing"

	"gatehouse/internal/netd"
)

func TestRemoteLoginIsOffUntilTurnedOnAndTheCheckWorksOnlyThen(t *testing.T) {
	c, fn, _ := siteBox(t, "192.168.1.1/24")
	body := map[string]any{"user": "admin", "password": "correct horse battery", "from": "192.168.1.50"}
	if rc, _, _ := c.call("POST", "/api/auth/ssh-verify", body); rc != 403 {
		t.Fatalf("the password check answered while remote login was off: %d", rc)
	}
	if rc, _, raw := c.call("PUT", "/api/system/ssh", map[string]any{"enabled": true, "password": "correct horse battery"}); rc != 200 {
		t.Fatalf("turn on: %d %s", rc, raw)
	}
	fn.mu.Lock()
	plans := append([]netd.SSHPlan{}, fn.sshPlans...)
	fn.mu.Unlock()
	if len(plans) == 0 || !plans[len(plans)-1].Enabled || strings.Join(plans[len(plans)-1].Users, ",") != "admin" || plans[len(plans)-1].Binary == "" {
		t.Fatalf("the helper was not asked to turn SSH on for the administrators: %+v", plans)
	}
	if rc, _, _ := c.call("POST", "/api/auth/ssh-verify", body); rc != 200 {
		t.Fatalf("the right password was refused: %d", rc)
	}
	body["password"] = "wrong wrong wrong"
	if rc, _, _ := c.call("POST", "/api/auth/ssh-verify", body); rc != 401 {
		t.Fatalf("a wrong password was accepted: %d", rc)
	}
	body["user"], body["password"] = "nobody", "correct horse battery"
	if rc, _, _ := c.call("POST", "/api/auth/ssh-verify", body); rc != 401 {
		t.Fatalf("an unknown name was accepted: %d", rc)
	}
	// Turning it off closes it again.
	c.call("PUT", "/api/system/ssh", map[string]any{"enabled": false})
	if rc, _, _ := c.call("POST", "/api/auth/ssh-verify", map[string]any{"user": "admin", "password": "correct horse battery", "from": "1.1.1.1"}); rc != 403 {
		t.Fatalf("still answering after it was turned off: %d", rc)
	}
}

func TestTurningOnRemoteLoginOpensManagementNetworksOnlyAndFailureLeavesItOff(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	c.call("PUT", "/api/system/ssh", map[string]any{"enabled": true, "password": "correct horse battery"})
	_, m, raw := c.call("GET", "/api/system/ssh", nil)
	if m["enabled"] != true {
		t.Fatalf("%s", raw)
	}
	nets, _ := m["networks"].([]any)
	if len(nets) == 0 {
		t.Fatalf("no network lists SSH as reachable: %s", raw)
	}
}

func TestStalledWifiIsRecordedAndRecoveryIsLogged(t *testing.T) {
	a, fn := newSpeedApp(t)
	a.Config.Exec(`INSERT INTO wifi_radios(iface,mode,ssid,psk,country,band,channel,hidden,wpa3,fast,enabled) VALUES('wlp3s0','ap','gatehouse','',?,'2.4',0,0,0,0,1)`, "US")
	fn.wifiStates = []netd.WifiState{{Iface: "wlp3s0", OK: false, Reason: "the card's driver does not answer", Action: "restarted the Wi-Fi service", Recoveries: 1}}
	a.wifiCheckOnce()
	var n int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE title LIKE 'Wi-Fi on wlp3s0 stopped working%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("the stall was not logged (%d)", n)
	}
	if st, ok := a.wifiState("wlp3s0"); !ok || st.OK {
		t.Fatalf("state: %+v %v", st, ok)
	}
	fn.wifiStates = []netd.WifiState{{Iface: "wlp3s0", OK: true, Stations: 2}}
	a.wifiCheckOnce()
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE title='Wi-Fi on wlp3s0 is working again'`).Scan(&n)
	if n != 1 {
		t.Fatalf("the recovery was not logged (%d)", n)
	}
}

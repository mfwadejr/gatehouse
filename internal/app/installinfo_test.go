package app

import "testing"

// The console asks every machine whether it is the installer; an installed one must answer no
// (200), not 404, so the browser's error log stays clean on every page load.
func TestInstallInfoSaysNotTheInstaller(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("GET", "/api/install/info", nil)
	if rc != 200 || m["installer"] != false {
		t.Fatalf("%d %s", rc, raw)
	}
}

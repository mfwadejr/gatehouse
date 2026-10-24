package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/netd"
	"gatehouse/internal/update"
)

const updTestPriv = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz
c2gtZWQyNTUxOQAAACBOfSjnkNwfOd66xbySynJJGY1YbBeYZGq66zwuLb1LtwAA
AIjLZc6Sy2XOkgAAAAtzc2gtZWQyNTUxOQAAACBOfSjnkNwfOd66xbySynJJGY1Y
bBeYZGq66zwuLb1LtwAAAEA1wLVyxuhSehBp4f0ZMNCmLUo4ewnkrmHU9oFf2XDS
zk59KOeQ3B853rrFvJLKckkZjVhsF5hkarrrPC4tvUu3AAAAAAECAwQF
-----END OPENSSH PRIVATE KEY-----
`
const updTestPub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE59KOeQ3B853rrFvJLKckkZjVhsF5hkarrrPC4tvUu3 test"

func updPackage(t *testing.T, version string, sign bool) (data []byte, sig string) {
	t.Helper()
	priv, err := update.ParsePrivateKey(updTestPriv)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gatehouse")
	os.WriteFile(bin, append([]byte("\x7fELF"), []byte("body")...), 0o755)
	data, err = update.Build(version, "Fixes", "2026-10-23", map[string]string{update.BinaryDest: bin}, map[string]uint32{update.BinaryDest: 0o755})
	if err != nil {
		t.Fatal(err)
	}
	sig = update.Sign(priv, data)
	if !sign {
		sig = update.Sign(priv, []byte("something else"))
	}
	return
}

func updServer(t *testing.T, version string, sign bool) string {
	data, sig := updPackage(t, version, sign)
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v` + version + `","body":"Fixes","assets":[
		 {"name":"gatehouse-update-` + version + `.tar.gz","browser_download_url":"` + base + `/pkg","size":1},
		 {"name":"gatehouse-update-` + version + `.tar.gz.sig","browser_download_url":"` + base + `/sig","size":1}]}`))
	})
	mux.HandleFunc("/pkg", func(w http.ResponseWriter, r *http.Request) { w.Write(data) })
	mux.HandleFunc("/sig", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sig)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return base + "/latest"
}

func updBox(t *testing.T, api string, withKey bool) (*client, *fakeNet, string) {
	state := t.TempDir()
	var data string
	c, fn, _ := siteBoxWith(t, "192.168.1.1/24", func(cfg *Config) {
		cfg.Version, cfg.UpdateAPI, cfg.UpdateAllowHTTP, cfg.UpdateState = "0.1.5", api, true, state
		if withKey {
			pk, _ := update.ParsePublicKey(updTestPub)
			cfg.UpdateKey = pk
		}
		data = cfg.DataDir
	})
	return c, fn, data
}

// waitFor polls until the condition holds; the update runs in the background.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *fakeNet) updateGiven() string { f.mu.Lock(); defer f.mu.Unlock(); return f.updatePath }

func TestUpdateCheckAndInstall(t *testing.T) {
	c, fn, data := updBox(t, updServer(t, "0.2.0", true), true)
	rc, m, raw := c.call("POST", "/api/system/update/check", nil)
	if rc != 200 || m["available"] != true || m["gatehouse_new"] != true || m["current"] != "0.1.5" {
		t.Fatalf("check: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/system/update/install", map[string]any{}); rc != 200 {
		t.Fatalf("install: %d %s", rc, raw)
	}
	waitFor(t, "the helper to be handed the package", func() bool { return fn.updateGiven() != "" })
	if !strings.HasPrefix(fn.updateGiven(), filepath.Join(data, "update")) {
		t.Fatalf("helper was handed %q", fn.updateGiven())
	}
	if _, err := os.Stat(fn.updateGiven() + ".sig"); err != nil {
		t.Fatal("signature not stored next to the package")
	}
	if fn.sysStarted {
		t.Fatal("a Gatehouse update must go through the full update, not the system-only one")
	}
}

func TestUpdateRefusals(t *testing.T) {
	// not newer
	c, fn, _ := updBox(t, updServer(t, "0.1.5", true), true)
	_, m, _ := c.call("POST", "/api/system/update/check", nil)
	if m["available"] == true {
		t.Fatal("same version offered")
	}
	if rc, _, _ := c.call("POST", "/api/system/update/install", map[string]any{}); rc == 200 || fn.updateGiven() != "" {
		t.Fatal("installed a version that is not newer")
	}
	// nothing checked yet
	c, fn, _ = updBox(t, updServer(t, "0.2.0", true), true)
	if rc, _, _ := c.call("POST", "/api/system/update/install", map[string]any{}); rc == 200 || fn.updateGiven() != "" {
		t.Fatal("installed without a check")
	}
	// bad signature: the run starts, then reports why it stopped, and nothing reaches the helper
	c, fn, _ = updBox(t, updServer(t, "0.2.0", false), true)
	c.call("POST", "/api/system/update/check", nil)
	c.call("POST", "/api/system/update/install", map[string]any{})
	waitFor(t, "the bad package to be refused", func() bool {
		_, m, _ := c.call("GET", "/api/system/update", nil)
		return m["working"] == false && m["run_error"] != nil && m["run_error"] != ""
	})
	if fn.updateGiven() != "" {
		t.Fatal("installed an unsigned package")
	}
}

func TestUpdateSettings(t *testing.T) {
	c, _, _ := updBox(t, updServer(t, "0.2.0", true), true)
	if rc, _, _ := c.call("PUT", "/api/system/update/settings", map[string]any{"repo": "not a repo"}); rc == 200 {
		t.Fatal("bad repo accepted")
	}
	if rc, _, _ := c.call("PUT", "/api/system/update/settings", map[string]any{"repo": "someone/else"}); rc != 200 {
		t.Fatal("good repo refused")
	}
	for _, bad := range []map[string]any{{"mode": "sometimes"}, {"window": 24}, {"window": -1}} {
		if rc, _, _ := c.call("PUT", "/api/system/update/settings", bad); rc == 200 {
			t.Fatalf("accepted %v", bad)
		}
	}
	if rc, _, _ := c.call("PUT", "/api/system/update/settings", map[string]any{"mode": "auto", "window": 4}); rc != 200 {
		t.Fatal("mode refused")
	}
	_, m, _ := c.call("GET", "/api/system/update", nil)
	if m["mode"] != "auto" || m["window"] != float64(4) || m["repo"] != "someone/else" || m["key_set"] != true {
		t.Fatalf("%v", m)
	}
	// changing the mode must not reset the repo
	if _, m, _ = c.call("GET", "/api/system/update", nil); m["repo"] != "someone/else" {
		t.Fatalf("repo was reset: %v", m)
	}
	c2, _, _ := updBox(t, updServer(t, "0.2.0", true), true)
	if _, m, _ := c2.call("GET", "/api/system/update", nil); m["mode"] != "notify" || m["window"] != float64(3) {
		t.Fatalf("defaults: %v", m)
	}
}

// With no newer Gatehouse, waiting system updates still make something to install, and the
// install goes the system-only way.
func TestSystemUpdatesAloneAreOffered(t *testing.T) {
	c, fn, _ := updBox(t, updServer(t, "0.1.5", true), true)
	fn.sysUpdates = netd.SystemUpdates{Count: 4, Names: []string{"libc6", "openssl"}, RebootLikely: true}
	c.call("POST", "/api/system/update/check", nil)
	waitFor(t, "the system check", func() bool {
		_, m, _ := c.call("GET", "/api/system/update", nil)
		return m["checking"] == false && m["system_count"] == float64(4)
	})
	_, m, _ := c.call("GET", "/api/system/update", nil)
	if m["available"] != true || m["gatehouse_new"] == true {
		t.Fatalf("%v", m)
	}
	if rc, _, raw := c.call("POST", "/api/system/update/install", map[string]any{}); rc != 200 {
		t.Fatalf("install: %d %s", rc, raw)
	}
	waitFor(t, "the system-only update to start", func() bool { fn.mu.Lock(); defer fn.mu.Unlock(); return fn.sysStarted })
	if fn.updateGiven() != "" {
		t.Fatal("a package was installed though no newer Gatehouse exists")
	}
}

// A Gatehouse update brings the system updates with it and checks them, even if the count is 0.
func TestGatehouseUpdateIncludesSystemStep(t *testing.T) {
	c, _, _ := updBox(t, updServer(t, "0.2.0", true), true)
	c.call("POST", "/api/system/update/check", nil)
	// While preparing, the progress list names every step in order.
	steps := updateSteps(update.Plan{Gatehouse: true, System: true}, "system")
	var ids []string
	for _, st := range steps {
		ids = append(ids, st.ID+":"+st.State)
	}
	got := strings.Join(ids, " ")
	want := "download:done verify:done backup:done system:active install:pending restart:pending health:pending"
	if got != want {
		t.Fatalf("%s", got)
	}
	if len(updateSteps(update.Plan{System: true}, "system")) != 1 {
		t.Fatal("a system-only run should show one step")
	}
}

func TestAutomaticUpdateOnlyInTheWindow(t *testing.T) {
	c, fn, _ := updBox(t, updServer(t, "0.2.0", true), true)
	c.call("POST", "/api/system/update/check", nil)
	a := c.app
	at := func(h int) time.Time { return time.Date(2026, 10, 25, h, 5, 0, 0, time.Local) }
	a.autoInstall(at(14)) // wrong hour
	time.Sleep(50 * time.Millisecond)
	if fn.updateGiven() != "" {
		t.Fatal("updated outside the window")
	}
	a.autoInstall(at(3))
	waitFor(t, "the automatic update", func() bool { return fn.updateGiven() != "" })
	// a second try the same night does nothing
	if last := a.Config.Setting(updateAutoAtSetting, "0"); last == "0" {
		t.Fatal("the attempt was not recorded")
	}
}

package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestUpdateCheckAndInstall(t *testing.T) {
	c, fn, data := updBox(t, updServer(t, "0.2.0", true), true)
	rc, m, raw := c.call("POST", "/api/system/update/check", nil)
	if rc != 200 || m["available"] != true || m["current"] != "0.1.5" {
		t.Fatalf("check: %d %s", rc, raw)
	}
	if rc, _, raw := c.call("POST", "/api/system/update/install", map[string]any{}); rc != 200 {
		t.Fatalf("install: %d %s", rc, raw)
	}
	if !strings.HasPrefix(fn.updatePath, filepath.Join(data, "update")) {
		t.Fatalf("helper was handed %q", fn.updatePath)
	}
	if _, err := os.Stat(fn.updatePath + ".sig"); err != nil {
		t.Fatal("signature not stored next to the package")
	}
}

func TestUpdateRefusals(t *testing.T) {
	// not newer
	c, fn, _ := updBox(t, updServer(t, "0.1.5", true), true)
	_, m, _ := c.call("POST", "/api/system/update/check", nil)
	if m["available"] == true {
		t.Fatal("same version offered")
	}
	if rc, _, _ := c.call("POST", "/api/system/update/install", map[string]any{}); rc == 200 || fn.updatePath != "" {
		t.Fatal("installed a version that is not newer")
	}
	// nothing checked yet
	c, fn, _ = updBox(t, updServer(t, "0.2.0", true), true)
	if rc, _, _ := c.call("POST", "/api/system/update/install", map[string]any{}); rc == 200 || fn.updatePath != "" {
		t.Fatal("installed without a check")
	}
	// bad signature
	c, fn, _ = updBox(t, updServer(t, "0.2.0", false), true)
	c.call("POST", "/api/system/update/check", nil)
	if rc, _, raw := c.call("POST", "/api/system/update/install", map[string]any{}); rc == 200 || fn.updatePath != "" {
		t.Fatalf("installed an unsigned package: %d %s", rc, raw)
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
	if rc, _, _ := c.call("PUT", "/api/system/update/os-security", map[string]any{"enabled": true}); rc != 200 {
		t.Fatal("os security")
	}
	_, m, _ := c.call("GET", "/api/system/update", nil)
	if m["os_security"] != true || m["repo"] != "someone/else" || m["key_set"] != true {
		t.Fatalf("%v", m)
	}
}

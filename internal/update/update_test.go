package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A key pair made by OpenSSH, in the formats ssh-keygen writes.
const testPriv = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz
c2gtZWQyNTUxOQAAACBOfSjnkNwfOd66xbySynJJGY1YbBeYZGq66zwuLb1LtwAA
AIjLZc6Sy2XOkgAAAAtzc2gtZWQyNTUxOQAAACBOfSjnkNwfOd66xbySynJJGY1Y
bBeYZGq66zwuLb1LtwAAAEA1wLVyxuhSehBp4f0ZMNCmLUo4ewnkrmHU9oFf2XDS
zk59KOeQ3B853rrFvJLKckkZjVhsF5hkarrrPC4tvUu3AAAAAAECAwQF
-----END OPENSSH PRIVATE KEY-----
`
const testPub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE59KOeQ3B853rrFvJLKckkZjVhsF5hkarrrPC4tvUu3 test"

func TestKeysAndSignatures(t *testing.T) {
	priv, err := ParsePrivateKey(testPriv)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ParsePublicKey(testPub)
	if err != nil {
		t.Fatal(err)
	}
	if !priv.Public().(ed25519.PublicKey).Equal(pub) {
		t.Fatal("the private and public key are not a pair")
	}
	if PublicLine(pub) != strings.Join(strings.Fields(testPub)[:2], " ") {
		t.Fatal("public key does not round-trip")
	}
	sig := Sign(priv, []byte("hello"))
	if VerifySignature(pub, []byte("hello"), sig) != nil {
		t.Fatal("good signature refused")
	}
	if VerifySignature(pub, []byte("hellO"), sig) == nil || VerifySignature(pub, []byte("hello"), "AAAA") == nil {
		t.Fatal("bad signature accepted")
	}
	if k, err := EmbeddedKey(); err != nil && err != ErrNoKey {
		t.Fatalf("the built-in key is damaged: %v", err)
	} else if err == nil && len(k) != ed25519.PublicKeySize {
		t.Fatal("the built-in key is the wrong size")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if VerifySignature(other, []byte("hello"), sig) == nil {
		t.Fatal("signature accepted for the wrong key")
	}
}

func TestVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{{"1.0.0", "1.0.1", -1}, {"v1.2.0", "1.2", 0}, {"0.1.30", "0.2.0", -1}, {"dev", "0.0.1", -1}, {"2.0.0", "1.9.9", 1}, {"1.10.0", "1.9.0", 1}} {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("%s vs %s: %d", tc.a, tc.b, got)
		}
	}
}

func fakeProgram() []byte { return append([]byte("\x7fELF"), []byte("program body")...) }

func makePkg(t *testing.T, version string, mutate func(files map[string]string)) (data []byte, sig string, pub ed25519.PublicKey) {
	t.Helper()
	priv, _ := ParsePrivateKey(testPriv)
	pub, _ = ParsePublicKey(testPub)
	dir := t.TempDir()
	bin := filepath.Join(dir, "gatehouse")
	os.WriteFile(bin, fakeProgram(), 0o755)
	unit := filepath.Join(dir, "gatehouse.service")
	os.WriteFile(unit, []byte("[Unit]\nDescription=new\n"), 0o644)
	files := map[string]string{BinaryDest: bin, "/etc/systemd/system/gatehouse.service": unit}
	if mutate != nil {
		mutate(files)
	}
	data, err := Build(version, "notes", "2026-10-23", files, map[string]uint32{BinaryDest: 0o755})
	if err != nil {
		t.Fatal(err)
	}
	return data, Sign(priv, data), pub
}

func TestOpenRejectsTampering(t *testing.T) {
	data, sig, pub := makePkg(t, "1.0.0", nil)
	p, err := Open(data, sig, pub)
	if err != nil {
		t.Fatal(err)
	}
	if p.Manifest.Version != "1.0.0" || len(p.Manifest.Files) != 2 {
		t.Fatalf("%+v", p.Manifest)
	}
	bad := append([]byte(nil), data...)
	bad[len(bad)/2] ^= 0xff
	if _, err := Open(bad, sig, pub); err == nil {
		t.Fatal("tampered package accepted")
	}
	if _, err := Open(data, "", pub); err == nil {
		t.Fatal("unsigned package accepted")
	}
	// something an update may not touch
	d2, s2, pub2 := makePkg(t, "1.0.1", func(f map[string]string) { f["/etc/passwd"] = f[BinaryDest] })
	if _, err := Open(d2, s2, pub2); err == nil || !strings.Contains(err.Error(), "may not change") {
		t.Fatalf("forbidden path accepted: %v", err)
	}
	// a "program" that is not one
	d3, s3, pub3 := makePkg(t, "1.0.2", func(f map[string]string) {
		p := filepath.Join(t.TempDir(), "x")
		os.WriteFile(p, []byte("#!/bin/sh\nrm -rf /\n"), 0o755)
		f[BinaryDest] = p
	})
	if _, err := Open(d3, s3, pub3); err == nil {
		t.Fatal("a script was accepted as the program")
	}
}

type fakeBox struct {
	root     string
	healthy  bool // whether the console answers after the update
	runs     []string
	failWhen string
	now      time.Time
}

func (f *fakeBox) env(t *testing.T) Env {
	return Env{
		StateDir: filepath.Join(f.root, "state"), Root: f.root,
		Run: func(name string, args ...string) error {
			c := name + " " + strings.Join(args, " ")
			f.runs = append(f.runs, c)
			if f.failWhen != "" && strings.Contains(c, f.failWhen) {
				return os.ErrInvalid
			}
			return nil
		},
		Health: func() bool {
			b, _ := os.ReadFile(filepath.Join(f.root, BinaryDest))
			isNew := strings.Contains(string(b), "program body")
			if isNew {
				return f.healthy
			}
			return true // the previous version always answers
		},
		Sleep:    func(d time.Duration) { f.now = f.now.Add(d) },
		Now:      func() time.Time { return f.now },
		Services: []string{"gatehouse-netd", "gatehouse"},
		Wait:     30 * time.Second, RollbackWait: 30 * time.Second,
	}
}

func setupBox(t *testing.T, healthy bool) *fakeBox {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "usr/local/bin"), 0o755)
	os.MkdirAll(filepath.Join(root, "etc/systemd/system"), 0o755)
	os.WriteFile(filepath.Join(root, BinaryDest), []byte("\x7fELFold"), 0o755)
	os.WriteFile(filepath.Join(root, "etc/systemd/system/gatehouse.service"), []byte("[Unit]\nDescription=old\n"), 0o644)
	os.WriteFile(filepath.Join(root, "etc/gatehouse-release"), []byte("0.9.0\n"), 0o644)
	return &fakeBox{root: root, healthy: healthy, now: time.Unix(1000, 0)}
}

func TestApplySucceeds(t *testing.T) {
	box := setupBox(t, true)
	data, sig, pub := makePkg(t, "1.0.0", nil)
	p, _ := Open(data, sig, pub)
	res := Apply(box.env(t), p, "0.9.0")
	if !res.OK || res.RolledBack || res.To != "1.0.0" {
		t.Fatalf("%+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(box.root, BinaryDest))
	rel, _ := os.ReadFile(filepath.Join(box.root, "etc/gatehouse-release"))
	if !strings.Contains(string(b), "program body") || strings.TrimSpace(string(rel)) != "1.0.0" {
		t.Fatal("files not installed")
	}
	if fi, _ := os.Stat(filepath.Join(box.root, BinaryDest)); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if len(box.runs) < 3 || !strings.Contains(strings.Join(box.runs, "|"), "restart gatehouse-netd|systemctl restart gatehouse") {
		t.Fatalf("services: %v", box.runs)
	}
	if r, ok := ReadResult(box.env(t).StateDir); !ok || !r.OK {
		t.Fatal("result not saved")
	}
}

func TestApplyRollsBackWhenConsoleStaysDown(t *testing.T) {
	box := setupBox(t, false)
	data, sig, pub := makePkg(t, "1.0.0", nil)
	p, _ := Open(data, sig, pub)
	res := Apply(box.env(t), p, "0.9.0")
	if res.OK || !res.RolledBack {
		t.Fatalf("%+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(box.root, BinaryDest))
	u, _ := os.ReadFile(filepath.Join(box.root, "etc/systemd/system/gatehouse.service"))
	rel, _ := os.ReadFile(filepath.Join(box.root, "etc/gatehouse-release"))
	if string(b) != "\x7fELFold" || !strings.Contains(string(u), "old") || strings.TrimSpace(string(rel)) != "0.9.0" {
		t.Fatalf("not restored: %q %q %q", b, u, rel)
	}
	if strings.Contains(res.Error, "Alt+F2") {
		t.Fatal("the old version answers, so no warning was needed")
	}
}

func TestApplyRollsBackWhenRestartFails(t *testing.T) {
	box := setupBox(t, true)
	box.failWhen = "restart gatehouse-netd"
	data, sig, pub := makePkg(t, "1.0.0", nil)
	p, _ := Open(data, sig, pub)
	if res := Apply(box.env(t), p, "0.9.0"); res.OK || !res.RolledBack {
		t.Fatalf("%+v", res)
	}
}

func TestReleaseLookup(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.2.0","body":"What changed","published_at":"2026-10-23T00:00:00Z","assets":[
		 {"name":"gatehouse-1.2.0-amd64.iso","browser_download_url":"` + base + `/iso","size":1},
		 {"name":"gatehouse-update-1.2.0.tar.gz","browser_download_url":"` + base + `/pkg","size":5},
		 {"name":"gatehouse-update-1.2.0.tar.gz.sig","browser_download_url":"` + base + `/sig","size":5}]}`))
	})
	mux.HandleFunc("/pkg", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("PKG")) })
	mux.HandleFunc("/sig", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("SIG")) })
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.3.0","assets":[{"name":"x.iso","browser_download_url":"` + base + `/iso"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL
	s := Source{API: base + "/latest", AllowHTTP: true}
	r, err := s.Latest(context.Background())
	if err != nil || r.Version != "1.2.0" || r.Notes != "What changed" {
		t.Fatalf("%+v %v", r, err)
	}
	pkg, sig, err := s.Fetch(context.Background(), r)
	if err != nil || string(pkg) != "PKG" || sig != "SIG" {
		t.Fatalf("%q %q %v", pkg, sig, err)
	}
	if _, err := (Source{API: base + "/empty", AllowHTTP: true}).Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "no update package") {
		t.Fatalf("%v", err)
	}
	if _, err := (Source{API: base + "/latest"}).Latest(context.Background()); err == nil {
		t.Fatal("plain http accepted outside tests")
	}
}

package netd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnboundConfigForEachPlan(t *testing.T) {
	on := UnboundConfig(UnboundPlan{Enabled: true, DNSSEC: true, IPv6: true})
	for _, want := range []string{"port: 5335", "interface: 127.0.0.1", "access-control: 0.0.0.0/0 refuse", "do-ip6: yes", `module-config: "validator iterator"`} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q in\n%s", want, on)
		}
	}
	off := UnboundConfig(UnboundPlan{Enabled: true})
	if !strings.Contains(off, `module-config: "iterator"`) || !strings.Contains(off, "do-ip6: no") {
		t.Errorf("no DNSSEC, no IPv6:\n%s", off)
	}
	if strings.Contains(off, "interface: 0.0.0.0") {
		t.Error("Unbound must only listen on this machine")
	}
}

func TestUnboundApplyChecksBeforeStarting(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root}
	if err := s.UnboundApply(UnboundPlan{Enabled: true}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("not installed: %v", err)
	}
	os.MkdirAll(filepath.Join(root, "usr/sbin"), 0o755)
	os.WriteFile(filepath.Join(root, unboundBinary), []byte("x"), 0o755)
	os.MkdirAll(filepath.Join(root, "usr/share/dns"), 0o755)
	os.WriteFile(filepath.Join(root, unboundAnchorShipped), []byte(". 86400 IN DS 20326 8 2 AAAA\n"), 0o644)
	var calls []string
	reject := false
	old := sysRun
	t.Cleanup(func() { sysRun = old })
	sysRun = func(_ time.Duration, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "unbound-checkconf" && reject {
			return "bad option", os.ErrInvalid
		}
		return "active", nil
	}
	if err := s.UnboundApply(UnboundPlan{Enabled: true, DNSSEC: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, unboundConf))
	if !strings.Contains(string(b), "validator") {
		t.Fatal("settings not written")
	}
	joined := strings.Join(calls, "|")
	if !strings.Contains(joined, "unbound-checkconf") || !strings.Contains(joined, "enable unbound.service") || !strings.Contains(joined, "restart unbound.service") {
		t.Fatalf("calls: %s", joined)
	}
	if strings.Index(joined, "unbound-checkconf") > strings.Index(joined, "restart") {
		t.Fatal("the settings must be checked before Unbound is restarted")
	}
	// a rejected file is taken out again, and Unbound is not restarted
	reject, calls = true, nil
	if err := s.UnboundApply(UnboundPlan{Enabled: true}); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("rejected: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(root, unboundConf))
	if !strings.Contains(string(b), "validator") {
		t.Fatal("the earlier good settings should be back")
	}
	if strings.Contains(strings.Join(calls, "|"), "restart") {
		t.Fatal("must not restart on rejected settings")
	}
	// turning it off removes the file and stops the service
	reject, calls = false, nil
	if err := s.UnboundApply(UnboundPlan{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, unboundConf)); err == nil {
		t.Fatal("settings file should be gone")
	}
	if !strings.Contains(strings.Join(calls, "|"), "disable --now unbound.service") {
		t.Fatalf("%v", calls)
	}
}

// The image ships Unbound's settings as plain text in its build hook. They must be the same as what
// NopeWarden writes itself, so a fresh install and an updated machine behave alike.
func TestImageUnboundSettingsMatchWhatNopeWardenWrites(t *testing.T) {
	b, err := os.ReadFile("../../iso/config/hooks/normal/0100-nopewarden.hook.chroot")
	if err != nil {
		t.Skip("image sources not here")
	}
	s := string(b)
	i := strings.Index(s, "<<'EOU'\n")
	j := strings.Index(s, "\nEOU\n")
	if i < 0 || j < 0 {
		t.Fatal("the hook does not write Unbound's settings")
	}
	got := s[i+len("<<'EOU'\n") : j+1]
	if want := UnboundConfig(UnboundPlan{DNSSEC: true}); got != want {
		t.Fatalf("the image's settings differ from UnboundConfig:\n--- image\n%s\n--- code\n%s", got, want)
	}
	if !strings.Contains(s, "unbound-resolvconf") {
		t.Error("the hook must keep unbound-resolvconf from rewriting resolv.conf")
	}
}

// A fresh install has no trust anchor file yet (the service makes it on its first start), and Unbound's own
// settings check fails without it. It must be made first, from the key Debian ships.
func TestUnboundApplyMakesTheMissingTrustAnchor(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root}
	os.MkdirAll(filepath.Join(root, "usr/sbin"), 0o755)
	os.WriteFile(filepath.Join(root, unboundBinary), []byte("x"), 0o755)
	os.MkdirAll(filepath.Join(root, "usr/share/dns"), 0o755)
	os.WriteFile(filepath.Join(root, unboundAnchorShipped), []byte(". 86400 IN DS 20326 8 2 AAAA\n"), 0o644)
	old := sysRun
	t.Cleanup(func() { sysRun = old })
	sysRun = func(_ time.Duration, name string, args ...string) (string, error) {
		if name == "unbound-checkconf" {
			if _, err := os.Stat(filepath.Join(root, unboundAnchor)); err != nil {
				return "auto-trust-anchor-file does not exist", os.ErrInvalid
			}
		}
		return "active", nil
	}
	if err := s.UnboundApply(UnboundPlan{Enabled: true, DNSSEC: true}); err != nil {
		t.Fatalf("a fresh install must be able to turn Resolve directly on: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, unboundAnchor))
	if err != nil || !strings.Contains(string(b), "20326") {
		t.Fatalf("the key was not copied: %v %q", err, b)
	}
	// a key that is already there is left alone
	os.WriteFile(filepath.Join(root, unboundAnchor), []byte("kept"), 0o644)
	if err := s.UnboundApply(UnboundPlan{Enabled: true, DNSSEC: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, unboundAnchor)); string(b) != "kept" {
		t.Fatalf("an existing key was overwritten: %q", b)
	}
}

// With no shipped key the package's helper is asked, and when nothing can make the file the person is told
// in plain words (not Unbound's own error), and nothing is written or restarted.
func TestUnboundApplyExplainsWhenTheTrustAnchorCannotBeMade(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root}
	os.MkdirAll(filepath.Join(root, "usr/sbin"), 0o755)
	os.WriteFile(filepath.Join(root, unboundBinary), []byte("x"), 0o755)
	var calls []string
	old := sysRun
	t.Cleanup(func() { sysRun = old })
	sysRun = func(_ time.Duration, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", os.ErrInvalid
	}
	err := s.UnboundApply(UnboundPlan{Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "DNSSEC key file") || strings.Contains(err.Error(), "checkconf") {
		t.Fatalf("plain message expected: %v", err)
	}
	j := strings.Join(calls, "|")
	if !strings.Contains(j, "unbound-helper root_trust_anchor_update") || !strings.Contains(j, "unbound-anchor -a") {
		t.Fatalf("both fallbacks should be tried: %s", j)
	}
	if strings.Contains(j, "restart") || strings.Contains(j, "unbound-checkconf") {
		t.Fatalf("must stop before checking or starting: %s", j)
	}
	if _, err := os.Stat(filepath.Join(root, unboundConf)); err == nil {
		t.Fatal("no settings file should be left")
	}
}

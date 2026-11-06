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

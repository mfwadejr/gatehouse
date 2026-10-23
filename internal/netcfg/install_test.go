package netcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type env struct {
	in      *Installer
	reloads int
	started [][]string
}

func newEnv(t *testing.T) *env {
	d := t.TempDir()
	e := &env{}
	e.in = &Installer{
		NetworkdDir: filepath.Join(d, "network"), PPPDir: filepath.Join(d, "ppp/peers"),
		PPPSecrets: filepath.Join(d, "ppp/chap-secrets"), SysctlFile: filepath.Join(d, "sysctl.conf"),
		StateDir: filepath.Join(d, "state"), Now: time.Now,
		Reload: func() error { e.reloads++; return nil },
		Services: func(start, stop []string) error {
			e.started = append(e.started, append(append([]string{}, start...), "|"), stop)
			return nil
		},
	}
	os.MkdirAll(e.in.NetworkdDir, 0o755)
	return e
}

func TestApplyConfirmRollback(t *testing.T) {
	e := newEnv(t)
	// Something already there, owned by someone else, must survive.
	other := filepath.Join(e.in.NetworkdDir, "50-custom.network")
	os.WriteFile(other, []byte("[Match]\nName=wg0\n"), 0o644)
	// And a hand-written secret.
	os.MkdirAll(filepath.Dir(e.in.PPPSecrets), 0o755)
	os.WriteFile(e.in.PPPSecrets, []byte("\"admin\" * \"mine\" *\n"), 0o600)

	o1, _ := Generate(cfg())
	tok, _, err := e.in.Apply(o1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.in.NetworkdDir, "gatehouse-eth1.network")); err != nil {
		t.Fatal("network file not written")
	}
	sec, _ := os.ReadFile(e.in.PPPSecrets)
	if !strings.Contains(string(sec), "\"admin\" * \"mine\" *") || !strings.Contains(string(sec), "s3cret") {
		t.Fatalf("secrets file: %q", sec)
	}
	if fi, _ := os.Stat(e.in.PPPSecrets); fi.Mode().Perm() != 0o600 {
		t.Fatalf("secrets mode %v", fi.Mode())
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("a file we do not own was removed")
	}
	if _, _, err := e.in.Apply(o1, time.Minute); err != ErrPending {
		t.Fatalf("stacked apply: %v", err)
	}
	if err := e.in.Confirm("bad"); err == nil {
		t.Fatal("bad token accepted")
	}
	if err := e.in.Confirm(tok); err != nil {
		t.Fatal(err)
	}

	// Second change drops the PPPoE link and the VLAN, then is rolled back.
	c2 := cfg()
	c2.WANs = c2.WANs[:1]
	c2.Interfaces = c2.Interfaces[:1]
	o2, _ := Generate(c2)
	if _, _, err := e.in.Apply(o2, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.in.NetworkdDir, "gatehouse-eth1.20.netdev")); err == nil {
		t.Fatal("stale file not removed")
	}
	sec, _ = os.ReadFile(e.in.PPPSecrets)
	if strings.Contains(string(sec), "s3cret") || !strings.Contains(string(sec), "mine") {
		t.Fatalf("secrets after removal: %q", sec)
	}
	if err := e.in.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.in.NetworkdDir, "gatehouse-eth1.20.netdev")); err != nil {
		t.Fatal("rollback did not restore the VLAN file")
	}
	sec, _ = os.ReadFile(e.in.PPPSecrets)
	if !strings.Contains(string(sec), "s3cret") {
		t.Fatal("rollback did not restore the secret")
	}
	if _, _, ok := e.in.Pending(); ok {
		t.Fatal("pending left after rollback")
	}
	if e.reloads < 3 {
		t.Fatalf("expected reloads for each apply and the rollback, got %d", e.reloads)
	}
}

func TestWatchdogRollsBackUnconfirmed(t *testing.T) {
	e := newEnv(t)
	o, _ := Generate(cfg())
	tok, dl, err := e.in.Apply(o, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunWatchdog(e.in, dl, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.in.NetworkdDir, "gatehouse-eth1.network")); err == nil {
		t.Fatal("unconfirmed change survived the watchdog")
	}
	// Confirmed changes are left alone.
	tok, dl, _ = e.in.Apply(o, 300*time.Millisecond)
	e.in.Confirm(tok)
	RunWatchdog(e.in, dl, tok)
	if _, err := os.Stat(filepath.Join(e.in.NetworkdDir, "gatehouse-eth1.network")); err != nil {
		t.Fatal("confirmed change was undone")
	}
}

func TestFailedReloadRestores(t *testing.T) {
	e := newEnv(t)
	o, _ := Generate(cfg())
	e.in.Reload = func() error { return os.ErrInvalid }
	if _, _, err := e.in.Apply(o, time.Minute); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(filepath.Join(e.in.NetworkdDir, "gatehouse-eth1.network")); err == nil {
		t.Fatal("failed apply left files behind")
	}
}

package fw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const lockoutScript = `add table inet gatehouse
delete table inet gatehouse
table inet gatehouse {
	chain input {
		type filter hook input priority filter; policy drop;
		iifname "lo" accept
	}
}
`

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gatehouse")
	root, _ := filepath.Abs("../..")
	c := exec.Command("go", "build", "-o", bin, "./cmd/gatehouse")
	c.Dir = root
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// A watchdog process, launched as a separate program, undoes an unconfirmed change.
func TestNetnsWatchdogRestoresAccess(t *testing.T) {
	l := newLab(t)
	bin := buildBinary(t)
	l.listen("ghfw", "0.0.0.0", 443)
	time.Sleep(500 * time.Millisecond)

	dir := t.TempDir()
	a := NewApplier(dir)
	a.Netns = "ghfw"
	a.Baseline, _ = Compile(labModel())
	a.Watchdog = func(dir string, deadline time.Time, token string) error {
		c := exec.Command(bin, "rollback", "-dir", dir, "-token", token, "-deadline", strconv.FormatInt(deadline.Unix(), 10), "-netns", "ghfw")
		c.SysProcAttr = detachAttr()
		return c.Start()
	}
	// Start from a known good ruleset.
	good, _ := Compile(labModel())
	tok, _, err := a.Apply(good, 2*time.Second, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Confirm(tok); err != nil {
		t.Fatal(err)
	}
	if !l.reach("ghhome", "192.168.1.1", 443) {
		t.Fatal("setup: console not reachable")
	}

	// Apply a change that cuts the administrator off, and do not confirm it.
	tok, deadline, err := a.Apply(lockoutScript, 2*time.Second, "test")
	if err != nil {
		t.Fatal(err)
	}
	if l.reach("ghhome", "192.168.1.1", 443) {
		t.Fatal("the lockout script did not lock anything out; the test proves nothing")
	}
	if _, _, ok := a.Pending(); !ok {
		t.Fatal("change should be pending")
	}
	// A second change cannot be stacked on top of an unconfirmed one.
	if _, _, err := a.Apply(good, time.Second, "test"); err != ErrPending {
		t.Fatalf("second apply: %v", err)
	}
	time.Sleep(time.Until(deadline) + 2500*time.Millisecond)
	if !l.reach("ghhome", "192.168.1.1", 443) {
		t.Fatal("the watchdog did not restore access")
	}
	if _, _, ok := a.Pending(); ok {
		t.Fatal("pending state not cleared")
	}
	if err := a.Confirm(tok); err == nil {
		t.Fatal("a rolled-back change could still be confirmed")
	}
}

func TestNetnsConfirmKeepsChange(t *testing.T) {
	l := newLab(t)
	l.listen("ghfw", "0.0.0.0", 443)
	l.listen("ghinet", "0.0.0.0", 80)
	time.Sleep(500 * time.Millisecond)

	a := NewApplier(t.TempDir())
	a.Netns = "ghfw"
	done := make(chan struct{}, 1)
	a.Watchdog = func(dir string, deadline time.Time, token string) error {
		go func() { RunWatchdog(dir, deadline, token, a); done <- struct{}{} }()
		return nil
	}
	m := labModel()
	script, _ := Compile(m)
	tok, _, err := a.Apply(script, 1*time.Second, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Confirm("wrong"); err == nil {
		t.Fatal("wrong token accepted")
	}
	if err := a.Confirm(tok); err != nil {
		t.Fatal(err)
	}
	<-done // the watchdog wakes and finds nothing to do
	if !l.reach("ghhome", "198.51.100.7", 80) {
		t.Fatal("confirmed change was undone")
	}
	saved, _ := os.ReadFile(filepath.Join(a.Dir, "lastgood.nft"))
	if string(saved) != script {
		t.Fatal("last known good was not updated")
	}

	// Boot discards an unconfirmed change and restores the last good one.
	a.Watchdog = nil
	if _, _, err := a.Apply(lockoutScript, time.Minute, "test"); err != nil {
		t.Fatal(err)
	}
	if l.reach("ghhome", "192.168.1.1", 443) {
		t.Fatal("lockout script had no effect")
	}
	if err := a.Boot(); err != nil {
		t.Fatal(err)
	}
	if !l.reach("ghhome", "192.168.1.1", 443) || !l.reach("ghhome", "198.51.100.7", 80) {
		t.Fatal("boot did not restore the last good ruleset")
	}
}

func TestApplyRejectsBadScriptWithoutChange(t *testing.T) {
	need(t)
	a := NewApplier(t.TempDir())
	a.Watchdog = nil
	_, _, err := a.Apply("this is not nft\n", time.Second, "t")
	if err == nil || !strings.Contains(err.Error(), "nft") {
		t.Fatalf("expected a parse error, got %v", err)
	}
	if _, _, ok := a.Pending(); ok {
		t.Fatal("a rejected script left pending state behind")
	}
}

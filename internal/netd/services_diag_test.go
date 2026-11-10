package netd

import (
	"testing"
	"time"
)

func TestLastUnitErrorPrefersTheProgramsOwnError(t *testing.T) {
	old := sysRun
	t.Cleanup(func() { sysRun = old })
	sysRun = func(_ time.Duration, name string, args ...string) (string, error) {
		return "i: suricata: This is Suricata\nE: detect: error parsing signature from file x.rules at line 1\nE: suricata: Loading signatures failed.\nnopewarden-suricata.service: Failed with result 'exit-code'.\n", nil
	}
	got := lastUnitError("nopewarden-suricata.service")
	if got != "E: suricata: Loading signatures failed." {
		t.Fatalf("got %q", got)
	}
}

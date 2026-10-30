package ha

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestWithoutTheWarmOptionTheScriptIsTheEarlierOne(t *testing.T) {
	if NotifyScriptFor(false) != NotifyScript {
		t.Fatal("the plain script must not change")
	}
	if NotifyScriptFor(true) == NotifyScript {
		t.Fatal("warm should differ")
	}
}

func TestWarmScriptKeepsThePortSilentThenTakesOverInOrder(t *testing.T) {
	s := NotifyScriptWarm
	for _, want := range []string{"Unmanaged=yes", "ip addr flush dev", "arp off multicast off", "disable_ipv6=1", "arping -q -U", "address \"$2\""} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// On takeover: wait for the old active unit, copy the address, then bring the port up.
	m := s[strings.Index(s, "MASTER)"):strings.Index(s, "BACKUP|FAULT")]
	order := []string{"sleep 1", "go_loud", "set_mac", "bring_up", "announce"}
	last := -1
	for _, w := range order {
		i := strings.Index(m, w)
		if i < 0 || i < last {
			t.Fatalf("%q is missing or out of order in the takeover steps:\n%s", w, m)
		}
		last = i
	}
	// The standby never gets an address of its own.
	b := s[strings.Index(s, "BACKUP|FAULT"):]
	if strings.Contains(b, "bring_up") || strings.Contains(b, "networkctl reconfigure") {
		t.Fatal("a standby unit must not ask the provider for anything")
	}
}

func TestWarmScriptIsValidShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	f := t.TempDir() + "/n.sh"
	os.WriteFile(f, []byte(NotifyScriptWarm), 0o755)
	if out, err := exec.Command(sh, "-n", f).CombinedOutput(); err != nil {
		t.Fatalf("the script has a syntax error: %s", out)
	}
	g := t.TempDir() + "/p.sh"
	os.WriteFile(g, []byte(NotifyScript), 0o755)
	if out, err := exec.Command(sh, "-n", g).CombinedOutput(); err != nil {
		t.Fatalf("the plain script has a syntax error: %s", out)
	}
}

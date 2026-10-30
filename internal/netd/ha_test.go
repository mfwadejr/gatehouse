package netd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gatehouse/internal/ha"
)

func TestHAApplyRefusesUnknownText(t *testing.T) {
	s := &Service{Root: t.TempDir()}
	for name, p := range map[string]HAPlan{
		"not ours":       {Enabled: true, Keepalived: "global_defs {}", Conntrackd: "# Written by Gatehouse\n"},
		"empty":          {Enabled: true},
		"nul":            {Enabled: true, Keepalived: "# Written by Gatehouse\x00", Conntrackd: "# Written by Gatehouse\n"},
		"bad port names": {Enabled: true, Keepalived: "# Written by Gatehouse\n", Conntrackd: "# Written by Gatehouse\n", WanDevs: []string{"eth0; reboot"}},
	} {
		if err := s.HAApply(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Root, haKeepalived)); err == nil {
		t.Fatal("nothing may be written for a refused plan")
	}
}

func TestHAApplyOffRemovesFiles(t *testing.T) {
	s := &Service{Root: t.TempDir()}
	for _, f := range []string{haKeepalived, haConntrackd, haWan, strings.TrimPrefix(ha.NotifyPath, "/")} {
		p := filepath.Join(s.Root, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o600)
	}
	if err := s.HAApply(HAPlan{}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{haKeepalived, haConntrackd, haWan} {
		if _, err := os.Stat(filepath.Join(s.Root, f)); err == nil {
			t.Errorf("%s was left behind", f)
		}
	}
}

func TestHeartApplyWritesAndRemovesTheCableFile(t *testing.T) {
	s := &Service{Root: t.TempDir()}
	if err := s.heartApply("bad port;", "10.255.40.1/30"); err == nil {
		t.Fatal("bad port name accepted")
	}
	if err := s.heartApply("eth2", "nonsense"); err == nil {
		t.Fatal("bad address accepted")
	}
	if err := s.heartApply("eth2", "10.255.40.1/30"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Root, haHeartNet))
	for _, want := range []string{"Name=eth2", "Address=10.255.40.1/30", "LinkLocalAddressing=no", "RequiredForOnline=no"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q in %s", want, b)
		}
	}
	if strings.Contains(string(b), "Gateway") {
		t.Fatal("the cable must never carry a route out")
	}
	if err := s.heartApply("", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, haHeartNet)); err == nil {
		t.Fatal("the cable file was left behind")
	}
}

func TestHAApplyWarmWritesTheListsAndTheWarmScript(t *testing.T) {
	s := &Service{Root: t.TempDir()}
	base := HAPlan{Enabled: true, Keepalived: "# Written by Gatehouse\n", Conntrackd: "# Written by Gatehouse\n"}
	bad := base
	bad.WanMACs = []string{"eth0 not-a-mac"}
	if err := s.HAApply(bad); err == nil {
		t.Fatal("bad hardware address accepted")
	}
	bad = base
	bad.WanStatic = []string{"eth0 nonsense"}
	if err := s.HAApply(bad); err == nil {
		t.Fatal("bad address accepted")
	}
	warm := base
	warm.WarmDevs, warm.WanStatic, warm.WanMACs = []string{"eth0"}, []string{"eth0 203.0.113.9"}, []string{"eth0 aa:bb:cc:00:11:22"}
	// keepalived is not in the test box; the apply stops there, after writing the files.
	s.HAApply(warm)
	n, _ := os.ReadFile(filepath.Join(s.Root, strings.TrimPrefix(ha.NotifyPath, "/")))
	if !strings.Contains(string(n), "Unmanaged=yes") {
		t.Fatal("the warm script was not written")
	}
	for f, want := range map[string]string{haWanWarm: "eth0\n", haWanStatic: "eth0 203.0.113.9\n", haWanMACs: "eth0 aa:bb:cc:00:11:22\n"} {
		if b, _ := os.ReadFile(filepath.Join(s.Root, f)); string(b) != want {
			t.Errorf("%s = %q", f, b)
		}
	}
	// Without the option the earlier script is written.
	s.HAApply(base)
	n, _ = os.ReadFile(filepath.Join(s.Root, strings.TrimPrefix(ha.NotifyPath, "/")))
	if strings.Contains(string(n), "Unmanaged=yes") {
		t.Fatal("the plain script was replaced by the warm one")
	}
}

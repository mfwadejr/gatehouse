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

package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFallbackConfigGivesAddressAndDHCPServer(t *testing.T) {
	c := FallbackConfig("enp2s0")
	for _, want := range []string{"Name=enp2s0", "Address=192.168.100.1/24", "DHCPServer=yes", "PoolOffset=100"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q:\n%s", want, c)
		}
	}
}

func TestFallbackPortRules(t *testing.T) {
	linked := []Port{{Name: "enp1s0"}, {Name: "enp2s0", Link: true}}
	none := func(string) bool { return false }
	has := func(string) bool { return true }
	long := 2 * fallbackWait
	cases := []struct {
		name  string
		setup string
		mgmt  string
		ps    []Port
		addr  func(string) bool
		wait  time.Duration
		want  string
	}{
		{"nothing answered for a while", "account", "enp2s0", linked, none, long, "enp2s0"},
		{"not yet 30 seconds", "account", "enp2s0", linked, none, time.Second, ""},
		{"a router answered", "account", "enp2s0", linked, has, long, ""},
		{"setup finished", "ready", "enp2s0", linked, none, long, ""},
		{"wizard still open", "wizard", "enp2s0", linked, none, long, "enp2s0"},
		{"no cable in the management port", "account", "enp1s0", linked, none, long, ""},
		{"no choice recorded: the only cable", "account", "", linked, none, long, "enp2s0"},
		{"no choice recorded: two cables", "account", "", []Port{{Name: "a", Link: true}, {Name: "b", Link: true}}, none, long, ""},
	}
	for _, c := range cases {
		if got := fallbackPort(c.setup, c.mgmt, c.ps, c.addr, c.wait); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestFallbackAppearsAndGoesAway(t *testing.T) {
	dir := t.TempDir()
	FallbackFile = filepath.Join(dir, "net", "05-fb.network")
	MgmtPortPath = filepath.Join(dir, "mgmt-port")
	os.WriteFile(MgmtPortPath, []byte("enp2s0 aa:bb\n"), 0o644)
	now := time.Unix(1000, 0)
	var reloads []string
	f := &fallback{now: func() time.Time { return now }, reload: func(p string) { reloads = append(reloads, p) }}
	ports := []Port{{Name: "enp2s0", MAC: "aa:bb", Link: true}}
	none := func(string) bool { return false }
	f.update("account", ports, none)
	if _, err := os.Stat(FallbackFile); err == nil {
		t.Fatal("fallback written before waiting")
	}
	now = now.Add(31 * time.Second)
	f.update("account", ports, none)
	b, err := os.ReadFile(FallbackFile)
	if err != nil || !strings.Contains(string(b), "Name=enp2s0") || len(reloads) != 1 {
		t.Fatalf("fallback not written: %v %v", err, reloads)
	}
	// It stays while setup is unfinished, even though the port now has an address (its own).
	now = now.Add(time.Minute)
	f.update("wizard", ports, func(string) bool { return true })
	if _, err := os.Stat(FallbackFile); err != nil {
		t.Fatal("fallback removed while setup was still open")
	}
	f.update("ready", ports, func(string) bool { return true })
	if _, err := os.Stat(FallbackFile); err == nil {
		t.Fatal("fallback left behind after setup finished")
	}
}

func TestMgmtPortIsFoundByMAC(t *testing.T) {
	dir := t.TempDir()
	MgmtPortPath = filepath.Join(dir, "mgmt-port")
	if MgmtPort() != "" {
		t.Fatal("a port was reported when none was chosen")
	}
	os.WriteFile(MgmtPortPath, []byte("enp9s0 00:00:00:00:00:00\n"), 0o644)
	if got := MgmtPort(); got != "enp9s0" { // the MAC is not on this machine, so the name stands
		t.Fatalf("%q", got)
	}
}

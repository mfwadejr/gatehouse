package installer

import (
	"net/netip"
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

func TestChosenAddressIsServedAtOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	FallbackFile = filepath.Join(dir, "net", "05-fb.network")
	MgmtPortPath = filepath.Join(dir, "mgmt-port")
	os.WriteFile(MgmtPortPath, []byte("enp2s0 aa:bb\n"), 0o644)
	now := time.Unix(1000, 0)
	var reloads []string
	f := &fallback{now: func() time.Time { return now }, reload: func(p string) { reloads = append(reloads, p) },
		addr: func() string { return "192.168.50.1/24" }}
	ports := []Port{{Name: "enp2s0", MAC: "aa:bb", Link: true}, {Name: "enp1s0", MAC: "cc:dd", Link: true}}
	// Even when a router has already given the port an address, the chosen one is used.
	f.update("account", ports, func(string) bool { return true })
	b, err := os.ReadFile(FallbackFile)
	if err != nil || !strings.Contains(string(b), "Name=enp2s0") || !strings.Contains(string(b), "Address=192.168.50.1/24") || strings.Contains(string(b), "DHCPServer") {
		t.Fatalf("chosen address not served: %v %q", err, b)
	}
	if strings.Contains(string(b), "enp1s0") {
		t.Fatal("the internet port must not get the management address")
	}
	// No cable, no address; cable back, address back.
	f.update("account", []Port{{Name: "enp2s0", MAC: "aa:bb", Link: false}}, func(string) bool { return false })
	if _, err := os.Stat(FallbackFile); err == nil {
		t.Fatal("address kept with no cable")
	}
	f.update("account", ports, func(string) bool { return false })
	if _, err := os.Stat(FallbackFile); err != nil {
		t.Fatal("address not restored when the cable returned")
	}
}

func TestInstallNetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	InstallNetPath = filepath.Join(dir, "install-net")
	if n := ReadInstallNet(); n.WanPort != "" || n.MgmtAddr != "" {
		t.Fatalf("empty: %+v", n)
	}
	r := &Request{WanPort: "enp1s0", WanMAC: "00:00:00:00:00:01", MgmtAddr: "192.168.50.1/24"}
	os.WriteFile(InstallNetPath, []byte(r.installNetBody()), 0o644)
	n := ReadInstallNet()
	if n.WanPort != "enp1s0" || n.MgmtAddr != "192.168.50.1/24" {
		t.Fatalf("read back: %+v", n)
	}
}

func TestMgmtAddrChecks(t *testing.T) {
	for in, ok := range map[string]bool{"192.168.50.1": true, "10.10.10.1/24": true, "8.8.8.1/24": false, "192.168.50.0/24": false,
		"192.168.50.255/24": false, "192.168.50.1/31": false, "nonsense": false, "192.168.50.1/8": false} {
		if _, err := ParseMgmtAddr(in); (err == nil) != ok {
			t.Errorf("%s: ok=%v err=%v", in, ok, err)
		}
	}
	got := SuggestMgmtAddr([]netip.Prefix{netip.MustParsePrefix("192.168.50.7/24")})
	if got != "192.168.77.1/24" {
		t.Errorf("should skip a used range: %s", got)
	}
	r := &Request{Disk: "/dev/sda", Confirm: "/dev/sda", WanPort: "a", MgmtPort: "a"}
	if err := Validate(r, []Disk{{Path: "/dev/sda"}}); err == nil {
		t.Error("the same port cannot be both")
	}
}

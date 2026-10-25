package app

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/dhcp"
	"gatehouse/internal/installer"
	"gatehouse/internal/seed"
)

// The network chosen in the installer is applied by itself on the first start.
func TestFirstBootAppliesInstallerChoices(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn, ff := &fakeNet{}, &fakeFW{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn, FW: ff})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	err = seed.Apply(a.Config, a.Box, &seed.Network{
		LAN: seed.LAN{Port: "enp2s0", Mode: "router", Address: "192.168.1.1/24"},
		WAN: &seed.WAN{Port: "enp1s0", Type: "dhcp"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()

	for i := 0; i < 100 && a.Config.Setting("net.autoapply", "") == "1"; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if a.Config.Setting("net.autoapply", "") != "0" {
		t.Fatal("the installer's choices were not applied")
	}
	fn.mu.Lock()
	defer fn.mu.Unlock()
	if len(fn.plan.Config.Interfaces) != 1 || fn.plan.Config.Interfaces[0].IPv4 != "192.168.1.1/24" || len(fn.plan.Config.WANs) != 1 {
		t.Fatalf("plan: %+v", fn.plan.Config)
	}
	if fn.pending || ff.pending {
		t.Fatal("first-boot changes must be confirmed, not left waiting")
	}
	if ff.script == "" {
		t.Fatal("no firewall was loaded")
	}
	if a.lastApplied() == "" {
		t.Fatal("the loaded firewall should count as confirmed")
	}
}

// A LAN set up to be shared with Wi-Fi is a one-port bridge, and everything that protects access
// to the device still follows it: the plan builds, the firewall loads and the setup port is
// allowed in under the bridge's name.
func TestFirstBootWithWifiShareKeepsTheDoorOpen(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn, ff := &fakeNet{}, &fakeFW{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn, FW: ff})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	err = seed.Apply(a.Config, a.Box, &seed.Network{WifiShare: true,
		LAN: seed.LAN{Port: "enp2s0", Mode: "router", Address: "192.168.1.1/24"},
		WAN: &seed.WAN{Port: "enp1s0", Type: "dhcp"}})
	if err != nil {
		t.Fatal(err)
	}
	a.Config.SetSetting("mgmt.rescue_iface", "enp2s0")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 100 && a.Config.Setting("net.autoapply", "") == "1"; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if a.Config.Setting("net.autoapply", "") != "0" {
		t.Fatal("the choices were not applied")
	}
	fn.mu.Lock()
	defer fn.mu.Unlock()
	var found bool
	for _, i := range fn.plan.Config.Interfaces {
		if i.Name == "br-lan" && i.Kind == "bridge" && len(i.Members) == 1 && i.Members[0] == "enp2s0" && i.IPv4 == "192.168.1.1/24" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan: %+v", fn.plan.Config.Interfaces)
	}
	if !strings.Contains(ff.script, "br-lan") || !strings.Contains(ff.script, "management console") {
		t.Fatal("the console must stay reachable from the bridged LAN")
	}
}

// Until the wizard is finished, a computer plugged into the management port gets an address and
// DNS from Gatehouse itself, using what the installer chose. Once the wizard has run, the
// temporary pool is gone.
func TestSetupPortHandsOutAddressesUntilTheWizardIsDone(t *testing.T) {
	dir := t.TempDir()
	installer.InstallNetPath = dir + "/install-net"
	installer.MgmtPortPath = dir + "/mgmt-port"
	os.WriteFile(installer.InstallNetPath, []byte("wan enp1s0 aa:aa\nmgmt 192.168.50.1/24\n"), 0o644)
	os.WriteFile(installer.MgmtPortPath, []byte("enp2s0 bb:bb\n"), 0o644)
	defer func() {
		installer.InstallNetPath = "/var/lib/gatehouse/install-net"
		installer.MgmtPortPath = "/var/lib/gatehouse/mgmt-port"
	}()
	web, dnsAddr := freeAddr(t), freeAddr(t)
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: &fakeNet{}, FW: &fakeFW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	in := dhcp.Ingress{Iface: "enp2s0", ServerIP: net.ParseIP("192.168.50.1").To4()}
	hw, _ := net.ParseMAC("de:ad:be:ef:00:01")
	disc := &dhcp.Packet{Op: 1, HType: 1, HLen: 6, XID: 9, CHAddr: hw, CIAddr: net.IPv4zero.To4(), YIAddr: net.IPv4zero.To4(), SIAddr: net.IPv4zero.To4(), GIAddr: net.IPv4zero.To4()}
	disc.Set(dhcp.OptMsgType, []byte{dhcp.Discover})
	off, err := a.DHCP.Handle(disc, in)
	if err != nil || off == nil || off.Type() != dhcp.Offer {
		t.Fatalf("a laptop on the management port got no offer: %v %v", off, err)
	}
	if got := off.YIAddr.String(); got != "192.168.50.100" {
		t.Fatalf("offered %s", got)
	}
	if dns := off.IPOption(dhcp.OptDNS); dns.String() != "192.168.50.1" {
		t.Fatalf("DNS option: %v", dns)
	}
	// The other port (the internet side) never gets one.
	if off, _ := a.DHCP.Handle(disc, dhcp.Ingress{Iface: "enp1s0", ServerIP: net.ParseIP("192.168.50.1").To4()}); off != nil {
		t.Fatal("the internet port must not hand out addresses")
	}

	// The wizard replaces it: the temporary pool disappears and the real one takes over.
	err = seed.Apply(a.Config, a.Box, &seed.Network{
		LAN: seed.LAN{Port: "enp2s0", Mode: "router", Address: "192.168.50.1/24"},
		WAN: &seed.WAN{Port: "enp1s0", Type: "dhcp"}})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	a.Config.QueryRow(`SELECT count(*) FROM dhcp_scopes WHERE name='Setup'`).Scan(&n)
	if n != 0 {
		t.Fatal("the temporary pool was left behind")
	}
	a.DHCP.Reload()
	off, err = a.DHCP.Handle(disc, in)
	if err != nil || off == nil || off.Type() != dhcp.Offer {
		t.Fatalf("no offer after the wizard: %v %v", off, err)
	}
}

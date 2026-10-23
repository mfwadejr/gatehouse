package netcfg

import (
	"strings"
	"testing"
)

func cfg() *Config {
	return &Config{
		Interfaces: []Interface{
			{Name: "eth1", Kind: "phys", IPv4: "192.168.1.1/24", PD: true, Enabled: true},
			{Name: "eth1.20", Kind: "vlan", Parent: "eth1", VLAN: 20, IPv4: "10.0.20.1/24", Enabled: true},
			{Name: "eth2", Kind: "phys", IPv4: "192.168.2.1/24", IPv6: "fd00:2::1/64", Enabled: true},
		},
		WANs: []WAN{
			{Name: "Fibre", Iface: "eth0", Type: "dhcp", VLAN: 100, V6Mode: "pd", Priority: 1, Enabled: true},
			{Name: "DSL Backup", Iface: "eth3", Type: "pppoe", PPPoEUser: "me@isp.example", PPPoEPass: "s3cret", V6Mode: "none", Priority: 2, Enabled: true},
		},
	}
}

func TestGenerate(t *testing.T) {
	o, err := Generate(cfg())
	if err != nil {
		t.Fatal(err)
	}
	must := func(file, want string) {
		t.Helper()
		got, ok := o.Networkd[file]
		if !ok {
			t.Fatalf("no file %s; have %v", file, keys(o.Networkd))
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %q:\n%s", file, want, got)
		}
	}
	must("gatehouse-eth0.100.netdev", "Id=100")
	must("gatehouse-eth0.100.network", "DHCP=yes")
	must("gatehouse-eth0.100.network", "RouteMetric=100")
	must("gatehouse-eth0.network", "VLAN=eth0.100")
	must("gatehouse-eth1.20.netdev", "Id=20")
	must("gatehouse-eth1.network", "VLAN=eth1.20")
	must("gatehouse-eth1.network", "DHCPPrefixDelegation=yes")
	must("gatehouse-eth1.network", "UplinkInterface=eth0.100")
	must("gatehouse-eth1.network", "Address=192.168.1.1/24")
	must("gatehouse-eth2.network", "Address=fd00:2::1/64")
	must("gatehouse-eth2.network", "IPv6SendRA=yes")
	must("gatehouse-eth3.network", "LinkLocalAddressing=no")
	if p := o.PPPPeers["gh-dslbacku"]; !strings.Contains(p, "plugin pppoe.so") || !strings.Contains(p, "nic-eth3") || !strings.Contains(p, "ifname ppp-dslbacku") || !strings.Contains(p, "defaultroute-metric 200") || !strings.Contains(p, "mtu 1492") {
		t.Errorf("ppp peer:\n%v", o.PPPPeers)
	}
	if strings.Contains(p0(o), "s3cret") {
		t.Error("password leaked into a peer file")
	}
	if !strings.Contains(o.Secrets, `"me@isp.example" * "s3cret" *`) {
		t.Errorf("secrets: %q", o.Secrets)
	}
	if len(o.PPPUnits) != 1 || o.PPPUnits[0] != "dslbacku" {
		t.Errorf("units: %v", o.PPPUnits)
	}
	if !strings.Contains(o.Sysctl, "net.ipv4.ip_forward=1") {
		t.Error("forwarding not enabled")
	}
	for name, body := range o.Networkd {
		if strings.Contains(body, "s3cret") {
			t.Errorf("%s contains the PPPoE password", name)
		}
	}
}

func p0(o *Output) string {
	var b strings.Builder
	for _, v := range o.PPPPeers {
		b.WriteString(v)
	}
	return b.String()
}

func keys(m map[string]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

func TestWanDeviceMatchesFirewallNaming(t *testing.T) {
	if got := WanDevice(WAN{Name: "DSL Backup", Type: "pppoe"}); got != "ppp-dslbacku" {
		t.Fatal(got)
	}
	if got := WanDevice(WAN{Iface: "eth0", Type: "dhcp", VLAN: 100}); got != "eth0.100" {
		t.Fatal(got)
	}
}

func TestStaticWAN(t *testing.T) {
	c := &Config{WANs: []WAN{{Name: "Office", Iface: "eth0", Type: "static", Addr: "203.0.113.10/29", Gateway: "203.0.113.9", DNS: "9.9.9.9", V6Mode: "static", V6Addr: "2001:db8::2/64", V6Gateway: "2001:db8::1", Priority: 1, Enabled: true}}}
	o, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	f := o.Networkd["gatehouse-eth0.network"]
	for _, want := range []string{"Address=203.0.113.10/29", "Gateway=203.0.113.9", "Address=2001:db8::2/64", "Gateway=2001:db8::1", "DNS=9.9.9.9"} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q:\n%s", want, f)
		}
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.Interfaces[0].Name = "eth1\n[Route]" },
		func(c *Config) { c.Interfaces[0].IPv4 = "192.168.1.1" },
		func(c *Config) { c.Interfaces[0].IPv4 = "fd00::1/64" },
		func(c *Config) { c.Interfaces[1].VLAN = 5000 },
		func(c *Config) { c.WANs[1].PPPoEUser = "me\" ; shell" },
		func(c *Config) { c.WANs[1].PPPoEPass = "pw\nuser evil" },
		func(c *Config) { c.WANs[0].Type = "carrier-pigeon" },
		func(c *Config) { c.WANs[0].Iface = "eth0 extra" },
		func(c *Config) { c.WANs[0].DNS = "1.1.1.1\nDNS=6.6.6.6" },
		func(c *Config) { c.WANs[0].MTU = 10 },
		func(c *Config) { c.WANs[1].Name = "fibre" }, // collides with the first after normalising
	}
	for i, f := range bad {
		c := cfg()
		if i == len(bad)-1 {
			c.WANs[1].Name = "Fib re"
		}
		f(c)
		if _, err := Generate(c); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestCGNAT(t *testing.T) {
	for _, ip := range []string{"100.64.1.2", "10.1.2.3", "192.168.0.5"} {
		if s, _ := CGNAT(ip); !s {
			t.Errorf("%s should be flagged", ip)
		}
	}
	if s, _ := CGNAT("203.0.113.9"); s {
		t.Error("public address flagged")
	}
}

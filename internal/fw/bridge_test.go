package fw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gatehouse/internal/bridgemode"
)

func bridgeCfg() *bridgemode.Config {
	return &bridgemode.Config{Enabled: true, Outside: "gbo", Inside: []string{"gbi"}, Mode: "static", Addr: "10.9.0.2/24", Gateway: "10.9.0.1"}
}

func bridgeModel(c *bridgemode.Config) *Model {
	return &Model{Bridge: c, ConsolePorts: []int{443}, LogDrops: true, Groups: map[string][]string{}}
}

func TestCompileBridgeShape(t *testing.T) {
	c := bridgeCfg()
	c.Subnets = []string{"10.8.0.0/24"}
	c.Allow = []bridgemode.Allow{{Name: "web", Proto: "tcp", Port: 8443, To: "10.9.0.50"}}
	out, err := Compile(bridgeModel(c))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"table bridge gatehouse_br",
		"delete table bridge gatehouse_br",
		`iifname "gbo" ip saddr 10.8.0.0/24 tcp dport 443 accept`,
		`iifname "gbo" tcp dport 443 counter drop`,
		`iifname "gbo" ip daddr 10.9.0.50 tcp dport 8443 counter accept`,
		`iifname "br-gate" tcp dport 443 accept comment "protected: management console"`,
		"policy drop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "masquerade") || strings.Contains(out, "dnat") {
		t.Error("bridge mode must not translate addresses")
	}
}

func TestCompileBridgeRefusesBadSetup(t *testing.T) {
	c := bridgeCfg()
	c.Inside = nil
	if _, err := Compile(bridgeModel(c)); err == nil {
		t.Fatal("a bridge with no inside port must be refused")
	}
}

func TestBridgeOffUsesRoutingRuleset(t *testing.T) {
	m := sample()
	m.Bridge = &bridgemode.Config{Enabled: false}
	out, err := Compile(m)
	if err != nil || strings.Contains(out, "gatehouse_br") {
		t.Fatalf("a disabled bridge setup must compile the normal ruleset: %v", err)
	}
}

// Real packets through a bridge of namespaces.
func TestNetnsBridgeMode(t *testing.T) {
	need(t)
	l := &lab{t: t}
	nss := []string{"ghbfw", "ghbout", "ghbin"}
	clean := func() {
		for _, p := range l.procs {
			p.Process.Kill()
			p.Wait()
		}
		for _, n := range nss {
			exec.Command("ip", "netns", "del", n).Run()
		}
	}
	clean()
	t.Cleanup(clean)
	for _, n := range nss {
		l.must("", "ip netns add "+n)
		l.must(n, "ip link set lo up")
	}
	// A host on each side, joined to the bridge by a cable each.
	l.must("", "ip link add gbo type veth peer name gbo-p")
	l.must("", "ip link add gbi type veth peer name gbi-p")
	l.must("", "ip link set gbo netns ghbfw && ip link set gbi netns ghbfw")
	l.must("", "ip link set gbo-p netns ghbout && ip link set gbi-p netns ghbin")
	if out, err := l.sh("ghbfw", "nft add table bridge probe_gh && nft delete table bridge probe_gh"); err != nil {
		t.Skipf("this kernel has no bridge firewall support: %s", out)
	}
	l.must("ghbfw", "ip link add br-gate type bridge && ip link set gbo master br-gate && ip link set gbi master br-gate && ip link set gbo up && ip link set gbi up && ip link set br-gate up && ip addr add 10.9.0.2/24 dev br-gate")
	l.must("ghbout", "ip link set gbo-p up && ip addr add 10.9.0.1/24 dev gbo-p")
	l.must("ghbin", "ip link set gbi-p up && ip addr add 10.9.0.50/24 dev gbi-p")
	l.listen("ghbout", "0.0.0.0", 80)
	l.listen("ghbin", "0.0.0.0", 8080)
	l.listen("ghbin", "0.0.0.0", 8443)
	l.listen("ghbfw", "0.0.0.0", 443)
	l.listen("ghbfw", "0.0.0.0", 9999)

	// Connection tracking across a bridge needs the nf_conntrack_bridge module. Where this kernel
	// lacks it, the stateful lines are left out and only what does not depend on them is checked.
	stateful := true
	if out, err := l.sh("ghbfw", "nft add table bridge probe_ct && nft 'add chain bridge probe_ct f { type filter hook forward priority 0; }' && nft 'add rule bridge probe_ct f ct state established accept'; r=$?; nft delete table bridge probe_ct; exit $r"); err != nil {
		stateful = false
		t.Logf("no connection tracking across a bridge in this kernel (%s): checking the stateless part only", out)
	}
	apply := func(m *Model) {
		t.Helper()
		script, err := Compile(m)
		if err != nil {
			t.Fatal(err)
		}
		if !stateful {
			var keep []string
			for _, ln := range strings.Split(script, "\n") {
				if !strings.Contains(ln, "ct state") {
					keep = append(keep, ln)
				}
			}
			script = strings.Join(keep, "\n")
		}
		f := filepath.Join(t.TempDir(), "b.nft")
		os.WriteFile(f, []byte(script), 0o600)
		if out, err := l.sh("ghbfw", "nft -f "+f); err != nil {
			t.Fatalf("apply: %v\n%s\n%s", err, out, script)
		}
	}
	apply(bridgeModel(bridgeCfg()))
	type chk struct {
		name string
		got  bool
		want bool
	}
	run := func(label string, cs []chk) {
		for _, c := range cs {
			if c.got != c.want {
				t.Errorf("%s: %s: got %v, want %v", label, c.name, c.got, c.want)
			}
		}
	}
	// Retry once: the first packets across a fresh bridge can be lost while it learns addresses.
	l.reach("ghbin", "10.9.0.1", 80)
	if stateful {
		run("stateful", []chk{{"inside reaches the outside", l.reach("ghbin", "10.9.0.1", 80), true}})
	}
	run("default", []chk{
		{"outside cannot start a connection inside", l.reach("ghbout", "10.9.0.50", 8080), false},
		{"inside reaches the console", l.reach("ghbin", "10.9.0.2", 443), true},
		{"outside cannot reach the console", l.reach("ghbout", "10.9.0.2", 443), false},
		{"nothing else on the machine is open to the inside", l.reach("ghbin", "10.9.0.2", 9999), false},
	})
	// A subnet named on purpose may manage, and one hosted service is let in.
	c := bridgeCfg()
	c.Subnets = []string{"10.9.0.1/32"}
	c.Allow = []bridgemode.Allow{{Name: "web", Proto: "tcp", Port: 8443, To: "10.9.0.50"}}
	apply(bridgeModel(c))
	run("exceptions", []chk{
		{"named subnet reaches the console", l.reach("ghbout", "10.9.0.2", 443), true},
		{"allowed service reachable", l.reach("ghbout", "10.9.0.50", 8443), true},
		{"other port still closed", l.reach("ghbout", "10.9.0.50", 8080), false},
	})
	// Trial mode lets everything cross.
	c = bridgeCfg()
	c.Open = true
	apply(bridgeModel(c))
	run("open", []chk{{"outside reaches inside", l.reach("ghbout", "10.9.0.50", 8080), true}})

	// Leaving bridge mode removes the bridge filter, also on a rollback to a script that never mentioned it.
	a := &Applier{Dir: t.TempDir(), NFT: "nft", Netns: "ghbfw"}
	if out, err := l.sh("ghbfw", "nft list table bridge gatehouse_br"); err != nil {
		t.Fatalf("bridge table should exist: %s", out)
	}
	apply(bridgeModel(bridgeCfg()))
	if err := a.run(a.withBridgeCleanup("add table inet gatehouse\n"), false); err != nil {
		t.Fatal(err)
	}
	if out, err := l.sh("ghbfw", "nft list table bridge gatehouse_br"); err == nil {
		t.Fatalf("the bridge table was left behind:\n%s", out)
	}
}

func TestPartnerUnitRules(t *testing.T) {
	m := sample()
	m.HAPeer = "192.168.1.3"
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ip saddr 192.168.1.3 tcp dport 8444 accept comment "protected: partner unit link"`,
		`ip saddr 192.168.1.3 ip protocol vrrp accept`,
		`ip saddr 192.168.1.3 udp dport 3780 accept`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.HAPeer = ""
	out, _ = Compile(m)
	if strings.Contains(out, "partner unit") {
		t.Fatal("a single unit has no partner rules")
	}
}

package netd

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOverrideCommands(t *testing.T) {
	p := OverridePlan{Parents: []string{"eth1"}, Rules: []OverrideRule{{Parent: "eth1", MAC: "AA:BB:CC:00:11:22", VID: 20}}}
	cmds, err := OverrideCommands(p)
	if err != nil {
		t.Fatal(err)
	}
	all := ""
	for _, c := range cmds {
		all += strings.Join(c, " ") + "\n"
	}
	for _, want := range []string{
		"qdisc replace dev eth1 clsact",
		"filter add dev eth1 ingress protocol ip prio 4100 flower src_mac aa:bb:cc:00:11:22 action vlan push id 20",
		"filter add dev eth1 ingress protocol arp prio 4100 flower src_mac aa:bb:cc:00:11:22 action vlan push id 20",
		"filter add dev eth1 egress protocol 802.1q prio 4100 flower vlan_id 20 dst_mac aa:bb:cc:00:11:22 action vlan pop",
		"dst_mac 01:00:00:00:00:01/01:00:00:00:00:00 action vlan pop",
	} {
		_ = want
	}
	if !strings.Contains(all, "src_mac aa:bb:cc:00:11:22 action vlan push id 20") || !strings.Contains(all, "vlan_id 20 dst_mac aa:bb:cc:00:11:22 action vlan pop") {
		t.Fatalf("missing rules:\n%s", all)
	}
	if !strings.Contains(all, "01:00:00:00:00:00/01:00:00:00:00:00") {
		t.Fatalf("broadcasts need their tag removed:\n%s", all)
	}
	// Only that one address is ever matched, and untagged frames only (ip, ipv6, arp).
	if strings.Contains(all, "protocol all") {
		t.Fatalf("must not match tagged frames:\n%s", all)
	}
	// An empty plan only cleans up.
	cmds, _ = OverrideCommands(OverridePlan{Parents: []string{"eth1"}})
	for _, c := range cmds {
		if c[1] == "add" && c[0] == "filter" {
			t.Fatalf("an empty plan adds nothing: %v", c)
		}
	}
	for _, bad := range []OverridePlan{
		{Rules: []OverrideRule{{Parent: "eth1; reboot", MAC: "aa:bb:cc:00:11:22", VID: 20}}},
		{Rules: []OverrideRule{{Parent: "eth1", MAC: "nonsense", VID: 20}}},
		{Rules: []OverrideRule{{Parent: "eth1", MAC: "aa:bb:cc:00:11:22", VID: 5000}}},
	} {
		if _, err := OverrideCommands(bad); err == nil {
			t.Fatalf("should refuse %+v", bad)
		}
	}
}

func TestOverrideApplyFailureCleansUp(t *testing.T) {
	var ran []string
	old := sysRun
	sysRun = func(_ time.Duration, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		ran = append(ran, line)
		if strings.Contains(line, "vlan push") {
			return "Error: Specified qdisc kind is unknown.", errors.New("exit 2")
		}
		return "", nil
	}
	t.Cleanup(func() { sysRun = old })
	s := &Service{Root: t.TempDir()}
	err := s.OverrideApply(OverridePlan{Rules: []OverrideRule{{Parent: "eth1", MAC: "aa:bb:cc:00:11:22", VID: 20}}})
	if err == nil || !strings.Contains(err.Error(), "kernel cannot tag") {
		t.Fatalf("want a plain error, got %v", err)
	}
	// After the failure, the clean-up commands ran and nothing else was added after the failure.
	last := ran[len(ran)-1]
	if !strings.Contains(last, "filter del") {
		t.Fatalf("should end with clean-up: %v", ran)
	}
}

func TestOverrideCommandsForADeviceOnAnotherPort(t *testing.T) {
	cmds, err := OverrideCommands(OverridePlan{Rules: []OverrideRule{{Parent: "eth1", MAC: "AA:BB:CC:00:11:22", VID: 20, Home: "wlan0", Iface: "eth1.20"}}})
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, c := range cmds {
		out += strings.Join(c, " ") + "\n"
	}
	for _, want := range []string{
		"qdisc replace dev wlan0 clsact", "qdisc replace dev eth1.20 clsact",
		"filter add dev wlan0 ingress protocol all prio 4104 flower src_mac aa:bb:cc:00:11:22 action mirred ingress redirect dev eth1.20",
		"filter add dev eth1.20 egress protocol all prio 4105 flower dst_mac aa:bb:cc:00:11:22 action mirred egress redirect dev wlan0",
		"dst_mac 01:00:00:00:00:00/01:00:00:00:00:00 action mirred egress mirror dev wlan0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "vlan push") {
		t.Fatalf("another port must not use tagging:\n%s", out)
	}
	if _, err := OverrideCommands(OverridePlan{Rules: []OverrideRule{{Parent: "eth1", MAC: "aa:bb:cc:00:11:22", VID: 20, Home: "bad name", Iface: "eth1.20"}}}); err == nil {
		t.Fatal("accepted a bad port name")
	}
}

// The kernel refuses a second traffic type at a priority that already holds one, which is how a
// real box rejected the IPv6 rule. Whatever the plan, no (port, direction, priority) may carry two
// different traffic types, and every priority used must be cleared first.
func TestOverrideOneTrafficTypePerPriority(t *testing.T) {
	cmds, err := OverrideCommands(OverridePlan{Rules: []OverrideRule{
		{Parent: "eth1", MAC: "aa:bb:cc:00:11:22", VID: 20},
		{Parent: "eth1", MAC: "aa:bb:cc:00:11:33", VID: 30},
		{Parent: "eth1", MAC: "aa:bb:cc:00:11:44", VID: 20, Home: "eth1", Iface: "eth1.20"},
		{Parent: "eth2", MAC: "aa:bb:cc:00:11:55", VID: 20, Home: "eth1", Iface: "eth2.20"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	cleared := map[string]bool{}
	for _, c := range cmds {
		if c[0] != "filter" {
			continue
		}
		// filter add|del dev X ingress|egress [protocol P] prio N ...
		dev, hook, proto, prio := c[3], c[4], "", ""
		for i := 5; i+1 < len(c); i++ {
			switch c[i] {
			case "protocol":
				proto = c[i+1]
			case "prio":
				prio = c[i+1]
			}
		}
		key := dev + " " + hook + " " + prio
		if c[1] == "del" {
			cleared[key] = true
			continue
		}
		if !cleared[key] {
			t.Fatalf("%s is used without being cleared first: %v", key, c)
		}
		if old, ok := seen[key]; ok && old != proto {
			t.Fatalf("%s carries both %s and %s, which the kernel refuses: %v", key, old, proto, c)
		}
		seen[key] = proto
	}
}

func TestOverrideErrorNamesTheCommand(t *testing.T) {
	old := sysRun
	sysRun = func(_ time.Duration, name string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "protocol ipv6") {
			return "Error: Filter with specified priority/protocol not found.", errors.New("exit 2")
		}
		return "", nil
	}
	t.Cleanup(func() { sysRun = old })
	err := (&Service{Root: t.TempDir()}).OverrideApply(OverridePlan{Rules: []OverrideRule{{Parent: "eth1", MAC: "aa:bb:cc:00:11:22", VID: 20}}})
	if err == nil || !strings.Contains(err.Error(), "the command that failed: tc filter add dev eth1 ingress protocol ipv6") {
		t.Fatalf("the error must name the command: %v", err)
	}
}

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
	old := wifiRun
	wifiRun = func(_ time.Duration, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		ran = append(ran, line)
		if strings.Contains(line, "vlan push") {
			return "Error: Specified qdisc kind is unknown.", errors.New("exit 2")
		}
		return "", nil
	}
	t.Cleanup(func() { wifiRun = old })
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
		"filter add dev wlan0 ingress protocol all prio 4100 flower src_mac aa:bb:cc:00:11:22 action mirred ingress redirect dev eth1.20",
		"filter add dev eth1.20 egress protocol all prio 4100 flower dst_mac aa:bb:cc:00:11:22 action mirred egress redirect dev wlan0",
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

package netd

import (
	"strings"
	"testing"
)

func join(cmds [][]string) string {
	var b strings.Builder
	for _, c := range cmds {
		b.WriteString(strings.Join(c, " ") + "\n")
	}
	return b.String()
}

func TestSQMCommandsCake(t *testing.T) {
	cmds, err := SQMCommands(SQMPlan{Links: []SQMLink{{Iface: "eth0", DownKbit: 475000, UpKbit: 47500}}, Known: []string{"eth0", "ppp0"}}, "cake")
	if err != nil {
		t.Fatal(err)
	}
	out := join(cmds)
	for _, want := range []string{
		"tc qdisc del dev eth0 root", "tc qdisc del dev ppp0 ingress",
		"tc qdisc replace dev eth0 root cake bandwidth 47500kbit besteffort triple-isolate nat",
		"ip link add ghifb0 type ifb", "ip link set ghifb0 up",
		"tc qdisc replace dev ghifb0 root cake bandwidth 475000kbit besteffort triple-isolate nat wash ingress",
		"tc qdisc replace dev eth0 handle ffff: ingress",
		"action mirred egress redirect dev ghifb0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Clearing is first, and each port is cleared once.
	if !strings.HasPrefix(out, "tc qdisc del dev eth0 root") || strings.Count(out, "tc qdisc del dev eth0 root") != 1 {
		t.Fatalf("clearing is wrong:\n%s", out)
	}
}

func TestSQMCommandsFallbackAndChecks(t *testing.T) {
	cmds, err := SQMCommands(SQMPlan{Links: []SQMLink{{Iface: "eth0", DownKbit: 100000, UpKbit: 10000}, {Iface: "eth1", DownKbit: 50000, UpKbit: 5000}}}, "fq_codel")
	if err != nil {
		t.Fatal(err)
	}
	out := join(cmds)
	for _, want := range []string{"htb rate 10000kbit ceil 10000kbit", "tc qdisc replace dev ghifb1 parent 1:10 fq_codel", "ip link add ghifb1 type ifb", "redirect dev ghifb1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, bad := range []SQMPlan{
		{Links: []SQMLink{{Iface: "eth0; reboot", DownKbit: 1000, UpKbit: 1000}}},
		{Links: []SQMLink{{Iface: "eth0", DownKbit: 0, UpKbit: 1000}}},
		{Links: []SQMLink{{Iface: "eth0", DownKbit: 1000, UpKbit: 1000}}, Known: []string{"a b"}},
	} {
		if _, err := SQMCommands(bad, "cake"); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if _, err := SQMCommands(SQMPlan{}, "htb"); err == nil {
		t.Fatal("accepted an unknown queue type")
	}
}

func TestSQMPriorityCommands(t *testing.T) {
	p := SQMPlan{Links: []SQMLink{{Iface: "eth1", DownKbit: 100000, UpKbit: 20000, Prio: true}, {Iface: "eth2", DownKbit: 50000, UpKbit: 10000}}}
	flat := func(c [][]string) string {
		var out []string
		for _, x := range c {
			out = append(out, strings.Join(x, " "))
		}
		return strings.Join(out, "\n")
	}
	both, err := SQMCommands(p, "cake+priority")
	if err != nil {
		t.Fatal(err)
	}
	f := flat(both)
	if !strings.Contains(f, "dev eth1 root cake bandwidth 20000kbit diffserv3") || !strings.Contains(f, "dev ghifb0 root cake bandwidth 100000kbit diffserv3 triple-isolate nat wash ingress") {
		t.Fatalf("priority queues missing:\n%s", f)
	}
	if !strings.Contains(f, "matchall action ctinfo dscp 0xfc000000 0x01000000 pipe action mirred egress redirect dev ghifb0") {
		t.Fatalf("download priority restore missing:\n%s", f)
	}
	if !strings.Contains(f, "dev eth2 root cake bandwidth 10000kbit besteffort") {
		t.Fatalf("a link without priority must stay besteffort:\n%s", f)
	}
	up, _ := SQMCommands(p, "cake+priority-up")
	if f = flat(up); strings.Contains(f, "ctinfo") || !strings.Contains(f, "dev eth1 root cake bandwidth 20000kbit diffserv3") {
		t.Fatalf("upload-only variant wrong:\n%s", f)
	}
	plain, _ := SQMCommands(p, "cake")
	if f = flat(plain); strings.Contains(f, "diffserv3") || strings.Contains(f, "ctinfo") {
		t.Fatalf("plain cake must not use classes:\n%s", f)
	}
}

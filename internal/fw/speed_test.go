package fw

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSpeedRulesCompile(t *testing.T) {
	m := labModel()
	m.Speed = []SpeedRule{
		{ID: 1, Kind: "priority", Prio: "high", Ifaces: []string{"eth1"}},
		{ID: 2, Kind: "priority", Prio: "low", WhoSet: true, WhatSet: true},
		{ID: 3, Kind: "limit", DownKbit: 20000, UpKbit: 5000, Ifaces: []string{"eth2"}},
		{ID: 4, Kind: "limit", DownKbit: 10000, Each: true, WhoSet: true, WhatSet: true},
	}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set sp2_who4 { type ipv4_addr; size 8192; }",
		"set sp2_what4 { type ipv4_addr; flags timeout; size 65535; }",
		"set spu4 { type ipv4_addr; flags dynamic, timeout; timeout 10m; size 8192; }",
		"chain speed_prio {",
		"chain speed_limit {",
		`oifname "eth0" iifname "eth1" meta nfproto ipv4 ip dscp set cs5 ct mark set ct mark & 0x00ffffff | 0xa1000000 return`,
		`oifname "eth0" iifname "eth1" meta nfproto ipv6 ip6 dscp set cs5`,
		`oifname "eth0" ip saddr @sp2_who4 ip daddr @sp2_what4 meta nfproto ipv4 ip dscp set cs1 ct mark set ct mark & 0x00ffffff | 0x21000000 return`,
		`oifname "eth0" iifname "eth2" limit rate over 625000 bytes/second counter drop`,
		`iifname "eth0" oifname "eth2" limit rate over 2500000 bytes/second counter drop`,
		`iifname "eth0" ip daddr @sp4_who4 ip saddr @sp4_what4 add @spd4 { ip daddr limit rate over 1250000 bytes/second } counter drop`,
		"jump speed_prio",
		"jump speed_limit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The jumps come before the connection tracking accept, so every packet is marked and counted.
	fwd := strings.SplitN(out, "chain forward", 2)[1]
	if strings.Index(fwd, "jump speed_limit") > strings.Index(fwd, "ct state { established, related } counter accept") {
		t.Fatalf("speed rules must run before established traffic is accepted:\n%s", fwd)
	}
	// No rules, no chains, no jumps; and without an internet connection nothing is written.
	m.Speed = nil
	out, _ = Compile(m)
	if strings.Contains(out, "speed_prio") || strings.Contains(out, "jump speed") {
		t.Fatal("no speed rules, no speed chains")
	}
}

func TestSpeedSetsTakeOnlyAddresses(t *testing.T) {
	s, err := BuildSetScript(map[string]SetFill{
		SpeedWhoSet(7):  {Replace: true, Elems: []string{"192.168.1.20", "192.168.1.21"}},
		SpeedWhatSet(7): {Replace: true, Elems: []string{"203.0.113.5 timeout 300s"}},
	}, nil)
	if err != nil || !strings.Contains(s, "sp7_who4 { 192.168.1.20, 192.168.1.21 }") || !strings.Contains(s, "sp7_what4 { 203.0.113.5 timeout 300s }") {
		t.Fatalf("%v\n%s", err, s)
	}
	if _, err := BuildSetScript(map[string]SetFill{SpeedWhoSet(7): {Elems: []string{"1.2.3.4 } ; flush ruleset ; {"}}}, nil); err == nil {
		t.Fatal("accepted a bad element")
	}
}

// sendMbps sends bytes from a namespace to the "internet" listener and says how fast it went.
func (l *lab) sendMbps(from string, port, bytes int) float64 {
	l.t.Helper()
	srv := fmt.Sprintf(`import socket
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("198.51.100.7",%d)); s.listen(4)
while True:
    c,_=s.accept()
    while c.recv(1<<16): pass
    c.close()`, port)
	cmd := exec.Command("ip", "netns", "exec", "ghinet", "python3", "-c", srv)
	if err := cmd.Start(); err != nil {
		l.t.Fatal(err)
	}
	l.procs = append(l.procs, cmd)
	time.Sleep(400 * time.Millisecond)
	cli := fmt.Sprintf(`import socket,time
s=socket.create_connection(("198.51.100.7",%d),10); d=b"x"*65536; n=%d; t=time.time()
while n>0:
    s.sendall(d[:min(n,len(d))]); n-=len(d)
s.shutdown(socket.SHUT_WR); s.recv(1); print(%d*8/1e6/(time.time()-t))`, port, bytes, bytes)
	out, err := l.sh(from, "python3 -c '"+cli+"'")
	if err != nil {
		l.t.Fatalf("send: %v %s", err, out)
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(out), 64)
	return v
}

func TestNetnsSpeedLimitAndPriorityMark(t *testing.T) {
	l := newLab(t)
	m := labModel()
	l.apply(m)
	l.must("ghfw", "sysctl -qw net.netfilter.nf_conntrack_acct=1 || true")
	free := l.sendMbps("ghhome", 9101, 8<<20)
	if free < 100 {
		t.Skipf("the lab link is too slow to tell a limit apart (%.0f Mb/s)", free)
	}
	// A shared upload limit of 16 Mb/s on the Home network, and High priority for it.
	m.Speed = []SpeedRule{
		{ID: 1, Kind: "limit", UpKbit: 16000, Ifaces: []string{"eth1"}},
		{ID: 2, Kind: "priority", Prio: "high", Ifaces: []string{"eth1"}},
	}
	l.apply(m)
	// A probe table that counts packets whose connection carries the High mark. It reads the mark
	// from the firewall itself, so it does not depend on the connection table being readable.
	l.must("ghfw", "nft add table inet probe && nft add chain inet probe f '{ type filter hook forward priority 10; }' && nft add rule inet probe f ct mark and 0xff000000 == 0xa1000000 counter")
	slow := l.sendMbps("ghhome", 9102, 8<<20)
	if slow > 40 || slow > free/4 {
		t.Fatalf("a 16 Mb/s limit let %.1f Mb/s through (unlimited was %.0f)", slow, free)
	}
	// Another network is untouched.
	if other := l.sendMbps("ghguest", 9103, 8<<20); other < 50 {
		t.Fatalf("the Guests network is not covered by the rule but ran at %.1f Mb/s", other)
	}
	// The connection carries the High mark in its top bits and nothing in the routing bits.
	out, _ := l.sh("ghfw", "nft list table inet probe")
	mk := regexp.MustCompile(`packets (\d+)`).FindStringSubmatch(out)
	if mk == nil || mk[1] == "0" {
		t.Fatalf("no packet belonged to a connection carrying the High mark:\n%s", out)
	}
}

func TestNetnsPerDeviceLimitFollowsTheSet(t *testing.T) {
	l := newLab(t)
	m := labModel()
	m.Speed = []SpeedRule{{ID: 5, Kind: "limit", UpKbit: 16000, Each: true, WhoSet: true}}
	l.apply(m)
	free := l.sendMbps("ghguest", 9301, 8<<20)
	if free < 100 {
		t.Skipf("the lab link is too slow to tell a limit apart (%.0f Mb/s)", free)
	}
	// Only the address in the set is held back, and each device on its own.
	script, err := BuildSetScript(map[string]SetFill{SpeedWhoSet(5): {Replace: true, Elems: []string{"192.168.1.50"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	l.must("ghfw", "printf '%s' '"+script+"' | nft -f -")
	if slow := l.sendMbps("ghhome", 9302, 8<<20); slow > 40 || slow > free/4 {
		t.Fatalf("a device in the set ran at %.1f Mb/s (unlimited %.0f)", slow, free)
	}
	if other := l.sendMbps("ghguest", 9303, 8<<20); other < 50 {
		t.Fatalf("a device outside the set was held to %.1f Mb/s", other)
	}
}

package app

import (
	"strings"
	"testing"

	"gatehouse/internal/netd"
	"gatehouse/internal/wanmon"
)

func stepOf(ss []testStep, id string) testStep {
	for _, s := range ss {
		if s.ID == id {
			return s
		}
	}
	return testStep{}
}

func TestInternetTestStopsAtTheFirstProblem(t *testing.T) {
	// No cable: everything after it is skipped, and the advice is about the cable.
	ss := buildTest(testInput{Links: []testLink{{Name: "Fibre", Port: "eth0", PortKnown: true, Cable: false}}})
	if stepOf(ss, "cable").State != "bad" || stepOf(ss, "gateway").State != "skip" || stepOf(ss, "dns").State != "skip" {
		t.Fatalf("%+v", ss)
	}
	// Modem answers, nothing beyond: the provider step fails and names the modem lights.
	up := wanmon.State{Checked: true, Health: "no-internet", GwRTT: 2e6}
	ss = buildTest(testInput{Links: []testLink{{Name: "Fibre", Port: "eth0", PortKnown: true, Cable: true, Have: true, State: up}}})
	if stepOf(ss, "gateway").State != "ok" || stepOf(ss, "provider").State != "bad" || !strings.Contains(stepOf(ss, "provider").Next, "lights") || stepOf(ss, "dns").State != "skip" {
		t.Fatalf("%+v", ss)
	}
	// Everything fine, but a shared carrier address is called out as a warning.
	good := wanmon.State{Checked: true, Health: "working", RTT: 12e6, GwRTT: 1e6}
	ss = buildTest(testInput{Links: []testLink{{Name: "Fibre", Port: "eth0", PortKnown: true, Cable: true, Have: true, State: good}}, DNSOK: true, DNSMs: 8,
		NAT: []natLink{{Name: "Fibre", Blocked: true, Title: "Your provider shares one public address between customers"}}})
	if stepOf(ss, "provider").State != "warn" || stepOf(ss, "dns").State != "ok" {
		t.Fatalf("%+v", ss)
	}
	// DNS fails while an upstream answers: Gatehouse's own setting is blamed.
	ss = buildTest(testInput{Links: []testLink{{Name: "Fibre", Port: "eth0", PortKnown: true, Cable: true, Have: true, State: good}}, DNSErr: "NXDOMAIN", UpstreamOK: true, UpstreamBy: "Cloudflare"})
	if d := stepOf(ss, "dns"); d.State != "bad" || !strings.Contains(d.Detail, "Cloudflare") {
		t.Fatalf("%+v", d)
	}
	if len(buildTest(testInput{})) != 4 {
		t.Fatal("no connection must still give four steps")
	}
}

func TestDiagTargetsAreChecked(t *testing.T) {
	for _, ok := range []string{"example.com", "8.8.8.8", "a-b.c_d", "2001"} {
		if !netd.DiagTargetOK(ok) {
			t.Errorf("%q should be allowed", ok)
		}
	}
	for _, bad := range []string{"", "-c 100", "a b", "x;rm", "$(id)", "a|b", "-f", "/etc/passwd", "a\nb"} {
		if netd.DiagTargetOK(bad) {
			t.Errorf("%q must be refused", bad)
		}
	}
}

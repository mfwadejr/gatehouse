package app

import (
	"strings"
	"testing"
	"time"

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

func okLink(name string, active, backup bool) testLink {
	return testLink{Name: name, Port: "eth-" + name, PortKnown: true, Cable: true, Have: true, Dev: "d-" + name, Backup: backup,
		State:    wanmon.State{Checked: true, Health: "working", Active: active, RTT: 12e6, GwRTT: 2e6},
		ViaTried: true, ViaOK: true, ViaMs: 20}
}

func TestMultiTestOneBlockPerConnectionAndAnIdleBackupIsNotAFault(t *testing.T) {
	v := buildMultiTest(multiInput{Mode: "failover", Links: []testLink{okLink("Fibre", true, false), okLink("Cellular", false, true)}})
	if v.State != "ok" || !v.OK || len(v.Conns) != 2 {
		t.Fatalf("%+v", v)
	}
	if !strings.Contains(v.Summary, "Fibre is carrying your traffic") || !strings.Contains(v.Summary, "Cellular waits as the backup") {
		t.Errorf("summary: %q", v.Summary)
	}
	c := v.Conns[1]
	if !c.Idle || c.InUse || c.Role != "backup" || !strings.Contains(c.Headline, "Waiting as the backup") {
		t.Errorf("backup: %+v", c)
	}
	if stepOf(c.Rows, "cable").State != "ok" || stepOf(c.Rows, "dns").State != "ok" || stepOf(c.Rows, "speed").ID != "speed" {
		t.Errorf("rows: %+v", c.Rows)
	}
	if !strings.Contains(stepOf(c.Rows, "dns").Detail, "through this connection") {
		t.Errorf("names row does not say it went through this connection: %q", stepOf(c.Rows, "dns").Detail)
	}
}

func TestMultiTestFailoverAndAmberVersusRed(t *testing.T) {
	// The main one is down and the backup carries the traffic: amber, and it says so.
	down := okLink("Fibre", false, false)
	down.State = wanmon.State{Checked: true, Health: "no-internet", GwRTT: 2e6}
	down.ViaTried = false
	v := buildMultiTest(multiInput{Mode: "failover", Links: []testLink{down, okLink("Cellular", true, true)}})
	if v.State != "warn" || !v.OK || !strings.Contains(v.Summary, "Cellular is carrying your traffic because Fibre is not working") {
		t.Fatalf("%+v", v)
	}
	if !strings.Contains(v.Note, "still works through Cellular") {
		t.Errorf("note: %q", v.Note)
	}
	// The advice is only on the connection that has the problem.
	if v.Conns[0].Next == "" || v.Conns[1].Next != "" {
		t.Errorf("advice on the wrong connection: %q / %q", v.Conns[0].Next, v.Conns[1].Next)
	}
	// A backup with a problem is amber, never red, while the main one works.
	bad := okLink("Cellular", false, true)
	bad.State = wanmon.State{Checked: true, Health: "no-internet", GwRTT: 2e6}
	v = buildMultiTest(multiInput{Mode: "failover", Links: []testLink{okLink("Fibre", true, false), bad}})
	if v.State != "warn" || v.Conns[1].State != "warn" || v.Conns[1].Idle {
		t.Errorf("backup fault: %+v", v)
	}
	// Both down: red.
	d2 := okLink("Cellular", false, true)
	d2.State = wanmon.State{Checked: true, Health: "no-internet", GwRTT: 2e6}
	d2.ViaTried = false
	v = buildMultiTest(multiInput{Mode: "failover", Links: []testLink{down, d2}})
	if v.State != "bad" || v.OK || !strings.Contains(v.Summary, "No connection is carrying traffic") {
		t.Errorf("all down: %+v", v)
	}
}

func TestMultiTestBalanceNamesTheSharing(t *testing.T) {
	v := buildMultiTest(multiInput{Mode: "balance", Links: []testLink{okLink("Fibre", true, false), okLink("Cellular", true, false)}})
	if !strings.Contains(v.Summary, "Load balancing: traffic is shared between Fibre and Cellular") || v.Conns[0].Role != "shared" {
		t.Errorf("%+v", v)
	}
}

func TestMultiTestSpeedRowUsesTheLastTest(t *testing.T) {
	l := okLink("Fibre", true, false)
	l.SpeedKnown, l.SpeedDown, l.SpeedUp, l.SpeedAt = true, 230.4e6, 19.6e6, time.Now().Unix()-7200
	v := buildMultiTest(multiInput{Mode: "failover", Links: []testLink{l, okLink("Cellular", false, true)}})
	d := stepOf(v.Conns[0].Rows, "speed").Detail
	if !strings.Contains(d, "230 Mbit/s down") || !strings.Contains(d, "19.6 up") || !strings.Contains(d, "2 hours ago") {
		t.Errorf("%q", d)
	}
}

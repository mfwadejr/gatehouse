package privsep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gatehouse/internal/fw"
	"gatehouse/internal/netd"
	"gatehouse/internal/speedtest"
	"gatehouse/internal/threatcfg"
	"gatehouse/internal/wanmon"
)

type fake struct {
	applied string
	window  time.Duration
	pending bool
}

func (f *fake) Check(s string) error {
	if s == "bad" {
		return errors.New("nft: syntax")
	}
	return nil
}
func (f *fake) Apply(s string, w time.Duration, a string) (string, time.Time, error) {
	if f.pending {
		return "", time.Time{}, fw.ErrPending
	}
	f.applied, f.window, f.pending = s, w, true
	return "tok", time.Now().Add(w), nil
}
func (f *fake) Confirm(t string) error {
	if t != "tok" {
		return errors.New("mismatch")
	}
	f.pending = false
	return nil
}
func (f *fake) LoadSets(map[string]fw.SetFill) error { return nil }
func (f *fake) Counters() (map[string]fw.Counter, error) {
	return map[string]fw.Counter{"rule:a:b:c": {Packets: 3, Bytes: 40}}, nil
}
func (f *fake) Rollback() error { f.pending = false; return nil }
func (f *fake) Pending() (string, time.Time, bool) {
	return "tok", time.Now().Add(time.Minute), f.pending
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netd.sock")
	l, err := Listen(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	f := &fake{}
	go (&Server{FW: f, AllowUID: []uint32{uint32(os.Getuid())}}).Serve(l)
	c := &Client{Path: path}

	if err := c.Check("bad"); err == nil || err.Error() != "nft: syntax" {
		t.Fatalf("check: %v", err)
	}
	if _, _, err := c.Apply("x", time.Second, "a"); err == nil {
		t.Fatal("a one second window must be refused")
	}
	tok, _, err := c.Apply("x", 30*time.Second, "alice")
	if err != nil || tok != "tok" || f.applied != "x" {
		t.Fatalf("apply: %q %v", tok, err)
	}
	if _, _, err := c.Apply("y", 30*time.Second, "alice"); err != fw.ErrPending {
		t.Fatalf("second apply: %v", err)
	}
	if _, _, ok := c.Pending(); !ok {
		t.Fatal("pending not reported")
	}
	if err := c.Confirm("nope"); err == nil {
		t.Fatal("bad token accepted")
	}
	if err := c.Confirm("tok"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Pending(); ok {
		t.Fatal("still pending after confirm")
	}
	// Set contents and rule counters travel through the helper too.
	if err := c.LoadSets(map[string]fw.SetFill{"dv_aaaaaaaa_4": {Replace: true, Elems: []string{"192.168.1.5"}}}); err != nil {
		t.Fatal(err)
	}
	cs, err := c.Counters()
	if err != nil || cs["rule:a:b:c"].Packets != 3 {
		t.Fatalf("counters: %v %v", cs, err)
	}
}

func TestMissingHelper(t *testing.T) {
	c := &Client{Path: filepath.Join(t.TempDir(), "none.sock")}
	if err := c.Check("x"); err == nil {
		t.Fatal("expected an error")
	}
}

type fakeNetCtl struct {
	updatePath    string
	systemStarted bool
	power         string
	speed         speedtest.Params
	threat        threatcfg.Plan
	restarted     string
	termSize      [2]int
	termIn        string
}

func (f *fakeNetCtl) Apply(netd.Plan, time.Duration) (string, time.Time, error) {
	return "", time.Time{}, nil
}
func (f *fakeNetCtl) Confirm(string) error               { return nil }
func (f *fakeNetCtl) Rollback() error                    { return nil }
func (f *fakeNetCtl) Pending() (string, time.Time, bool) { return "", time.Time{}, false }
func (f *fakeNetCtl) Status() map[string]wanmon.State    { return nil }
func (f *fakeNetCtl) ResetNetwork() error                { return nil }
func (f *fakeNetCtl) ApplyVPN(netd.VPNPlan) error        { return nil }
func (f *fakeNetCtl) VPNStatus() netd.VPNStatus          { return netd.VPNStatus{} }
func (f *fakeNetCtl) VPNSizeTest(name string) (netd.SizeReport, error) {
	return netd.SizeReport{Name: name, After: 1360}, nil
}
func (f *fakeNetCtl) FWEvents(uint64, int) ([]netd.FWEvent, uint64) { return nil, 7 }
func (f *fakeNetCtl) SetSystem(string, string) error                { return nil }
func (f *fakeNetCtl) UPnPApply([]netd.UPnPMapping) error            { return nil }
func (f *fakeNetCtl) UpdateStart(p string) error                    { f.updatePath = p; return nil }
func (f *fakeNetCtl) SystemCheck() (netd.SystemUpdates, error) {
	return netd.SystemUpdates{Count: 3, Names: []string{"libc6"}, RebootLikely: true}, nil
}
func (f *fakeNetCtl) SystemUpdateStart() error { f.systemStarted = true; return nil }

// Every call the client can make must reach the helper. A call missing from the helper's routing
// fails on a real machine with "unknown method" while every other test passes.
func TestEveryClientCallIsRouted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netd.sock")
	l, err := Listen(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	nc := &fakeNetCtl{}
	go (&Server{FW: &fake{}, Net: nc, MinWindow: time.Second, MaxWindow: time.Hour, AllowUID: []uint32{uint32(os.Getuid())}}).Serve(l)
	c := &Client{Path: path}
	if err := c.UpdateStart("/x/pkg.tar.gz"); err != nil || nc.updatePath != "/x/pkg.tar.gz" {
		t.Fatalf("update.start: %v %q", err, nc.updatePath)
	}
	if su, err := c.SystemCheck(); err != nil || su.Count != 3 || !su.RebootLikely {
		t.Fatalf("system.check: %v %+v", err, su)
	}
	if err := c.SystemUpdateStart(); err != nil || !nc.systemStarted {
		t.Fatalf("system.start: %v", err)
	}
	if err := c.Power("reboot"); err != nil || nc.power != "reboot" {
		t.Fatalf("system.power: %v %q", err, nc.power)
	}
	if _, last, err := c.FWEvents(0, 10); err != nil || last != 7 {
		t.Fatalf("fwlog.read: %v %d", err, last)
	}
	if err := c.UPnPApply(nil); err != nil {
		t.Fatalf("net.upnp: %v", err)
	}
	if err := c.SetSystem("h", "UTC"); err != nil {
		t.Fatalf("net.system: %v", err)
	}
	if rep, err := c.VPNSizeTest("tun2"); err != nil || rep.Name != "tun2" || rep.After != 1360 {
		t.Fatalf("vpn.sizetest: %+v %v", rep, err)
	}
	r, err := c.SpeedTest(speedtest.Params{Dev: "eth0", Quick: true})
	if err != nil || r.Server != "t" || nc.speed.Dev != "eth0" || !nc.speed.Quick {
		t.Fatalf("speed.run: %+v %v %+v", r, err, nc.speed)
	}
	if ti := c.ThreatStatus(); !ti.Installed || ti.State != "idle" {
		t.Fatalf("threat.status: %+v", ti)
	}
	if p, err := c.ThreatInstallPreview(); err != nil || len(p.Packages) != 1 {
		t.Fatalf("threat.preview: %+v %v", p, err)
	}
	if err := c.ThreatInstallStart(); err != nil {
		t.Fatalf("threat.install: %v", err)
	}
	if err := c.ThreatApply(threatcfg.Plan{Enabled: true, Ifaces: []string{"lan0"}, Preset: "light"}); err != nil || nc.threat.Preset != "light" || nc.threat.Ifaces[0] != "lan0" {
		t.Fatalf("threat.apply: %v %+v", err, nc.threat)
	}
	if err := c.ThreatUpdateRules(); err != nil {
		t.Fatalf("threat.update: %v", err)
	}
	if err := c.ThreatRollback(); err != nil {
		t.Fatalf("threat.rollback: %v", err)
	}
	if err := c.ThreatRemove(); err != nil {
		t.Fatalf("threat.remove: %v", err)
	}
	if sv := c.DiagServices(); len(sv) != 1 || sv[0].State != "running" {
		t.Fatalf("diag.services: %+v", sv)
	}
	if l, err := c.DiagLog("ssh", 50); err != nil || l != "log of ssh" {
		t.Fatalf("diag.log: %q %v", l, err)
	}
	if err := c.DiagRestart("ssh"); err != nil || nc.restarted != "ssh" {
		t.Fatalf("diag.restart: %v %q", err, nc.restarted)
	}
	if b, err := c.DiagBundle(); err != nil || b != "bundle" {
		t.Fatalf("diag.bundle: %q %v", b, err)
	}
	if id, err := c.TermOpen(30, 100); err != nil || id != "t1" || nc.termSize != [2]int{30, 100} {
		t.Fatalf("term.open: %q %v %v", id, err, nc.termSize)
	}
	if d, next, closed, err := c.TermRead("t1", 4, time.Second); err != nil || string(d) != "hi" || next != 6 || closed {
		t.Fatalf("term.read: %q %d %v %v", d, next, closed, err)
	}
	if err := c.TermWrite("t1", []byte("ls\n")); err != nil || nc.termIn != "ls\n" {
		t.Fatalf("term.write: %v %q", err, nc.termIn)
	}
	if err := c.TermResize("t1", 50, 160); err != nil || nc.termSize != [2]int{50, 160} {
		t.Fatalf("term.resize: %v %v", err, nc.termSize)
	}
	if sum, err := c.TermClose("t1"); err != nil || sum.BytesOut != 9 {
		t.Fatalf("term.close: %+v %v", sum, err)
	}
	if err := c.call("nope.nothing", nil, nil); err == nil {
		t.Fatal("a made-up method was accepted")
	}
}

func (f *fakeNetCtl) SpeedTest(ctx context.Context, p speedtest.Params) (speedtest.Result, error) {
	f.speed = p
	return speedtest.Result{DownBps: 1e6, Server: "t"}, nil
}
func (f *fakeNetCtl) Power(a string) error { f.power = a; return nil }

func (f *fakeNetCtl) Diag(kind, target string) (string, error) { return kind + " " + target, nil }
func (f *fakeNetCtl) SSHApply(p netd.SSHPlan) error            { return nil }
func (f *fakeNetCtl) HAApply(p netd.HAPlan) error              { return nil }
func (f *fakeNetCtl) NDPIApply(p netd.NDPIPlan) error          { return nil }
func (f *fakeNetCtl) NDPIInstalled() bool                      { return true }
func (f *fakeNetCtl) FlowAcct(on bool) error                   { return nil }
func (f *fakeNetCtl) ConntrackDump() (string, error)           { return "", nil }
func (f *fakeNetCtl) SQMApply(p netd.SQMPlan) (string, error)  { return "cake", nil }
func (f *fakeNetCtl) OverrideApply(p netd.OverridePlan) error  { return nil }
func (f *fakeNetCtl) ThreatStatus() netd.ThreatInfo {
	return netd.ThreatInfo{Installed: true, State: "idle"}
}
func (f *fakeNetCtl) ThreatInstallPreview() (netd.ThreatPreview, error) {
	return netd.ThreatPreview{Packages: []string{"suricata"}}, nil
}
func (f *fakeNetCtl) ThreatInstallStart() error            { return nil }
func (f *fakeNetCtl) ThreatRemove() error                  { return nil }
func (f *fakeNetCtl) ThreatApply(p threatcfg.Plan) error   { f.threat = p; return nil }
func (f *fakeNetCtl) ThreatUpdateRules() error             { return nil }
func (f *fakeNetCtl) ThreatRollback() error                { return nil }
func (f *fakeNetCtl) SSHLog(since int64) ([]string, error) { return nil, nil }
func (f *fakeNetCtl) DiagServices() []netd.DiagService {
	return []netd.DiagService{{Unit: "ssh", Title: "Remote login", State: "running"}}
}
func (f *fakeNetCtl) DiagLog(unit string, lines int) (string, error) {
	return "log of " + unit, nil
}
func (f *fakeNetCtl) DiagRestart(unit string) error { f.restarted = unit; return nil }
func (f *fakeNetCtl) DiagBundle() (string, error)   { return "bundle", nil }
func (f *fakeNetCtl) TermOpen(rows, cols int) (string, error) {
	f.termSize = [2]int{rows, cols}
	return "t1", nil
}
func (f *fakeNetCtl) TermRead(id string, since int, wait time.Duration) ([]byte, int, bool, error) {
	return []byte("hi"), since + 2, false, nil
}
func (f *fakeNetCtl) TermWrite(id string, data []byte) error { f.termIn = string(data); return nil }
func (f *fakeNetCtl) TermResize(id string, rows, cols int) error {
	f.termSize = [2]int{rows, cols}
	return nil
}
func (f *fakeNetCtl) TermClose(id string) (netd.TermSummary, error) {
	return netd.TermSummary{Seconds: 3, BytesIn: 5, BytesOut: 9}, nil
}

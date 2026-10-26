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
}

func (f *fakeNetCtl) Apply(netd.Plan, time.Duration) (string, time.Time, error) {
	return "", time.Time{}, nil
}
func (f *fakeNetCtl) Confirm(string) error                          { return nil }
func (f *fakeNetCtl) Rollback() error                               { return nil }
func (f *fakeNetCtl) Pending() (string, time.Time, bool)            { return "", time.Time{}, false }
func (f *fakeNetCtl) Status() map[string]wanmon.State               { return nil }
func (f *fakeNetCtl) ResetNetwork() error                           { return nil }
func (f *fakeNetCtl) ApplyVPN(netd.VPNPlan) error                   { return nil }
func (f *fakeNetCtl) VPNStatus() netd.VPNStatus                     { return netd.VPNStatus{} }
func (f *fakeNetCtl) FWEvents(uint64, int) ([]netd.FWEvent, uint64) { return nil, 7 }
func (f *fakeNetCtl) SetSystem(string, string) error                { return nil }
func (f *fakeNetCtl) WifiScan(string) ([]netd.WifiNetwork, error)   { return nil, nil }
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
	if _, err := c.WifiScan("wlan0"); err != nil {
		t.Fatalf("net.wifiscan: %v", err)
	}
	if err := c.UPnPApply(nil); err != nil {
		t.Fatalf("net.upnp: %v", err)
	}
	if err := c.SetSystem("h", "UTC"); err != nil {
		t.Fatalf("net.system: %v", err)
	}
	r, err := c.SpeedTest(speedtest.Params{Dev: "eth0", Quick: true})
	if err != nil || r.Server != "t" || nc.speed.Dev != "eth0" || !nc.speed.Quick {
		t.Fatalf("speed.run: %+v %v %+v", r, err, nc.speed)
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

func (f *fakeNetCtl) WifiHealth(ifaces []string, keepAwake bool) ([]netd.WifiState, error) {
	return []netd.WifiState{{Iface: "wlan0", OK: true}}, nil
}
func (f *fakeNetCtl) WifiPowerSave(iface string, keepAwake bool) error { return nil }
func (f *fakeNetCtl) WifiReport(iface string) (string, error)          { return "report " + iface, nil }
func (f *fakeNetCtl) SSHApply(p netd.SSHPlan) error                    { return nil }
func (f *fakeNetCtl) SSHLog(since int64) ([]string, error)             { return nil, nil }

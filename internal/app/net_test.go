package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gatehouse/internal/auth"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
	"gatehouse/internal/speedtest"
	"gatehouse/internal/wanmon"
)

type fakeNet struct {
	mu      sync.Mutex
	plan    netd.Plan
	pending bool

	vpn      netd.VPNPlan
	vpnCalls int
	upnp     []netd.UPnPMapping
	status   netd.VPNStatus
	events   []netd.FWEvent
	hostname string
	zone     string

	updatePath string
	sysUpdates netd.SystemUpdates
	sysErr     error
	sysStarted bool
	powerCalls []string
	resets     int
	speedCalls []speedtest.Params
	speedErr   error

	wifiStates []netd.WifiState
	wifiCalls  int
	powerSave  map[string]bool
	sshPlans   []netd.SSHPlan
	sshLines   []string
	haPlans    []netd.HAPlan
	ndpiPlans  []netd.NDPIPlan
	ndpiHave   bool
	ovPlans    []netd.OverridePlan
	ovErr      error
	flowAcct   []bool
	flowDump   string
	sqmPlans   []netd.SQMPlan
	sqmErr     error
}

func (f *fakeNet) WifiHealth(ifaces []string, keepAwake bool) ([]netd.WifiState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wifiCalls++
	return f.wifiStates, nil
}

func (f *fakeNet) WifiPowerSave(iface string, keepAwake bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.powerSave == nil {
		f.powerSave = map[string]bool{}
	}
	f.powerSave[iface] = keepAwake
	return nil
}

func (f *fakeNet) Diag(kind, target string) (string, error) { return kind + " " + target, nil }
func (f *fakeNet) WifiReport(iface string) (string, error)  { return "== Card ==\nsample " + iface, nil }

func (f *fakeNet) SSHApply(p netd.SSHPlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sshPlans = append(f.sshPlans, p)
	return nil
}

func (f *fakeNet) HAApply(p netd.HAPlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.haPlans = append(f.haPlans, p)
	return nil
}
func (f *fakeNet) NDPIApply(p netd.NDPIPlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ndpiPlans = append(f.ndpiPlans, p)
	return nil
}
func (f *fakeNet) OverrideApply(p netd.OverridePlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ovPlans = append(f.ovPlans, p)
	return f.ovErr
}
func (f *fakeNet) FlowAcct(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flowAcct = append(f.flowAcct, on)
	return nil
}
func (f *fakeNet) SQMApply(p netd.SQMPlan) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sqmPlans = append(f.sqmPlans, p)
	if f.sqmErr != nil {
		return "", f.sqmErr
	}
	if len(p.Links) == 0 {
		return "", nil
	}
	return "cake", nil
}
func (f *fakeNet) ConntrackDump() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flowDump, nil
}
func (f *fakeNet) NDPIInstalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ndpiHave
}
func (f *fakeNet) SSHLog(since int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sshLines, nil
}

func (f *fakeNet) NetApply(p netd.Plan, w time.Duration) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending {
		return "", time.Time{}, netcfg.ErrPending
	}
	f.plan, f.pending = p, true
	return "ntok", time.Now().Add(w), nil
}
func (f *fakeNet) NetConfirm(t string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.pending || t != "ntok" {
		return http.ErrNotSupported
	}
	f.pending = false
	return nil
}
func (f *fakeNet) NetReset() error    { f.mu.Lock(); f.resets++; f.mu.Unlock(); return nil }
func (f *fakeNet) NetRollback() error { f.mu.Lock(); f.pending = false; f.mu.Unlock(); return nil }
func (f *fakeNet) NetPending() (string, time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return "ntok", time.Now().Add(time.Minute), f.pending
}
func (f *fakeNet) WanStatus() (map[string]wanmon.State, error) { return map[string]wanmon.State{}, nil }
func (f *fakeNet) ApplyVPN(p netd.VPNPlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vpn, f.vpnCalls = p, f.vpnCalls+1
	return nil
}
func (f *fakeNet) VPNStatus() (netd.VPNStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *fakeNet) WifiScan(string) ([]netd.WifiNetwork, error) {
	return []netd.WifiNetwork{{SSID: "Home", Signal: -50, Security: "wpa2"}}, nil
}

func (f *fakeNet) UpdateStart(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatePath = path
	return nil
}

func (f *fakeNet) SystemCheck() (netd.SystemUpdates, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sysUpdates, f.sysErr
}

func (f *fakeNet) SystemUpdateStart() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sysStarted = true
	return nil
}

func (f *fakeNet) UPnPApply(ms []netd.UPnPMapping) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upnp = ms
	return nil
}

func (f *fakeNet) SetSystem(h, z string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hostname, f.zone = h, z
	return nil
}

func (f *fakeNet) FWEvents(since uint64, max int) ([]netd.FWEvent, uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []netd.FWEvent
	for _, e := range f.events {
		if e.Seq > since {
			out = append(out, e)
		}
	}
	return out, uint64(len(f.events)), nil
}

func TestNetworkFlow(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn := &fakeNet{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Auth.CreateUser("admin", "correct horse battery", "admin")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: "http://" + web, hc: &http.Client{Jar: jar}}
	for i := 0; i < 300; i++ {
		if resp, err := c.hc.Get(c.base + "/api/health"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, m, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"})
	c.csrf = m["csrf"].(string)
	_, m, _ = c.call("POST", "/api/auth/enrol/begin", nil)
	secret := m["secret"].(string)
	c.call("POST", "/api/auth/enrol/confirm", map[string]string{"code": auth.TOTPCodeAt(secret, time.Now())})
	step := 0
	code := func() map[string]string {
		step++
		return map[string]string{"code": auth.TOTPCodeAt(secret, time.Now().Add(time.Duration(30*step)*time.Second))}
	}

	// Bad input never reaches storage.
	if code, _, _ := c.call("PUT", "/api/net/interfaces", map[string]any{"name": "eth1; reboot", "kind": "phys", "ipv4": "192.168.1.1/24", "enabled": true}); code != 400 {
		t.Fatalf("hostile interface name: %d", code)
	}
	if code, _, _ := c.call("PUT", "/api/net/interfaces", map[string]any{"name": "eth1", "kind": "phys", "ipv4": "not-an-address", "enabled": true}); code != 400 {
		t.Fatalf("bad address: %d", code)
	}
	if code, _, body := c.call("PUT", "/api/net/interfaces", map[string]any{"name": "eth1", "kind": "phys", "ipv4": "192.168.1.1/24", "enabled": true}); code != 200 {
		t.Fatalf("lan: %d %s", code, body)
	}
	if code, _, body := c.call("PUT", "/api/net/wans", map[string]any{"name": "Fibre", "iface": "eth0", "type": "pppoe", "pppoe_user": "me@isp", "pppoe_pass": "s3cret!", "enabled": true}); code != 200 {
		t.Fatalf("pppoe: %d %s", code, body)
	}
	if code, _, body := c.call("PUT", "/api/net/wans", map[string]any{"name": "Backup", "iface": "eth2", "type": "dhcp", "priority": 2, "enabled": true}); code != 200 {
		t.Fatalf("dhcp: %d %s", code, body)
	}
	_, _, body := c.call("GET", "/api/net/wans", nil)
	if bytes.Contains(body, []byte("s3cret")) || !bytes.Contains(body, []byte(`"has_pass":true`)) {
		t.Fatalf("password leaked or missing flag: %s", body)
	}
	// Editing without re-entering the password keeps it.
	if code, _, _ := c.call("PUT", "/api/net/wans", map[string]any{"id": 1, "name": "Fibre", "iface": "eth0", "type": "pppoe", "pppoe_user": "me@isp", "enabled": true}); code != 200 {
		t.Fatalf("edit: %d", code)
	}
	_, _, body = c.call("POST", "/api/net/preview", nil)
	if !bytes.Contains(body, []byte(`"ok":true`)) || !bytes.Contains(body, []byte(`"changed":true`)) {
		t.Fatalf("preview: %s", body)
	}
	if rc, _, body := c.call("POST", "/api/net/apply", code()); rc != 200 {
		t.Fatalf("apply: %d %s", rc, body)
	}
	p := fn.plan
	if len(p.Links) != 2 || p.Links[0].Dev != "ppp-fibre" || p.Links[0].Table != 100 || p.Links[1].Table != 101 || p.Links[1].Mark != 0x101 {
		t.Fatalf("links: %+v", p.Links)
	}
	if p.Config.WANs[0].PPPoEPass != "s3cret!" {
		t.Fatal("the saved password was not passed to the helper")
	}
	if code, _, _ := c.call("POST", "/api/net/apply", code()); code != 409 {
		t.Fatalf("stacked apply: %d", code)
	}
	if code, _, body := c.call("POST", "/api/net/confirm", map[string]string{"token": "ntok"}); code != 200 {
		t.Fatalf("confirm: %d %s", code, body)
	}
	_, _, body = c.call("POST", "/api/net/preview", nil)
	if !bytes.Contains(body, []byte(`"changed":false`)) {
		t.Fatalf("still changed after confirm: %s", body)
	}
	if code, _, _ := c.call("PUT", "/api/net/mode", map[string]string{"mode": "balance"}); code != 200 {
		t.Fatal("mode")
	}
	_, _, body = c.call("POST", "/api/net/preview", nil)
	if !bytes.Contains(body, []byte(`"changed":true`)) {
		t.Fatalf("mode change not noticed: %s", body)
	}
}

func TestFirstRunSetup(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	dir := t.TempDir()
	a, err := New(Config{DataDir: dir, WebAddr: web, DNSAddrs: []string{dnsAddr}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: "http://" + web, hc: &http.Client{Jar: jar}}
	for i := 0; i < 300; i++ {
		if resp, err := c.hc.Get(c.base + "/api/health"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, m, _ := c.call("GET", "/api/setup", nil); m["needed"] != true {
		t.Fatal("setup not offered on a fresh box")
	}
	if st, _ := os.ReadFile(dir + "/setup-state"); strings.TrimSpace(string(st)) != "account" {
		t.Fatalf("the machine's screen is not told setup is unfinished: %q", st)
	}
	body := map[string]string{"username": "admin", "password": "short"}
	if rc, _, _ := c.call("POST", "/api/setup", body); rc != 400 {
		t.Fatalf("weak password accepted: %d", rc)
	}
	body["password"] = "correct horse battery"
	if rc, _, b := c.call("POST", "/api/setup", body); rc != 200 {
		t.Fatalf("setup: %d %s", rc, b)
	}
	if rc, _, _ := c.call("POST", "/api/setup", body); rc != 409 {
		t.Fatalf("second setup: %d", rc)
	}
	if st, _ := os.ReadFile(dir + "/setup-state"); strings.TrimSpace(string(st)) != "wizard" {
		t.Fatalf("after the account, the screen should say the wizard is open: %q", st)
	}
	if rc, _, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}); rc != 200 {
		t.Fatal("cannot log in as the new admin")
	}
}

func (f *fakeNet) SpeedTest(p speedtest.Params) (speedtest.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.speedCalls = append(f.speedCalls, p)
	if f.speedErr != nil {
		return speedtest.Result{}, f.speedErr
	}
	return speedtest.Result{DownBps: 412e6, UpBps: 38e6, DelayMS: 9, JitterMS: 1, LoadedMS: 25, Grade: "A", Server: "Cloudflare TST", BytesUsed: 100e6, Seconds: 20}, nil
}
func (f *fakeNet) Power(action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.powerCalls = append(f.powerCalls, action)
	return nil
}

// A change in the tunnel file Gatehouse writes must show as waiting to be applied on a machine
// that has tunnels, and must not touch one that has none.
func TestFingerprintNoticesTunnelFileVersion(t *testing.T) {
	plain := &netd.Plan{}
	if a, b := fingerprint(plain), fingerprint(&netd.Plan{}); a != b {
		t.Fatal("a plan without tunnels must be stable")
	}
	with := &netd.Plan{}
	with.Config.Tunnels = append(with.Config.Tunnels, netcfg.Tunnel{Name: "tun0", Enabled: true})
	q := *with
	q.Seconds = 0
	b, _ := json.Marshal(q)
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) == fingerprint(with) {
		t.Fatal("the tunnel file version is not part of the fingerprint")
	}
}

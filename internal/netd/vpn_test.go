package netd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseWGDump(t *testing.T) {
	dump := "wg0\tPRIV\tSERVERPUB\t51820\toff\n" +
		"wg0\tPEER1\t(none)\t203.0.113.9:40000\t10.8.0.2/32\t1700000000\t1234\t5678\toff\n" +
		"wg0\tPEER2\t(none)\t(none)\t10.8.0.3/32\t0\t0\t0\toff\n" +
		"wg1\tPRIV\tCLIENTPUB\t0\toff\n" +
		"wg1\tPROV\tPSK\t198.51.100.1:51820\t0.0.0.0/0\t1700000100\t10\t20\t25\n"
	m := ParseWGDump(dump)
	if m["wg0"].Port != 51820 || m["wg0"].PublicKey != "SERVERPUB" || len(m["wg0"].Peers) != 2 {
		t.Fatalf("%+v", m["wg0"])
	}
	p := m["wg0"].Peers[0]
	if p.Handshake != 1700000000 || p.Rx != 1234 || p.Tx != 5678 || p.Endpoint != "203.0.113.9:40000" || p.AllowedIPs[0] != "10.8.0.2/32" {
		t.Errorf("%+v", p)
	}
	if m["wg0"].Peers[1].Handshake != 0 || m["wg0"].Peers[1].Endpoint != "" {
		t.Errorf("%+v", m["wg0"].Peers[1])
	}
	if len(m["wg1"].Peers) != 1 || m["wg1"].Peers[0].AllowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("%+v", m["wg1"])
	}
}

func TestParseOVPNStatus(t *testing.T) {
	txt := "TITLE,OpenVPN 2.6\nHEADER,CLIENT_LIST,Common Name,...\nCLIENT_LIST,Marvin phone,203.0.113.5:51000,10.9.0.2,,100,200,2026-10-23 10:00:00,1790000000,UNDEF,1,2,AES-256-GCM\nEND\n"
	c := ParseOVPNStatus(txt)
	if len(c) != 1 || c[0].Name != "Marvin phone" || c[0].Virtual != "10.9.0.2" || c[0].Rx != 100 || c[0].Tx != 200 || c[0].Since != 1790000000 {
		t.Fatalf("%+v", c)
	}
}

type rec struct {
	mu    sync.Mutex
	calls []string
}

func (r *rec) sys(args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, strings.Join(args, " "))
	return nil
}
func (r *rec) has(s string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c == s {
			return true
		}
	}
	return false
}

func newTestVPN(t *testing.T) (*VPN, *rec) {
	d := t.TempDir()
	r := &rec{}
	v := NewVPN(filepath.Join(d, "state"))
	v.ServerDir, v.ClientDir, v.RunDir = filepath.Join(d, "server"), filepath.Join(d, "client"), filepath.Join(d, "run")
	v.Systemctl = r.sys
	v.WGDump = func() (string, error) { return "", nil }
	v.Interval = 10 * time.Millisecond
	v.Ping = func(dev, target string) (time.Duration, error) {
		if target == "192.0.2.1" {
			return 12 * time.Millisecond, nil
		}
		return 0, errors.New("no reply")
	}
	return v, r
}

func TestApplyOpenVPN(t *testing.T) {
	v, r := newTestVPN(t)
	defer v.Stop()
	srv := OVPNInstance{Name: "tun0", Role: "server", Conf: "dev tun0\n", Files: map[string]string{"ca.crt": "CA", "server.key": "KEY", "crl.pem": "CRL"}}
	cl := OVPNInstance{Name: "tun1", Role: "client", Conf: "dev tun1\n", Files: map[string]string{"auth": "u\np\n"}}
	if err := v.Apply(VPNPlan{OpenVPN: []OVPNInstance{srv, cl}}); err != nil {
		t.Fatal(err)
	}
	if !r.has("enable openvpn-server@gatehouse-tun0.service") || !r.has("restart openvpn-server@gatehouse-tun0.service") || !r.has("restart openvpn-client@gatehouse-tun1.service") {
		t.Errorf("units not started: %v", r.calls)
	}
	key := filepath.Join(v.ServerDir, "gatehouse-tun0", "server.key")
	if fi, err := os.Stat(key); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("server key must be private: %v %v", fi, err)
	}
	if fi, _ := os.Stat(filepath.Join(v.ServerDir, "gatehouse-tun0", "crl.pem")); fi == nil || fi.Mode().Perm() != 0o644 {
		t.Error("the revocation list must stay readable after OpenVPN drops privileges")
	}
	if fi, _ := os.Stat(filepath.Join(v.ClientDir, "gatehouse-tun1.conf")); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Error("client profile holds a key and must be private")
	}
	// Applying the same thing again restarts nothing.
	r.calls = nil
	if err := v.Apply(VPNPlan{OpenVPN: []OVPNInstance{srv, cl}}); err != nil {
		t.Fatal(err)
	}
	if r.has("restart openvpn-server@gatehouse-tun0.service") {
		t.Errorf("restarted without a change: %v", r.calls)
	}
	// Removing the server stops it and deletes its files.
	r.calls = nil
	if err := v.Apply(VPNPlan{OpenVPN: []OVPNInstance{cl}}); err != nil {
		t.Fatal(err)
	}
	if !r.has("disable --now openvpn-server@gatehouse-tun0.service") {
		t.Errorf("not stopped: %v", r.calls)
	}
	if _, err := os.Stat(key); err == nil {
		t.Error("server key left behind")
	}
	// The saved state never holds key material.
	b, _ := os.ReadFile(filepath.Join(v.StateDir, "vpn.json"))
	if strings.Contains(string(b), "KEY") {
		t.Error("state file contains a key")
	}
	// Bad input is refused.
	for _, bad := range []OVPNInstance{
		{Name: "../x", Role: "server", Conf: "x"},
		{Name: "tun2", Role: "server", Conf: "x", Files: map[string]string{"../../etc/passwd": "x"}},
		{Name: "tun2", Role: "boss", Conf: "x"},
		{Name: "tun2", Role: "server", Conf: ""},
	} {
		if err := v.Apply(VPNPlan{OpenVPN: []OVPNInstance{bad}}); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if err := v.Apply(VPNPlan{Checks: []Check{{Name: "wg1", Target: "1.1.1.1; rm -rf /"}}}); err == nil {
		t.Error("shell characters in a check target accepted")
	}
}

func TestChecks(t *testing.T) {
	v, _ := newTestVPN(t)
	defer v.Stop()
	if err := v.Apply(VPNPlan{Checks: []Check{{Name: "wg1", Target: "192.0.2.1"}, {Name: "tun1", Target: "192.0.2.99"}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := v.Status()
		if s.Checks["wg1"].At != 0 && s.Checks["tun1"].At != 0 {
			if !s.Checks["wg1"].OK || s.Checks["wg1"].RTTMs != 12 {
				t.Errorf("wg1: %+v", s.Checks["wg1"])
			}
			if s.Checks["tun1"].OK || s.Checks["tun1"].Error == "" {
				t.Errorf("tun1: %+v", s.Checks["tun1"])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("checks never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOVPNReasonSaysWhatIsWrong(t *testing.T) {
	cases := []struct {
		lines []string
		want  string
	}{
		{[]string{"TLS Error: TLS key negotiation failed to occur within 60 seconds (check your network connectivity)", "TLS Error: TLS handshake failed"}, "did not answer"},
		{[]string{"AUTH: Received control message: AUTH_FAILED"}, "refused the username and password"},
		{[]string{"VERIFY KU ERROR", "TLS_ERROR: BIO read tls_read_plaintext error"}, "certificate was rejected"},
		{[]string{"RESOLVE: Cannot resolve host address: nl.example.com:1195 (Name or service not known)"}, "look up the server's name"},
		{[]string{"Options error: Unrecognized option or missing or extra parameter(s) in x.conf:5: foo (2.6.14)"}, "would not accept a setting"},
		{[]string{"Initialization Sequence Completed"}, ""},
		// A failure that is already past is not reported once the tunnel is up.
		{[]string{"AUTH_FAILED", "Initialization Sequence Completed"}, ""},
		{[]string{"nothing useful"}, ""},
	}
	for _, c := range cases {
		got := OVPNReason(c.lines)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%v: got %q, want it to contain %q", c.lines, got, c.want)
		}
	}
}

func TestStatusReadsOpenVPNMessagesForABrokenClient(t *testing.T) {
	v, _ := newTestVPN(t)
	defer v.Stop()
	old := wifiRun
	defer func() { wifiRun = old }()
	var asked string
	wifiRun = func(_ time.Duration, name string, args ...string) (string, error) {
		asked = name + " " + strings.Join(args, " ")
		return "TLS Error: TLS handshake failed\n", nil
	}
	cl := OVPNInstance{Name: "tun1", Role: "client", Conf: "dev tun1\n", Files: map[string]string{"auth": "u\np\n"}}
	if err := v.Apply(VPNPlan{OpenVPN: []OVPNInstance{cl}}); err != nil {
		t.Fatal(err)
	}
	st := v.Status()
	o := st.OVPN["tun1"]
	if !strings.Contains(asked, "openvpn-client@gatehouse-tun1.service") {
		t.Fatalf("journal not asked for the client's unit: %q", asked)
	}
	if len(o.Log) != 1 || !strings.Contains(o.Reason, "did not answer") {
		t.Fatalf("no reason: %+v", o)
	}
}

func TestOVPNConnectedFollowsTheNewestWord(t *testing.T) {
	if !OVPNConnected([]string{"TLS handshake failed", "Initialization Sequence Completed", "Timers: ping 10"}) {
		t.Error("connected after a failure was not seen as connected")
	}
	if OVPNConnected([]string{"Initialization Sequence Completed", "[x] Inactivity timeout (--ping-restart), restarting", "SIGUSR1[soft,ping-restart] received, process restarting"}) {
		t.Error("a tunnel that is restarting was seen as connected")
	}
	if OVPNConnected([]string{"Starting openvpn-client@gatehouse-tun0.service"}) {
		t.Error("starting is not connected")
	}
}

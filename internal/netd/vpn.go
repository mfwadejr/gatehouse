package netd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OVPNInstance is one OpenVPN process: a server for devices that connect in, or a client
// that connects out to a provider.
type OVPNInstance struct {
	Name  string            `json:"name"` // the tun device, tun0 ...
	Role  string            `json:"role"` // server or client
	Conf  string            `json:"conf"`
	Files map[string]string `json:"files"` // ca.crt, server.crt, server.key, tls-crypt.key, crl.pem; auth for a client
}

// Check is a reachability test through a tunnel.
type Check struct {
	Name   string `json:"name"` // the tunnel's device
	Target string `json:"target"`
	Exit   bool   `json:"exit"` // also learn the address the internet sees for this tunnel
}

// VPNPlan is what the helper keeps running for OpenVPN and what it watches.
type VPNPlan struct {
	OpenVPN []OVPNInstance `json:"openvpn"`
	Checks  []Check        `json:"checks"`
}

type WGPeerStatus struct {
	PublicKey  string   `json:"public_key"`
	Endpoint   string   `json:"endpoint"`
	AllowedIPs []string `json:"allowed_ips"`
	Handshake  int64    `json:"handshake"` // unix seconds, 0 if never
	Rx         int64    `json:"rx"`
	Tx         int64    `json:"tx"`
}

type WGStatus struct {
	PublicKey string         `json:"public_key"`
	Port      int            `json:"port"`
	Peers     []WGPeerStatus `json:"peers"`
}

type OVPNClientStatus struct {
	Name    string `json:"name"`
	Real    string `json:"real"`
	Virtual string `json:"virtual"`
	Rx      int64  `json:"rx"`
	Tx      int64  `json:"tx"`
	Since   int64  `json:"since"`
}

type OVPNStatus struct {
	Active  bool               `json:"active"`
	Clients []OVPNClientStatus `json:"clients"`
	// For a client that is not working: what OpenVPN's own messages say, in plain words, and
	// its last few lines.
	Reason string   `json:"reason,omitempty"`
	Log    []string `json:"log,omitempty"`
}

type LinkInfo struct {
	Up   bool   `json:"up"`
	Addr string `json:"addr"` // the tunnel's own IPv4 address
}

type CheckResult struct {
	OK    bool `json:"ok"`
	RTTMs int  `json:"rtt_ms"`
	// ExitIP is the public address the tunnel presents to the internet, when asked for.
	ExitIP string `json:"exit_ip,omitempty"`
	At     int64  `json:"at"`
	Error  string `json:"error,omitempty"`
}

// VPNStatus is the live picture the console shows.
type VPNStatus struct {
	WG     map[string]WGStatus    `json:"wg"`
	OVPN   map[string]OVPNStatus  `json:"ovpn"`
	Links  map[string]LinkInfo    `json:"links"`
	Checks map[string]CheckResult `json:"checks"`
	Now    int64                  `json:"now"`
}

// VPN holds the OpenVPN and tunnel-health side of the helper.
type VPN struct {
	ServerDir string // /etc/openvpn/server
	ClientDir string // /etc/openvpn/client
	RunDir    string // /run
	StateDir  string

	Systemctl func(args ...string) error
	WGDump    func() (string, error)
	Ping      func(dev, target string) (time.Duration, error)
	ExitIP    func(dev string) (string, error)
	Interval  time.Duration

	mu       sync.Mutex // plan and process control
	rmu      sync.Mutex // check results; never held while waiting on the checks
	lmu      sync.Mutex // recent OpenVPN messages
	logCache map[string]logEntry
	plan     VPNPlan
	results  map[string]CheckResult
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

var (
	reTunName = regexp.MustCompile(`^tun[0-9]{1,2}$`)
	reTarget  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.\-]{0,252}$`)
	vpnFiles  = map[string]os.FileMode{"ca.crt": 0o644, "server.crt": 0o644, "crl.pem": 0o644, "server.key": 0o600, "tls-crypt.key": 0o600, "auth": 0o600}
)

func NewVPN(stateDir string) *VPN {
	return &VPN{
		ServerDir: "/etc/openvpn/server", ClientDir: "/etc/openvpn/client", RunDir: "/run", StateDir: stateDir,
		Interval: 20 * time.Second, results: map[string]CheckResult{},
		Systemctl: func(args ...string) error {
			if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
			}
			return nil
		},
		WGDump: func() (string, error) {
			out, err := exec.Command("wg", "show", "all", "dump").Output()
			return string(out), err
		},
		Ping: pingThrough, ExitIP: exitAddress,
	}
}

// Standard locations, for the daemon (which does not run the helper itself).
const (
	ServerBase = "/etc/openvpn/server"
	ClientBase = "/etc/openvpn/client"
)

func ServerKeyDir(name string) string   { return filepath.Join(ServerBase, "gatehouse-"+name) }
func ClientAuthPath(name string) string { return filepath.Join(ClientBase, "gatehouse-"+name+".auth") }
func unitFor(i OVPNInstance) string     { return "openvpn-" + i.Role + "@gatehouse-" + i.Name + ".service" }
func (v *VPN) confPath(i OVPNInstance) string {
	d := v.ServerDir
	if i.Role == "client" {
		d = v.ClientDir
	}
	return filepath.Join(d, "gatehouse-"+i.Name+".conf")
}

func (v *VPN) validate(p VPNPlan) error {
	seen := map[string]bool{}
	for _, i := range p.OpenVPN {
		if !reTunName.MatchString(i.Name) || (i.Role != "server" && i.Role != "client") {
			return fmt.Errorf("OpenVPN %q: invalid name or role", i.Name)
		}
		if seen[i.Name] {
			return fmt.Errorf("OpenVPN %s is listed twice", i.Name)
		}
		seen[i.Name] = true
		if len(i.Conf) == 0 || len(i.Conf) > 512*1024 {
			return fmt.Errorf("OpenVPN %s: the configuration is empty or too large", i.Name)
		}
		for n, b := range i.Files {
			if _, ok := vpnFiles[n]; !ok {
				return fmt.Errorf("OpenVPN %s: %q is not a file the helper writes", i.Name, n)
			}
			if len(b) > 64*1024 {
				return fmt.Errorf("OpenVPN %s: %s is too large", i.Name, n)
			}
		}
	}
	for _, c := range p.Checks {
		if !reTunName.MatchString(c.Name) && !regexp.MustCompile(`^wg[0-9]{1,2}$`).MatchString(c.Name) {
			return fmt.Errorf("check %q: invalid device", c.Name)
		}
		if !reTarget.MatchString(c.Target) {
			return fmt.Errorf("check %s: %q is not an address or name", c.Name, c.Target)
		}
	}
	return nil
}

func writeIfChanged(path string, body string, mode os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && string(old) == body {
		os.Chmod(path, mode)
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".gh-tmp"
	if err := os.WriteFile(tmp, []byte(body), mode); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

// Apply installs the OpenVPN configuration, starts what is needed and stops what is gone.
func (v *VPN) Apply(p VPNPlan) error {
	if err := v.validate(p); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	prev := v.loadState()
	want := map[string]OVPNInstance{}
	for _, i := range p.OpenVPN {
		want[i.Role+":"+i.Name] = i
		changed := false
		for n, b := range i.Files {
			var path string
			if i.Role == "server" {
				path = filepath.Join(v.ServerDir, "gatehouse-"+i.Name, n)
			} else {
				path = filepath.Join(v.ClientDir, "gatehouse-"+i.Name+".auth")
			}
			c, err := writeIfChanged(path, b, vpnFiles[n])
			if err != nil {
				return err
			}
			changed = changed || c
		}
		mode := os.FileMode(0o644)
		if i.Role == "client" {
			mode = 0o600
		}
		c, err := writeIfChanged(v.confPath(i), i.Conf, mode)
		if err != nil {
			return err
		}
		changed = changed || c
		u := unitFor(i)
		if changed {
			if err := v.Systemctl("enable", u); err != nil {
				return err
			}
			if err := v.Systemctl("restart", u); err != nil {
				return err
			}
		} else if err := v.Systemctl("enable", "--now", u); err != nil {
			return err
		}
	}
	for key, old := range prev.OpenVPN {
		if _, keep := want[key]; keep {
			continue
		}
		v.Systemctl("disable", "--now", unitFor(old))
		os.Remove(v.confPath(old))
		os.RemoveAll(filepath.Join(v.ServerDir, "gatehouse-"+old.Name))
		os.Remove(filepath.Join(v.ClientDir, "gatehouse-"+old.Name+".auth"))
	}
	v.plan = p
	if err := v.saveState(p); err != nil {
		return err
	}
	v.restartChecks()
	return nil
}

type vpnState struct {
	OpenVPN map[string]OVPNInstance `json:"openvpn"`
	Plan    VPNPlan                 `json:"plan"`
}

func (v *VPN) loadState() vpnState {
	st := vpnState{OpenVPN: map[string]OVPNInstance{}}
	if b, err := os.ReadFile(filepath.Join(v.StateDir, "vpn.json")); err == nil {
		json.Unmarshal(b, &st)
		if st.OpenVPN == nil {
			st.OpenVPN = map[string]OVPNInstance{}
		}
	}
	return st
}

func (v *VPN) saveState(p VPNPlan) error {
	st := vpnState{OpenVPN: map[string]OVPNInstance{}, Plan: p}
	for _, i := range p.OpenVPN {
		// Keep only what is needed to remove it later, never the keys.
		st.OpenVPN[i.Role+":"+i.Name] = OVPNInstance{Name: i.Name, Role: i.Role}
	}
	b, _ := json.Marshal(st)
	if err := os.MkdirAll(v.StateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(v.StateDir, "vpn.json"), b, 0o600)
}

// Start resumes health checks from the saved plan.
func (v *VPN) Start() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.plan = v.loadState().Plan
	v.restartChecks()
}

// Stop ends the health checks.
func (v *VPN) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cancel != nil {
		v.cancel()
		v.wg.Wait()
		v.cancel = nil
	}
}

func (v *VPN) restartChecks() {
	if v.cancel != nil {
		v.cancel()
		v.wg.Wait()
		v.cancel = nil
	}
	keep := map[string]CheckResult{}
	v.rmu.Lock()
	for _, c := range v.plan.Checks {
		if r, ok := v.results[c.Name]; ok {
			keep[c.Name] = r
		}
	}
	v.results = keep
	v.rmu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	for _, c := range v.plan.Checks {
		v.wg.Add(1)
		go v.watch(ctx, c)
	}
}

func (v *VPN) watch(ctx context.Context, c Check) {
	defer v.wg.Done()
	every := v.Interval
	if every <= 0 {
		every = 20 * time.Second
	}
	var exit string
	var exitAt time.Time
	for {
		rtt, err := v.Ping(c.Name, c.Target)
		r := CheckResult{OK: err == nil, RTTMs: int(rtt.Round(time.Millisecond) / time.Millisecond), At: time.Now().Unix()}
		if err != nil {
			r.Error = "no reply"
			exit = ""
		} else if c.Exit && v.ExitIP != nil && (exit == "" || time.Since(exitAt) > 10*time.Minute) {
			if ip, e := v.ExitIP(c.Name); e == nil {
				exit = ip
			}
			exitAt = time.Now()
		}
		r.ExitIP = exit
		v.rmu.Lock()
		v.results[c.Name] = r
		v.rmu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// exitAddress asks a plain-text "what is my address" service through the tunnel's device, so
// the answer is the address the internet sees for traffic leaving that way.
func exitAddress(dev string) (string, error) {
	d := &net.Dialer{Timeout: 6 * time.Second, Control: bindToDevice(dev)}
	cl := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: d.DialContext, DisableKeepAlives: true}}
	resp, err := cl.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(strings.TrimSpace(string(b)))
	if ip == nil || resp.StatusCode != 200 {
		return "", errors.New("unexpected answer")
	}
	return ip.String(), nil
}

var reTime = regexp.MustCompile(`time[=<]([0-9.]+) ?ms`)

func pingThrough(dev, target string) (time.Duration, error) {
	out, err := exec.Command("ping", "-n", "-c", "1", "-W", "3", "-I", dev, target).CombinedOutput()
	if err != nil {
		return 0, errors.New("no reply")
	}
	if m := reTime.FindSubmatch(out); m != nil {
		if f, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
			return time.Duration(f * float64(time.Millisecond)), nil
		}
	}
	return time.Millisecond, nil
}

// ParseWGDump reads "wg show all dump": a line per interface and per peer, tab separated.
func ParseWGDump(dump string) map[string]WGStatus {
	out := map[string]WGStatus{}
	for _, line := range strings.Split(dump, "\n") {
		f := strings.Split(line, "\t")
		switch len(f) {
		case 5: // interface: name private public port fwmark
			port, _ := strconv.Atoi(f[3])
			st := out[f[0]]
			st.PublicKey, st.Port = f[2], port
			out[f[0]] = st
		case 9: // peer: name public psk endpoint allowed handshake rx tx keepalive
			st := out[f[0]]
			hs, _ := strconv.ParseInt(f[5], 10, 64)
			rx, _ := strconv.ParseInt(f[6], 10, 64)
			tx, _ := strconv.ParseInt(f[7], 10, 64)
			ep := f[3]
			if ep == "(none)" {
				ep = ""
			}
			var allowed []string
			if f[4] != "(none)" && f[4] != "" {
				allowed = strings.Split(f[4], ",")
			}
			st.Peers = append(st.Peers, WGPeerStatus{PublicKey: f[1], Endpoint: ep, AllowedIPs: allowed, Handshake: hs, Rx: rx, Tx: tx})
			out[f[0]] = st
		}
	}
	return out
}

// ParseOVPNStatus reads a version 2 status file.
func ParseOVPNStatus(text string) []OVPNClientStatus {
	var out []OVPNClientStatus
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		f := strings.Split(sc.Text(), ",")
		// CLIENT_LIST,CN,Real,Virtual,VirtualIPv6,BytesRx,BytesTx,ConnectedSince,ConnectedSince_t,...
		if len(f) >= 9 && f[0] == "CLIENT_LIST" {
			rx, _ := strconv.ParseInt(f[5], 10, 64)
			tx, _ := strconv.ParseInt(f[6], 10, 64)
			since, _ := strconv.ParseInt(f[8], 10, 64)
			out = append(out, OVPNClientStatus{Name: f[1], Real: f[2], Virtual: f[3], Rx: rx, Tx: tx, Since: since})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func linkInfo(name string) (LinkInfo, bool) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return LinkInfo{}, false
	}
	li := LinkInfo{Up: ifi.Flags&net.FlagUp != 0}
	if as, err := ifi.Addrs(); err == nil {
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				li.Addr = n.IP.String()
				break
			}
		}
	}
	return li, true
}

// Status reports the live state of every tunnel.
func (v *VPN) Status() VPNStatus {
	v.mu.Lock()
	plan := v.plan
	v.mu.Unlock()
	res := map[string]CheckResult{}
	v.rmu.Lock()
	for k, r := range v.results {
		res[k] = r
	}
	v.rmu.Unlock()

	st := VPNStatus{WG: map[string]WGStatus{}, OVPN: map[string]OVPNStatus{}, Links: map[string]LinkInfo{}, Checks: res, Now: time.Now().Unix()}
	if v.WGDump != nil {
		if d, err := v.WGDump(); err == nil {
			st.WG = ParseWGDump(d)
		}
	}
	names := map[string]bool{}
	for n := range st.WG {
		names[n] = true
	}
	for _, i := range plan.OpenVPN {
		names[i.Name] = true
		o := OVPNStatus{Clients: []OVPNClientStatus{}}
		o.Active = v.Systemctl != nil && v.isActive(unitFor(i))
		file := filepath.Join(v.RunDir, "openvpn-server", "status-gatehouse-"+i.Name+".log")
		if i.Role == "server" {
			if b, err := os.ReadFile(file); err == nil {
				o.Clients = ParseOVPNStatus(string(b))
			}
		} else if v.Systemctl != nil {
			// A client that has no address in the tunnel, or whose test fails, gets its own
			// messages read so the console can say why.
			li, _ := linkInfo(i.Name)
			if r, ok := res[i.Name]; li.Addr == "" || (ok && !r.OK) {
				lines := v.recentOVPN(unitFor(i))
				if len(lines) > 12 {
					lines = lines[len(lines)-12:]
				}
				o.Log, o.Reason = lines, OVPNReason(lines)
			}
		}
		st.OVPN[i.Name] = o
	}
	for _, c := range plan.Checks {
		names[c.Name] = true
	}
	for n := range names {
		if li, ok := linkInfo(n); ok {
			st.Links[n] = li
		}
	}
	return st
}

func (v *VPN) isActive(unit string) bool {
	return v.Systemctl("is-active", "--quiet", unit) == nil
}

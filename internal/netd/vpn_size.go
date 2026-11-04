package netd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// The packet size of an outbound tunnel. A tunnel has to fit inside the packets of the connection
// under it, and its own wrapping takes room. When the tunnel is set too high, small packets (a
// ping) pass and big ones (a secure web page) are dropped silently. The helper therefore measures
// which size gets through, sets the tunnel to it, and measures again after every reconnect, when
// a big packet stops getting through, and now and then in case the path changed.

// SizeSpec says which tunnel to watch and where to start.
type SizeSpec struct {
	Name string `json:"name"`
	// Start is the first (largest) size tried: the profile's own size when it sets one, else the
	// tunnel's default (1500 for OpenVPN, 1420 for WireGuard). Zero means that default.
	Start       int  `json:"start,omitempty"`
	FromProfile bool `json:"from_profile,omitempty"`
	// Check is the tunnel's own check address, tried before the shared list.
	Check string `json:"check,omitempty"`
}

// TunnelInfo is what the helper knows about one outbound tunnel's size and exit address.
type TunnelInfo struct {
	MTU int `json:"mtu"`
	// Source is "measured", "profile" (the profile's own size passed), or "default" (nothing could
	// be confirmed, so the lowest allowed size is in use).
	Source string `json:"source,omitempty"`
	At     int64  `json:"at,omitempty"`
	// Passing is true when at least one small test packet came back through the tunnel.
	Passing bool   `json:"passing"`
	Note    string `json:"note,omitempty"`

	ExitIP    string `json:"exit_ip,omitempty"`
	ExitError string `json:"exit_error,omitempty"`
	ExitAt    int64  `json:"exit_at,omitempty"`
}

// SizeRow is what came back for one size in a test.
type SizeRow struct {
	Size     int      `json:"size"`
	Answered []string `json:"answered"`
	OK       bool     `json:"ok"`
}

// SizeReport is the result of one test, shown after the person asks for it.
type SizeReport struct {
	Name    string    `json:"name"`
	Before  int       `json:"before"`
	After   int       `json:"after"`
	Source  string    `json:"source"`
	Passing bool      `json:"passing"`
	Targets []string  `json:"targets"`
	Alive   []string  `json:"alive"`
	Rows    []SizeRow `json:"rows"`
	Note    string    `json:"note"`
}

// Sizes tried, largest first. The floor is 1280, the smallest packet the internet must carry.
var sizeSteps = []int{1500, 1460, 1420, 1400, 1380, 1360, 1340, 1300, 1280}

const sizeFloor = 1280

// How long to wait before measuring again when a size could not be confirmed, then every five
// minutes. A confirmed size is measured again every half hour.
var sizeRetry = []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}

const sizeRecheck = 30 * time.Minute
const exitRecheck = 10 * time.Minute

// DefaultPingAddrs and DefaultExitSources are used when the console sends none.
var DefaultPingAddrs = []string{"1.1.1.1", "8.8.8.8", "1.0.0.1", "8.8.4.4", "9.9.9.9"}
var DefaultExitSources = []string{"dns:google", "web:https://checkip.amazonaws.com", "dns:cloudflare", "web:https://1.1.1.1/cdn-cgi/trace"}

var reExitSource = regexp.MustCompile(`^(dns:(google|cloudflare)|web:https://[A-Za-z0-9][A-Za-z0-9.\-]{0,200}(:[0-9]{1,5})?(/[A-Za-z0-9._~\-/]{0,200})?)$`)

// ValidExitSource reports whether s is a lookup the helper will run.
func ValidExitSource(s string) bool { return reExitSource.MatchString(s) }

func sizeStart(s SizeSpec) int {
	if s.Start >= sizeFloor && s.Start <= 9000 {
		return s.Start
	}
	if strings.HasPrefix(s.Name, "wg") {
		return 1420
	}
	return 1500
}

// stepsFrom lists the sizes to try: the start first, then every standard step below it.
func stepsFrom(start int) []int {
	out := []int{start}
	for _, n := range sizeSteps {
		if n < start {
			out = append(out, n)
		}
	}
	if out[len(out)-1] != sizeFloor && start > sizeFloor {
		out = append(out, sizeFloor)
	}
	return out
}

// pingSize sends one packet of the given total size that may not be split, out of one device.
func pingSize(dev, target string, size int) bool {
	payload := size - 28
	if payload < 0 {
		payload = 0
	}
	cmd := exec.Command("ping", "-n", "-q", "-c", "1", "-W", "2", "-M", "do", "-s", strconv.Itoa(payload), "-I", dev, target)
	return cmd.Run() == nil
}

func setMTU(dev string, mtu int) error {
	if out, err := exec.Command("ip", "link", "set", "dev", dev, "mtu", strconv.Itoa(mtu)).CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set mtu: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func ifaceInfo(dev string) (index, mtu int, up, ok bool) {
	ifc, err := net.InterfaceByName(dev)
	if err != nil {
		return 0, 0, false, false
	}
	return ifc.Index, ifc.MTU, ifc.Flags&net.FlagUp != 0, true
}

func publicIP(s string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", false
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 { // 100.64.0.0/10, carrier-grade NAT
		return "", false
	}
	return ip.String(), true
}

// exitLookup asks one service, out of the tunnel's own device, what address it sees.
func exitLookup(dev, source string) (string, error) {
	switch {
	case source == "dns:google":
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, "ns1.google.com")
		if err != nil {
			return "", errors.New("could not find Google's server")
		}
		var server string
		for _, a := range addrs {
			if a.IP.To4() != nil {
				server = a.IP.String()
				break
			}
		}
		if server == "" {
			return "", errors.New("no address for Google's server")
		}
		return dnsTXT(dev, server+":53", "o-o.myaddr.l.google.com.", dns.ClassINET)
	case source == "dns:cloudflare":
		return dnsTXT(dev, "1.1.1.1:53", "whoami.cloudflare.", dns.ClassCHAOS)
	case strings.HasPrefix(source, "web:https://"):
		return webAddress(dev, strings.TrimPrefix(source, "web:"))
	}
	return "", errors.New("unknown lookup")
}

func dnsTXT(dev, server, name string, class uint16) (string, error) {
	m := new(dns.Msg)
	m.SetQuestion(name, dns.TypeTXT)
	m.Question[0].Qclass = class
	m.RecursionDesired = true
	cl := &dns.Client{Net: "udp", Timeout: 3 * time.Second, Dialer: &net.Dialer{Timeout: 3 * time.Second, Control: bindToDevice(dev)}}
	r, _, err := cl.Exchange(m, server)
	if err != nil {
		return "", err
	}
	for _, rr := range r.Answer {
		if t, ok := rr.(*dns.TXT); ok {
			for _, s := range t.Txt {
				if ip, ok := publicIP(s); ok {
					return ip, nil
				}
			}
		}
	}
	return "", errors.New("the answer was not a public address")
}

func webAddress(dev, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return "", errors.New("not an https address")
	}
	d := &net.Dialer{Timeout: 3 * time.Second, Control: bindToDevice(dev)}
	cl := &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{DialContext: d.DialContext, DisableKeepAlives: true}}
	resp, err := cl.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || resp.StatusCode != 200 {
		return "", errors.New("unexpected answer")
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "ip=")
		if ip, ok := publicIP(line); ok {
			return ip, nil
		}
	}
	return "", errors.New("the answer was not a public address")
}

// sizeReq asks a running tunnel watcher to test now.
type sizeReq struct{ reply chan SizeReport }

type tunState struct {
	ifIndex   int
	applied   int
	confirmed bool
	attempt   int
	nextProbe time.Time
	exit      string
	exitAt    time.Time
}

func (v *VPN) pingTargets(spec SizeSpec) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(spec.Check)
	v.mu2.Lock()
	list := v.pingAddrs
	v.mu2.Unlock()
	if len(list) == 0 {
		list = DefaultPingAddrs
	}
	for _, a := range list {
		add(a)
	}
	return out
}

func (v *VPN) exitSources() []string {
	v.mu2.Lock()
	defer v.mu2.Unlock()
	if len(v.exitSrcs) == 0 {
		return DefaultExitSources
	}
	return v.exitSrcs
}

// probe measures which size gets through and returns the report. It does not change the tunnel.
func (v *VPN) probe(dev string, spec SizeSpec, current int) SizeReport {
	rep := SizeReport{Name: spec.Name, Before: current, Targets: v.pingTargets(spec)}
	// Which addresses answer a small packet at all. An address that never answers (some networks
	// drop ping) is left out, so it cannot spoil the result.
	alive := make([]bool, len(rep.Targets))
	var wg sync.WaitGroup
	for i, t := range rep.Targets {
		wg.Add(1)
		go func(i int, t string) {
			defer wg.Done()
			alive[i] = v.PingSize(dev, t, 84)
		}(i, t)
	}
	wg.Wait()
	for i, t := range rep.Targets {
		if alive[i] {
			rep.Alive = append(rep.Alive, t)
		}
	}
	if len(rep.Alive) == 0 {
		rep.Note = "Nothing answered through the tunnel, so no size was measured."
		return rep
	}
	rep.Passing = true
	need := 2
	if len(rep.Alive) < 2 {
		need = 1
	}
	steps := stepsFrom(sizeStart(spec))
	rep.Rows = make([]SizeRow, len(steps))
	for i, size := range steps {
		wg.Add(1)
		go func(i, size int) {
			defer wg.Done()
			row := SizeRow{Size: size, Answered: []string{}}
			for _, t := range rep.Alive {
				if v.PingSize(dev, t, size) {
					row.Answered = append(row.Answered, t)
					if len(row.Answered) >= need {
						break
					}
				}
			}
			row.OK = len(row.Answered) >= need
			rep.Rows[i] = row
		}(i, size)
	}
	wg.Wait()
	best := 0
	for _, r := range rep.Rows {
		if r.OK && r.Size > best {
			best = r.Size
		}
	}
	switch {
	case best == 0:
		rep.After, rep.Source = sizeFloor, "default"
		rep.Note = fmt.Sprintf("Small packets pass but no tested size could be confirmed, so %d is in use. It is tested again shortly.", sizeFloor)
	case best == sizeStart(spec) && spec.FromProfile:
		rep.After, rep.Source = best, "profile"
	default:
		rep.After, rep.Source = best, "measured"
	}
	return rep
}

func (v *VPN) setInfo(name string, f func(*TunnelInfo)) {
	v.rmu.Lock()
	t := v.tunnels[name]
	f(&t)
	v.tunnels[name] = t
	v.rmu.Unlock()
}

// measure runs a probe and, when the answer differs, sets the tunnel to it.
func (v *VPN) measure(dev string, spec SizeSpec, st *tunState, mtu int) SizeReport {
	rep := v.probe(dev, spec, mtu)
	now := time.Now()
	if !rep.Passing {
		st.confirmed = false
		st.attempt++
		st.nextProbe = now.Add(sizeRetry[minInt(st.attempt-1, len(sizeRetry)-1)])
		v.setInfo(spec.Name, func(t *TunnelInfo) {
			t.Passing, t.Note, t.At = false, rep.Note, now.Unix()
		})
		return rep
	}
	if rep.After != mtu {
		if err := v.SetMTU(dev, rep.After); err != nil {
			rep.Note = "Could not set the size: " + err.Error()
		}
	}
	st.applied = rep.After
	st.confirmed = rep.Source != "default"
	if st.confirmed {
		st.attempt = 0
		st.nextProbe = now.Add(sizeRecheck)
	} else {
		st.attempt++
		st.nextProbe = now.Add(sizeRetry[minInt(st.attempt-1, len(sizeRetry)-1)])
	}
	v.setInfo(spec.Name, func(t *TunnelInfo) {
		t.MTU, t.Source, t.At, t.Passing, t.Note = rep.After, rep.Source, now.Unix(), true, rep.Note
	})
	return rep
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// lookExit tries the exit-address services in order and keeps the first plain public address. A
// failure is tried again on the next round, never held back for ten minutes.
func (v *VPN) lookExit(dev, name string, st *tunState, force bool) {
	if st.exit != "" && !force && time.Since(st.exitAt) < exitRecheck {
		return
	}
	var errs []string
	for _, src := range v.exitSources() {
		ip, err := v.ExitLookup(dev, src)
		if err == nil {
			st.exit, st.exitAt = ip, time.Now()
			v.setInfo(name, func(t *TunnelInfo) { t.ExitIP, t.ExitError, t.ExitAt = ip, "", st.exitAt.Unix() })
			return
		}
		errs = append(errs, exitLabel(src)+": "+shortErr(err))
	}
	st.exit = ""
	v.setInfo(name, func(t *TunnelInfo) { t.ExitIP, t.ExitError = "", strings.Join(errs, "; ") })
}

func exitLabel(src string) string {
	switch src {
	case "dns:google":
		return "Google"
	case "dns:cloudflare":
		return "Cloudflare"
	}
	if u, err := url.Parse(strings.TrimPrefix(src, "web:")); err == nil && u.Host != "" {
		return u.Host
	}
	return src
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// watchTunnel keeps one tunnel's size and exit address right.
func (v *VPN) watchTunnel(ctx context.Context, spec SizeSpec, reqs <-chan sizeReq) {
	defer v.wg.Done()
	every := v.Interval
	if every <= 0 {
		every = 20 * time.Second
	}
	st := &tunState{}
	for {
		forceExit := false
		var manual *sizeReq
		idx, mtu, up, ok := v.IfaceInfo(spec.Name)
		switch {
		case !ok || !up:
			// Not there (yet) or down: remember that, and measure again the moment it is back.
			v.setInfo(spec.Name, func(t *TunnelInfo) { *t = TunnelInfo{Note: "The tunnel is not up."} })
			*st = tunState{}
		default:
			reconnected := st.ifIndex != 0 && idx != st.ifIndex
			resized := st.applied != 0 && mtu != st.applied
			first := st.ifIndex == 0
			st.ifIndex = idx
			due := first || reconnected || resized || !time.Now().Before(st.nextProbe)
			if reconnected || first {
				forceExit = true
			}
			if !due && st.confirmed {
				// A big packet at the size in use must still get through while small ones do.
				// If it does not, the path changed: measure again now.
				if !v.bigPasses(spec.Name, spec, st.applied) {
					due = true
				}
			}
			if due {
				v.measure(spec.Name, spec, st, mtu)
				forceExit = forceExit || reconnected
			}
			if info := v.info(spec.Name); info.Passing {
				v.lookExit(spec.Name, spec.Name, st, forceExit)
			} else {
				st.exit = ""
				v.setInfo(spec.Name, func(t *TunnelInfo) { t.ExitIP = "" })
			}
		}
		wait := every
		if !st.nextProbe.IsZero() {
			if d := time.Until(st.nextProbe); d > 0 && d < wait {
				wait = d
			} else if d <= 0 {
				wait = time.Second
			}
		}
		if wait < time.Second {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case r := <-reqs:
			manual = &r
		case <-time.After(wait):
		}
		if manual != nil {
			idx, mtu, up, ok = v.IfaceInfo(spec.Name)
			var rep SizeReport
			if !ok || !up {
				rep = SizeReport{Name: spec.Name, Note: "The tunnel is not up."}
			} else {
				st.ifIndex = idx
				rep = v.measure(spec.Name, spec, st, mtu)
				v.lookExit(spec.Name, spec.Name, st, true)
			}
			manual.reply <- rep
		}
	}
}

func (v *VPN) info(name string) TunnelInfo {
	v.rmu.Lock()
	defer v.rmu.Unlock()
	return v.tunnels[name]
}

// bigPasses sends a packet of the size in use to the addresses and says whether any came back.
func (v *VPN) bigPasses(devName string, spec SizeSpec, size int) bool {
	targets := v.pingTargets(spec)
	if len(targets) > 3 {
		targets = targets[:3]
	}
	// If a small packet does not pass either, the tunnel is down, not too big; that is the
	// health check's business and the schedule's, so say "fine" here.
	smallOK := false
	for _, t := range targets {
		if v.PingSize(devName, t, 84) {
			smallOK = true
			if v.PingSize(devName, t, size) {
				return true
			}
		}
	}
	return !smallOK
}

// SizeTest measures one tunnel now, sets its size, and reports what was found.
func (v *VPN) SizeTest(name string) (SizeReport, error) {
	v.rmu.Lock()
	ch, ok := v.reqs[name]
	v.rmu.Unlock()
	if !ok {
		return SizeReport{}, errors.New("that tunnel is not being watched (is it turned on and applied?)")
	}
	r := sizeReq{reply: make(chan SizeReport, 1)}
	select {
	case ch <- r:
	case <-time.After(5 * time.Second):
		return SizeReport{}, errors.New("a test is already running; try again in a moment")
	}
	select {
	case rep := <-r.reply:
		sort.Slice(rep.Rows, func(i, j int) bool { return rep.Rows[i].Size > rep.Rows[j].Size })
		return rep, nil
	case <-time.After(45 * time.Second):
		return SizeReport{}, errors.New("the test took too long")
	}
}

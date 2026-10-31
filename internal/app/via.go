package app

import (
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	dnssrv "gatehouse/internal/dns"
)

// Which connection a device's traffic leaves through. The firewall log already records the port
// each new connection left by; this turns that port into the name a person gave the connection
// (a VPN's name, or an internet connection's name) so the console can say "ExpressVPN NL".

type viaInfo struct {
	Label string
	Kind  string // tunnel or wan
}

type viaCache struct {
	mu sync.Mutex
	at time.Time
	m  map[string]viaInfo
}

// connNames maps a port name to the connection that uses it. Kept for half a minute.
func (a *App) connNames() map[string]viaInfo {
	c := &a.viaNames
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m != nil && time.Since(c.at) < 30*time.Second {
		return c.m
	}
	out := map[string]viaInfo{}
	for _, v := range a.vpnSnapshot().Views {
		if v.Role == "server" {
			continue
		}
		l := v.Label
		if l == "" {
			l = v.Dev
		}
		out[v.Dev] = viaInfo{l, "tunnel"}
	}
	for name, dev := range a.wanDevices() {
		if dev != "" {
			out[dev] = viaInfo{name, "wan"}
		}
	}
	c.m, c.at = out, time.Now()
	return out
}

// viaOf names the connection a logged connection left through, or "" when it did not leave
// through one (traffic to the router itself, or between local networks).
func (a *App) viaOf(outIf string) (label, kind string) {
	if outIf == "" {
		return "", ""
	}
	if v, ok := a.connNames()[outIf]; ok {
		return v.Label, v.Kind
	}
	return "", ""
}

// tunnelFor finds the VPN client connection a device address is sent through.
func (a *App) tunnelFor(ip string) (VPNView, bool) {
	p := net.ParseIP(ip)
	if p == nil {
		return VPNView{}, false
	}
	for _, v := range a.vpnSnapshot().Views {
		if v.Role == "server" {
			continue
		}
		for _, u := range v.Uses {
			for _, pref := range u.IPs {
				if prefixHas(pref, p) {
					return v, true
				}
			}
		}
	}
	return VPNView{}, false
}

// tunnelOfIP names the tunnel a device address is sent through and whether it is working.
func (a *App) tunnelOfIP(ip string) (label, dev, state string) {
	if v, ok := a.tunnelFor(ip); ok {
		return v.Label, v.Dev, v.State
	}
	return
}

// dnsPath says how a device's name lookups travel. A device sent through a VPN has its lookups
// sent through it as well. If the tunnel is down: with the kill switch on the lookups fail with
// the rest of the device's traffic; with it off they go the normal way so DNS keeps working.
func (a *App) dnsPath(client string) dnssrv.Path {
	v, ok := a.tunnelFor(client)
	if !ok || !v.Enabled {
		return dnssrv.Path{}
	}
	if v.State == "up" {
		return dnssrv.Path{Dev: v.Dev, Strict: v.KillSwitch}
	}
	if v.KillSwitch {
		return dnssrv.Path{Dev: v.Dev, Hold: true}
	}
	return dnssrv.Path{}
}

func prefixHas(pref string, ip net.IP) bool {
	if !strings.Contains(pref, "/") {
		return net.ParseIP(pref).Equal(ip)
	}
	_, n, err := net.ParseCIDR(pref)
	return err == nil && n.Contains(ip)
}

// ViaCheck is the answer to "is this device really going through its VPN?".
type ViaCheck struct {
	Device     string     `json:"device"`
	Tunnel     string     `json:"tunnel"`  // the VPN it is sent through, or ""
	State      string     `json:"state"`   // that VPN's state
	Window     int        `json:"window"`  // seconds looked at
	Through    int        `json:"through"` // new connections that left through the tunnel
	Direct     int        `json:"direct"`  // new connections that left some other way
	Others     []ViaCount `json:"others"`  // where the direct ones went
	Leaked     []ViaLeak  `json:"leaked"`  // a few of the direct ones, newest first
	Verdict    string     `json:"verdict"` // ok, leak, idle, none, address
	CheckedAt  int64      `json:"checked_at"`
	DNSThrough int        `json:"dns_through"` // name lookups that went out through the tunnel
	DNSDirect  int        `json:"dns_direct"`  // name lookups that went out the normal way
	Sentence   string     `json:"sentence"`
}

type ViaCount struct {
	Label string `json:"label"`
	N     int    `json:"n"`
}

type ViaLeak struct {
	TS   int64  `json:"ts"`
	What string `json:"what"`
	Via  string `json:"via"`
}

// viaWindow is how far back the check looks. Short on purpose: a problem that has been fixed
// should not stay red for an hour.
const viaWindow = 900

// viaCheck checks one device address.
func (a *App) viaCheck(ip, name string) ViaCheck {
	tl, td, ts := a.tunnelOfIP(ip)
	return a.viaCheckFor(name, []string{ip}, tl, td, ts)
}

// viaCheckMAC checks a device by its hardware address, using every address it has now: the one it
// is on today as well as the one the VPN routing was set up for.
func (a *App) viaCheckMAC(mac string) ViaCheck {
	mac = strings.ToLower(mac)
	name, cur := "", ""
	if a.Devices != nil {
		if list, err := a.Devices.List(); err == nil {
			for _, d := range list {
				if strings.ToLower(d.MAC) == mac {
					name, cur = deviceTitle(d), d.IP
				}
			}
		}
	}
	var tv VPNView
	var covered []string
	found := false
	for _, v := range a.vpnSnapshot().Views {
		if v.Role == "server" {
			continue
		}
		for _, u := range v.Uses {
			if u.Kind == "device" && strings.ToLower(u.Value) == mac {
				tv, found = v, true
				for _, p := range u.IPs {
					covered = append(covered, strings.SplitN(p, "/", 2)[0])
				}
			}
		}
	}
	if !found && cur != "" {
		if v, ok := a.tunnelFor(cur); ok {
			tv, found = v, true
		}
	}
	ips := append([]string{}, covered...)
	if cur != "" {
		have := false
		for _, c := range ips {
			if c == cur {
				have = true
			}
		}
		if !have {
			ips = append(ips, cur)
		}
	}
	out := a.viaCheckFor(name, ips, tv.Label, tv.Dev, tv.State)
	if found && cur != "" && len(covered) > 0 {
		in := false
		for _, c := range covered {
			if c == cur {
				in = true
			}
		}
		if !in {
			out.Verdict = "address"
			out.Sentence = "This device is now at " + cur + ", but the VPN is set up for " + strings.Join(covered, ", ") + ". Press Review and apply so the VPN follows its new address."
		}
	}
	return out
}

// viaCheckFor counts the recent new connections and name lookups of these addresses by the way
// they left.
func (a *App) viaCheckFor(name string, ips []string, tl, td, ts string) ViaCheck {
	out := ViaCheck{Device: name, Window: viaWindow, Others: []ViaCount{}, Leaked: []ViaLeak{}, CheckedAt: time.Now().Unix(), Tunnel: tl, State: ts}
	if len(ips) == 0 {
		out.Verdict, out.Sentence = "none", "This device has no address yet, so there is nothing to check."
		return out
	}
	since := time.Now().Unix() - viaWindow
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ips)), ",")
	args := []any{}
	for _, ip := range ips {
		args = append(args, ip)
	}
	args = append(args, since)
	rows, err := a.Logs.Query(`SELECT ts,to_addr,dport,proto,out_if FROM fw_events WHERE from_addr IN (`+ph+`) AND action='allow' AND out_if<>'' AND ts>=? ORDER BY ts DESC,id DESC LIMIT 5000`, args...)
	by := map[string]int{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t int64
			var to, proto, oif string
			var port int
			rows.Scan(&t, &to, &port, &proto, &oif)
			label, _ := a.viaOf(oif)
			if label == "" {
				continue
			}
			if td != "" && oif == td {
				out.Through++
				continue
			}
			out.Direct++
			by[label]++
			if len(out.Leaked) < 5 {
				w := strings.ToUpper(proto) + " to " + to
				if port > 0 {
					w += ":" + strconv.Itoa(port)
				}
				out.Leaked = append(out.Leaked, ViaLeak{t, w, label})
			}
		}
	}
	// Name lookups that really left the box (answers from memory or the block list did not).
	if drows, derr := a.Logs.Query(`SELECT out_if, COUNT(*) FROM dns_queries WHERE client IN (`+ph+`) AND result='forwarded' AND ts>=? GROUP BY out_if`, args...); derr == nil {
		defer drows.Close()
		for drows.Next() {
			var oif string
			var n int
			drows.Scan(&oif, &n)
			if td != "" && oif == td {
				out.DNSThrough += n
			} else {
				out.DNSDirect += n
			}
		}
	}
	for l, n := range by {
		out.Others = append(out.Others, ViaCount{l, n})
	}
	sort.Slice(out.Others, func(i, j int) bool { return out.Others[i].N > out.Others[j].N })
	mins := strconv.Itoa(viaWindow / 60)
	switch {
	case tl == "":
		out.Verdict = "none"
		out.Sentence = "This device is not sent through a VPN."
	case out.Through == 0 && out.Direct == 0 && out.DNSThrough == 0 && out.DNSDirect == 0:
		out.Verdict = "idle"
		out.Sentence = "No new connections from this device in the last " + mins + " minutes, so there is nothing to check yet. Open a website on it; this updates by itself."
	case out.Direct > 0:
		out.Verdict = "leak"
		out.Sentence = strconv.Itoa(out.Direct) + " connection" + plural(out.Direct) + " from this device left without using " + tl + " in the last " + mins + " minutes. See below."
		if ts != "up" {
			out.Sentence += " The VPN is not working right now."
		}
	case out.DNSDirect > 0:
		out.Verdict = "leak"
		out.Sentence = "Its connections went through " + tl + ", but " + strconv.Itoa(out.DNSDirect) + " name lookup" + plural(out.DNSDirect) + " went out the normal way in the last " + mins + " minutes."
	default:
		out.Verdict = "ok"
		out.Sentence = "All " + strconv.Itoa(out.Through) + " new connection" + plural(out.Through) + " from this device in the last " + mins + " minutes went through " + tl
		if out.DNSThrough > 0 {
			out.Sentence += ", and so did its " + strconv.Itoa(out.DNSThrough) + " name lookup" + plural(out.DNSThrough)
		}
		out.Sentence += "."
	}
	return out
}

func (a *App) registerViaRoutes() {
	a.API.Handle("GET /api/vpn/check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if mac := r.URL.Query().Get("mac"); mac != "" {
			api.JSON(w, 200, a.viaCheckMAC(mac))
			return
		}
		ip := r.URL.Query().Get("ip")
		if net.ParseIP(ip) == nil {
			api.Error(w, http.StatusBadRequest, "give the device")
			return
		}
		api.JSON(w, 200, a.viaCheck(ip, a.deviceName(ip)))
	})
}

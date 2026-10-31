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

// tunnelOfIP finds the VPN client connection a device address has been sent through, and whether
// that tunnel is working right now.
func (a *App) tunnelOfIP(ip string) (label, dev, state string) {
	p := net.ParseIP(ip)
	if p == nil {
		return
	}
	for _, v := range a.vpnSnapshot().Views {
		if v.Role == "server" {
			continue
		}
		for _, u := range v.Uses {
			for _, pref := range u.IPs {
				if prefixHas(pref, p) {
					return v.Label, v.Dev, v.State
				}
			}
		}
	}
	return
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
	Device   string     `json:"device"`
	Tunnel   string     `json:"tunnel"`  // the VPN it is sent through, or ""
	State    string     `json:"state"`   // that VPN's state
	Window   int        `json:"window"`  // seconds looked at
	Through  int        `json:"through"` // new connections that left through the tunnel
	Direct   int        `json:"direct"`  // new connections that left some other way
	Others   []ViaCount `json:"others"`  // where the direct ones went
	Leaked   []ViaLeak  `json:"leaked"`  // a few of the direct ones, newest first
	Verdict  string     `json:"verdict"` // ok, leak, idle, none
	Sentence string     `json:"sentence"`
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

const viaWindow = 3600

// viaCheck counts the device's recent new connections by the connection they left through.
func (a *App) viaCheck(ip, name string) ViaCheck {
	out := ViaCheck{Device: name, Window: viaWindow, Others: []ViaCount{}, Leaked: []ViaLeak{}}
	tl, td, ts := a.tunnelOfIP(ip)
	out.Tunnel, out.State = tl, ts
	rows, err := a.Logs.Query(`SELECT ts,to_addr,dport,proto,out_if FROM fw_events WHERE from_addr=? AND action='allow' AND out_if<>'' AND ts>=? ORDER BY ts DESC,id DESC LIMIT 5000`,
		ip, time.Now().Unix()-viaWindow)
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
	for l, n := range by {
		out.Others = append(out.Others, ViaCount{l, n})
	}
	sort.Slice(out.Others, func(i, j int) bool { return out.Others[i].N > out.Others[j].N })
	switch {
	case tl == "":
		out.Verdict = "none"
		out.Sentence = "This device is not sent through a VPN."
	case out.Through == 0 && out.Direct == 0:
		out.Verdict = "idle"
		out.Sentence = "No new connections from this device in the last hour, so there is nothing to check yet. Open a website on it and check again."
	case out.Direct > 0:
		out.Verdict = "leak"
		out.Sentence = strconv.Itoa(out.Direct) + " connection" + plural(out.Direct) + " from this device left without using " + tl + " in the last hour. See below."
		if ts != "up" {
			out.Sentence += " The VPN is not working right now."
		}
	default:
		out.Verdict = "ok"
		out.Sentence = "All " + strconv.Itoa(out.Through) + " new connection" + plural(out.Through) + " from this device in the last hour went through " + tl + "."
	}
	return out
}

func (a *App) registerViaRoutes() {
	a.API.Handle("GET /api/vpn/check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		ip := r.URL.Query().Get("ip")
		if net.ParseIP(ip) == nil {
			api.Error(w, http.StatusBadRequest, "give the device's address")
			return
		}
		api.JSON(w, 200, a.viaCheck(ip, a.deviceName(ip)))
	})
}

package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/devices"
	"gatehouse/internal/fw"
	"gatehouse/internal/netd"
)

// Tunnel health. Every VPN gets one plain state the console can show and alert on:
//
//	up          working: traffic gets through
//	connecting  just started, or still negotiating
//	degraded    connected, but the test through it fails
//	down        not connected
//	off         switched off by the administrator
type healthIn struct {
	Enabled   bool
	LinkUp    bool
	WG        bool  // WireGuard: handshakes tell whether the provider answers
	Handshake int64 // unix seconds of the latest handshake, 0 for never
	HasCheck  bool
	CheckOK   bool
	CheckAt   int64
	Target    string
	Now       int64
	AppliedAt int64
}

const (
	handshakeStale = 180 // WireGuard re-handshakes about every two minutes while traffic flows
	settleSeconds  = 90
)

func clientHealth(in healthIn) (state, msg string) {
	young := in.AppliedAt > 0 && in.Now-in.AppliedAt < settleSeconds
	switch {
	case !in.Enabled:
		return "off", "Turned off"
	case !in.LinkUp:
		if young {
			return "connecting", "Connecting"
		}
		return "down", "The tunnel is not connected"
	case in.WG && in.Handshake == 0:
		if young {
			return "connecting", "Waiting for the provider to answer"
		}
		return "down", "The provider is not answering. Check the server address and keys"
	case in.WG && in.Now-in.Handshake > handshakeStale:
		return "down", fmt.Sprintf("No contact with the provider for %s", ago(in.Now-in.Handshake))
	case in.HasCheck && in.CheckAt == 0:
		if young {
			return "connecting", "Testing the connection"
		}
		return "up", "Connected"
	case in.HasCheck && !in.CheckOK:
		return "degraded", fmt.Sprintf("Connected, but traffic is not getting through (test to %s failed)", in.Target)
	}
	return "up", "Connected"
}

func ago(sec int64) string {
	switch {
	case sec < 90:
		return fmt.Sprintf("%d seconds", sec)
	case sec < 5400:
		return fmt.Sprintf("%d minutes", (sec+30)/60)
	}
	return fmt.Sprintf("%d hours", (sec+1800)/3600)
}

// Views --------------------------------------------------------------------

type UseView struct {
	ID     int64    `json:"id"`
	Kind   string   `json:"kind"` // network, device, cidr
	Value  string   `json:"value"`
	Label  string   `json:"label"`
	Detail string   `json:"detail"`
	IPs    []string `json:"ips"`
}

type PeerView struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	State    string `json:"state"` // connected, idle, never, revoked
	LastSeen int64  `json:"last_seen"`
	Real     string `json:"real"`
	Rx       int64  `json:"rx"`
	Tx       int64  `json:"tx"`
	Created  int64  `json:"created"`
	Networks string `json:"networks,omitempty"`
	Paused   bool   `json:"paused,omitempty"` // switched off without being deleted
}

type VPNView struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"` // wireguard or openvpn
	Dev        string     `json:"dev"`
	Label      string     `json:"label"`
	Role       string     `json:"role"` // server or client
	Enabled    bool       `json:"enabled"`
	State      string     `json:"state"`
	Message    string     `json:"message"`
	Address    string     `json:"address"`  // the tunnel's own address
	ExitIP     string     `json:"exit_ip"`  // what the internet sees
	Endpoint   string     `json:"endpoint"` // client: the provider; server: what devices connect to
	Port       int        `json:"port"`
	Proto      string     `json:"proto"`
	Handshake  int64      `json:"handshake"`
	Rx         int64      `json:"rx"`
	Tx         int64      `json:"tx"`
	RTTMs      int        `json:"rtt_ms"`
	Check      string     `json:"check_target"`
	CheckedAt  int64      `json:"checked_at"`
	KillSwitch bool       `json:"killswitch"`
	RouteAll   bool       `json:"route_all"`
	Access     string     `json:"access"`   // full or internet
	Site       bool       `json:"site"`     // a link to another site
	Networks   []string   `json:"networks"` // site link: the networks at the other end
	Subnet     string     `json:"subnet"`
	Uses       []UseView  `json:"uses"`
	Peers      []PeerView `json:"peers"`
	Connected  int        `json:"connected"`
	MTU        int        `json:"mtu"` // 0 means automatic
	Warnings   []string   `json:"warnings,omitempty"`
	Created    int64      `json:"created"`
}

type vpnSnapshot struct {
	Views []VPNView `json:"vpns"`
	At    int64     `json:"at"`
}

// vpnState keeps the latest picture and what the alerts last saw.
type vpnState struct {
	mu      sync.Mutex
	snap    vpnSnapshot
	last    map[string]string // dev -> state
	since   map[string]int64  // dev -> when it entered the state
	applied int64
}

func (a *App) vpnMarkApplied() {
	a.vpn.mu.Lock()
	a.vpn.applied = time.Now().Unix()
	a.vpn.mu.Unlock()
}

func (a *App) vpnSnapshot() vpnSnapshot {
	a.vpn.mu.Lock()
	defer a.vpn.mu.Unlock()
	return a.vpn.snap
}

func (a *App) deviceNames() map[string]string {
	out := map[string]string{}
	if a.Devices == nil {
		return out
	}
	list, err := a.Devices.List()
	if err != nil {
		return out
	}
	for _, d := range list {
		out[strings.ToLower(d.MAC)] = deviceTitle(d)
	}
	return out
}

func deviceTitle(d devices.Device) string {
	switch {
	case d.Name != "":
		return d.Name
	case d.Hostname != "":
		return d.Hostname
	}
	return d.MAC
}

func (a *App) usesOf(dev string, srcs map[string][]string) []UseView {
	rows, err := a.Config.Query(`SELECT id,kind,value,label FROM vpn_routes WHERE tunnel=? ORDER BY id`, dev)
	if err != nil {
		return nil
	}
	var out []UseView
	for rows.Next() {
		var u UseView
		rows.Scan(&u.ID, &u.Kind, &u.Value, &u.Label)
		out = append(out, u)
	}
	rows.Close()
	names := a.deviceNames()
	for i := range out {
		u := &out[i]
		switch u.Kind {
		case "device":
			if u.Label == "" {
				u.Label = names[strings.ToLower(u.Value)]
			}
			u.Detail = u.Value
		case "cidr":
			u.Detail = "Address range"
		case "network":
			u.Detail = "Everything on this network"
		case "group":
			u.Detail = "Every device in this group"
		}
		if u.Label == "" {
			u.Label = u.Value
		}
		// the addresses this entry covers right now come from the same function the routing uses
		one, _ := fw.TunnelSourcesFor(a.Config, u.Kind, u.Value)
		u.IPs = one
	}
	return out
}

// computeVPN merges the stored configuration with the helper's live status.
func (a *App) computeVPN(st netd.VPNStatus) ([]VPNView, error) {
	wgs, err := a.listWG(a.Config)
	if err != nil {
		return nil, err
	}
	ovs, err := a.listOVPN(a.Config)
	if err != nil {
		return nil, err
	}
	a.vpn.mu.Lock()
	applied := a.vpn.applied
	a.vpn.mu.Unlock()
	now := st.Now
	if now == 0 {
		now = time.Now().Unix()
	}
	defEndpoint := a.Config.Setting(vpnEndpointSetting, "")
	var out []VPNView
	for _, w := range wgs {
		v := VPNView{ID: w.ID, Kind: "wireguard", Dev: w.Name, Label: w.Label, Role: w.Mode, Enabled: w.Enabled, Address: w.Address,
			Check: w.Check, KillSwitch: w.KillSwitch, RouteAll: w.RouteAll, Created: w.CreatedAt, Port: w.Port, Proto: "udp", Site: w.Site, MTU: w.MTU}
		if v.Label == "" {
			v.Label = w.Name
		}
		peers, err := a.listWGPeers(a.Config, w.ID)
		if err != nil {
			return nil, err
		}
		live := map[string]netd.WGPeerStatus{}
		wst, haveWG := st.WG[w.Name]
		for _, p := range wst.Peers {
			live[p.PublicKey] = p
		}
		if w.Mode == "server" {
			v.Access = "internet"
			if a.zoneIsFull(a.Config, w.ZoneID) {
				v.Access = "full"
			}
			v.Subnet = w.Address
			ep := w.Endpoint
			if ep == "" {
				ep = defEndpoint
			}
			v.Endpoint = ep
			for _, p := range peers {
				pv := PeerView{ID: p.ID, Name: p.Name, Address: strings.TrimSuffix(p.Allowed, "/32"), State: "never", Created: p.CreatedAt, Paused: !p.Enabled}
				if lp, ok := live[p.PublicKey]; ok {
					pv.Rx, pv.Tx, pv.LastSeen, pv.Real = lp.Rx, lp.Tx, lp.Handshake, lp.Endpoint
					switch {
					case lp.Handshake == 0:
					case now-lp.Handshake < handshakeStale:
						pv.State = "connected"
					default:
						pv.State = "idle"
					}
				}
				if pv.State == "connected" {
					v.Connected++
				}
				v.Peers = append(v.Peers, pv)
			}
			switch {
			case !w.Enabled:
				v.State, v.Message = "off", "Turned off"
			case !haveWG && st.Now > 0:
				if applied > 0 && now-applied < settleSeconds {
					v.State, v.Message = "connecting", "Starting"
				} else {
					v.State, v.Message = "down", "The VPN is not running"
				}
			case w.Site:
				v.State, v.Message = "up", "Waiting for the other site"
				if v.Connected > 0 {
					v.Message = "Connected to the other site"
				}
			case v.Endpoint == "":
				v.State, v.Message = "degraded", "Set the address devices will connect to"
			default:
				v.State = "up"
				v.Message = "Waiting for devices"
				if v.Connected == 1 {
					v.Message = "1 device connected"
				} else if v.Connected > 1 {
					v.Message = fmt.Sprintf("%d devices connected", v.Connected)
				}
			}
		} else {
			var hs int64
			if len(peers) > 0 {
				p := peers[0]
				v.Endpoint = p.Endpoint
				if lp, ok := live[p.PublicKey]; ok {
					hs, v.Rx, v.Tx = lp.Handshake, lp.Rx, lp.Tx
				}
			}
			v.Handshake = hs
			chk, haveChk := st.Checks[w.Name]
			v.RTTMs, v.CheckedAt, v.ExitIP = chk.RTTMs, chk.At, chk.ExitIP
			v.State, v.Message = clientHealth(healthIn{Enabled: w.Enabled, LinkUp: haveWG || st.Links[w.Name].Up, WG: true, Handshake: hs,
				HasCheck: w.Check != "", CheckOK: haveChk && chk.OK, CheckAt: chk.At, Target: w.Check, Now: now, AppliedAt: applied})
			v.Uses = a.usesOf(w.Name, nil)
		}
		out = append(out, v)
	}
	for _, o := range ovs {
		v := VPNView{ID: o.ID, Kind: "openvpn", Dev: o.Name, Label: o.Label, Role: o.Mode, Enabled: o.Enabled, Check: o.Check,
			KillSwitch: o.KillSwitch, RouteAll: o.RouteAll, Created: o.CreatedAt, Port: o.Port, Proto: o.Proto}
		if v.Label == "" {
			v.Label = o.Name
		}
		os := st.OVPN[o.Name]
		link := st.Links[o.Name]
		if o.Mode == "server" {
			v.Access = "internet"
			if a.zoneIsFull(a.Config, o.ZoneID) {
				v.Access = "full"
			}
			v.Subnet = o.Subnet
			v.Address = o.Subnet
			v.Endpoint = o.Endpoint
			if v.Endpoint == "" {
				v.Endpoint = defEndpoint
			}
			users, err := a.listOVPNUsers(a.Config, o.ID)
			if err != nil {
				return nil, err
			}
			byName := map[string]netd.OVPNClientStatus{}
			for _, c := range os.Clients {
				byName[c.Name] = c
			}
			for _, u := range users {
				if u.RevokedAt > 0 {
					continue
				}
				pv := PeerView{ID: u.ID, Name: u.Name, State: "never", Created: u.CreatedAt, Paused: u.Paused}
				if c, ok := byName[u.Name]; ok && !u.Paused {
					pv.State, pv.Address, pv.Real, pv.Rx, pv.Tx, pv.LastSeen = "connected", c.Virtual, c.Real, c.Rx, c.Tx, c.Since
					v.Connected++
				}
				v.Peers = append(v.Peers, pv)
			}
			switch {
			case !o.Enabled:
				v.State, v.Message = "off", "Turned off"
			case !os.Active && st.Now > 0:
				if applied > 0 && now-applied < settleSeconds {
					v.State, v.Message = "connecting", "Starting"
				} else {
					v.State, v.Message = "down", "The VPN is not running"
				}
			case v.Endpoint == "":
				v.State, v.Message = "degraded", "Set the address devices will connect to"
			default:
				v.State, v.Message = "up", "Waiting for devices"
				if v.Connected == 1 {
					v.Message = "1 device connected"
				} else if v.Connected > 1 {
					v.Message = fmt.Sprintf("%d devices connected", v.Connected)
				}
			}
		} else {
			if imp := a.profileRemote(o); imp != "" {
				v.Endpoint = imp
			}
			v.Address = link.Addr
			chk, haveChk := st.Checks[o.Name]
			v.RTTMs, v.CheckedAt, v.ExitIP = chk.RTTMs, chk.At, chk.ExitIP
			v.State, v.Message = clientHealth(healthIn{Enabled: o.Enabled, LinkUp: link.Up && os.Active, HasCheck: o.Check != "", CheckOK: haveChk && chk.OK,
				CheckAt: chk.At, Target: o.Check, Now: now, AppliedAt: applied})
			if len(os.Clients) == 0 && os.Active {
				// a client's own byte counters are not exported; the check carries the health
			}
			v.Uses = a.usesOf(o.Name, nil)
		}
		out = append(out, v)
	}
	for i := range out {
		if out[i].Role == "server" && out[i].Port > 0 && out[i].Enabled {
			out[i].Warnings = append(out[i].Warnings, a.portClash(out[i].Proto, out[i].Port)...)
		}
	}
	return out, nil
}

// portClash says, in plain words, when a port forward uses the port this VPN listens on.
func (a *App) portClash(proto string, port int) []string {
	rows, err := a.Config.Query(`SELECT name,proto,ext_port FROM nat_forwards WHERE enabled=1`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, fp, ext string
		rows.Scan(&name, &fp, &ext)
		if fp != "both" && fp != "tcp+udp" && fp != proto {
			continue
		}
		for _, part := range strings.Split(ext, ",") {
			lo, hi, isRange := strings.Cut(strings.TrimSpace(part), "-")
			a1, e1 := strconv.Atoi(lo)
			b1 := a1
			var e2 error
			if isRange {
				b1, e2 = strconv.Atoi(hi)
			}
			if e1 == nil && e2 == nil && port >= a1 && port <= b1 {
				out = append(out, fmt.Sprintf("The port forward %q also uses port %d (%s), so devices cannot reach this VPN. Change one of them.", name, port, strings.ToUpper(proto)))
				break
			}
		}
	}
	return out
}

// refreshVPN reads live status, updates the cache, raises alerts on changes, and tells the console.
func (a *App) refreshVPN() {
	var st netd.VPNStatus
	if a.Net != nil {
		if s, err := a.Net.VPNStatus(); err == nil {
			st = s
		}
	}
	views, err := a.computeVPN(st)
	if err != nil {
		return
	}
	now := time.Now().Unix()
	a.vpn.mu.Lock()
	if a.vpn.last == nil {
		a.vpn.last, a.vpn.since = map[string]string{}, map[string]int64{}
	}
	type change struct {
		v    VPNView
		from string
	}
	var changes []change
	seen := map[string]bool{}
	for _, v := range views {
		seen[v.Dev] = true
		prev, known := a.vpn.last[v.Dev]
		if !known || prev != v.State {
			if known {
				changes = append(changes, change{v, prev})
			}
			a.vpn.last[v.Dev], a.vpn.since[v.Dev] = v.State, now
		}
	}
	for d := range a.vpn.last {
		if !seen[d] {
			delete(a.vpn.last, d)
			delete(a.vpn.since, d)
		}
	}
	a.vpn.snap = vpnSnapshot{Views: views, At: now}
	snap := a.vpn.snap
	a.vpn.mu.Unlock()

	for _, c := range changes {
		a.vpnAlert(c.v, c.from)
	}
	a.Bus.Publish("vpn.status", snap)
}

// vpnAlert writes a line to the event log when a tunnel changes state in a way that matters.
// Settling from "connecting" to "up" is not news, and neither is turning one off on purpose.
func (a *App) vpnAlert(v VPNView, from string) {
	title, sev := "", "info"
	switch {
	case v.State == "down" && from != "off":
		title, sev = fmt.Sprintf("VPN %s is down", v.Label), "warning"
	case v.State == "degraded":
		title, sev = fmt.Sprintf("VPN %s is connected but not passing traffic", v.Label), "warning"
	case v.State == "up" && (from == "down" || from == "degraded"):
		title = fmt.Sprintf("VPN %s is back up", v.Label)
	}
	if title == "" || a.Logs == nil {
		return
	}
	detail := v.Message
	if v.Role == "client" && v.KillSwitch && v.State != "up" && len(v.Uses) > 0 {
		detail += ". The kill switch is holding back internet for the devices that use it."
	}
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, time.Now().Unix(), "vpn", sev, "system", title, detail)
	a.Bus.Publish("events", map[string]int64{"at": time.Now().Unix()})
}

// vpnMonitor polls the helper and keeps the console and the event log current.
func (a *App) vpnMonitor(ctx context.Context) {
	if a.Net == nil {
		return
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	a.refreshVPN()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.refreshVPN()
		}
	}
}

func (a *App) profileRemote(o ovpnRow) string {
	for _, line := range strings.Split(o.Profile, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && strings.EqualFold(f[0], "remote") {
			return f[1] + ":" + f[2]
		}
		if len(f) == 2 && strings.EqualFold(f[0], "remote") {
			return f[1]
		}
	}
	return ""
}

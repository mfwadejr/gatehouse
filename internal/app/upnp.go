package app

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
	"gatehouse/internal/netd"
	"gatehouse/internal/upnp"
)

// UPnP lets a game console or media app on the inside open a port for itself. It is off unless
// a network has "devices may open ports" switched on AND an internet connection allows it, and
// a device can only ever open ports to itself.

type upnpState struct {
	once sync.Once
	tbl  *upnp.Table
	srv  *upnp.Server
	mu   sync.Mutex
	log  []UPnPEvent
}

type UPnPEvent struct {
	At   int64  `json:"at"`
	Kind string `json:"kind"`
	Msg  string `json:"msg"`
}

func (a *App) upnpInit() {
	a.upnp.once.Do(func() {
		t := upnp.NewTable(a.upnpPolicy())
		t.OnEvent = func(kind, msg string) {
			a.upnp.mu.Lock()
			a.upnp.log = append(a.upnp.log, UPnPEvent{time.Now().Unix(), kind, msg})
			if len(a.upnp.log) > 200 {
				a.upnp.log = a.upnp.log[len(a.upnp.log)-200:]
			}
			a.upnp.mu.Unlock()
		}
		t.OnChange = func(ms []upnp.Mapping) {
			a.pushUPnP(ms)
			a.Bus.Publish("upnp.maps", map[string]int{"count": len(ms)})
		}
		a.upnp.tbl = t
		a.upnp.srv = upnp.NewServer(t, "Gatehouse", a.upnpConfig)
	})
}

// upnpPolicy is rebuilt from the settings whenever they change.
func (a *App) upnpPolicy() upnp.Policy {
	p := upnp.DefaultPolicy()
	if h, err := strconv.Atoi(a.setting("upnp.max_lease_hours", "24")); err == nil && h >= 1 && h <= 168 {
		p.MaxLease = time.Duration(h) * time.Hour
	}
	reserved := map[int]bool{upnp.ControlPort: true}
	for _, port := range fw.DefaultConsolePorts {
		reserved[port] = true
	}
	if _, ps, err := net.SplitHostPort(a.Cfg.WebAddr); err == nil {
		if n, err := strconv.Atoi(ps); err == nil {
			reserved[n] = true
		}
	}
	for _, q := range []string{`SELECT listen_port FROM vpn_wg`, `SELECT port FROM vpn_ovpn`} {
		if rows, err := a.Config.Query(q); err == nil {
			for rows.Next() {
				var n int
				rows.Scan(&n)
				reserved[n] = true
			}
			rows.Close()
		}
	}
	forwarded := portSet(a.Config, `SELECT ext_port FROM nat_forwards WHERE enabled=1`)
	p.Reserved = func(proto string, port int) bool { return reserved[port] || forwarded[port] }
	if a.setting("upnp.restrict", "0") == "1" {
		allowed := map[string]bool{}
		if rows, err := a.Config.Query(`SELECT mac FROM upnp_allow`); err == nil {
			for rows.Next() {
				var m string
				rows.Scan(&m)
				allowed[strings.ToLower(m)] = true
			}
			rows.Close()
		}
		p.Allowed = func(ip netip.Addr) bool {
			return allowed[strings.ToLower(a.DHCP.Lookups().MACFor(ip.String()))]
		}
	}
	return p
}

// portSet reads a column of "80" or "8000-8010" values into a set of ports.
func portSet(db interface {
	Query(string, ...any) (*sql.Rows, error)
}, q string) map[int]bool {
	out := map[int]bool{}
	rows, err := db.Query(q)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		rows.Scan(&s)
		lo, hi, _ := strings.Cut(strings.TrimSpace(s), "-")
		l, e1 := strconv.Atoi(lo)
		h := l
		if hi != "" {
			h, _ = strconv.Atoi(hi)
		}
		if e1 == nil && h >= l && h-l < 5000 {
			for p := l; p <= h; p++ {
				out[p] = true
			}
		}
	}
	return out
}

// upnpConfig works out which networks to listen on and which internet connection mappings use.
func (a *App) upnpConfig() upnp.Config {
	var cfg upnp.Config
	if a.Net == nil {
		return cfg
	}
	m, err := fw.Load(a.Config)
	if err != nil || !m.UPnP {
		return cfg
	}
	st, err := a.Net.WanStatus()
	if err != nil {
		return cfg
	}
	rows, err := a.Config.Query(`SELECT name,iface,type,vlan_id FROM wan_links WHERE enabled=1 AND upnp=1 ORDER BY priority,id`)
	if err != nil {
		return cfg
	}
	for rows.Next() {
		var name, iface, typ string
		var vlan int
		rows.Scan(&name, &iface, &typ, &vlan)
		s, ok := st[name]
		if cfg.WanDev != "" || !ok || !s.Up || (len(st) > 1 && !s.Active) {
			continue
		}
		dev := fw.WanDevice(iface, typ, vlan, name)
		if ip := firstV4(dev); ip != nil {
			cfg.WanDev, cfg.ExtIP = dev, ip.Addr.String()
		}
	}
	rows.Close()
	if cfg.WanDev == "" {
		return cfg
	}
	for _, z := range m.Zones {
		if !z.UPnP || z.Kind == fw.KindWan {
			continue
		}
		for _, ifn := range z.Ifaces {
			if l := firstV4(ifn); l != nil {
				cfg.LANs = append(cfg.LANs, upnp.LAN{Iface: ifn, IP: l.Addr, Prefix: l.Prefix})
			}
		}
	}
	return cfg
}

type v4 struct {
	Addr   netip.Addr
	Prefix netip.Prefix
}

func firstV4(dev string) *v4 {
	ifi, err := net.InterfaceByName(dev)
	if err != nil {
		return nil
	}
	addrs, _ := ifi.Addrs()
	for _, ad := range addrs {
		if p, err := netip.ParsePrefix(ad.String()); err == nil && p.Addr().Is4() {
			return &v4{p.Addr(), p.Masked()}
		}
	}
	return nil
}

func (a *App) pushUPnP(ms []upnp.Mapping) {
	if a.Net == nil {
		return
	}
	dev := a.upnp.srv.WanDev()
	out := make([]netd.UPnPMapping, 0, len(ms))
	for _, m := range ms {
		out = append(out, netd.UPnPMapping{ID: m.ID, Proto: m.Proto, ExtPort: m.ExtPort, IntPort: m.IntPort, IntAddr: m.IntAddr, WanDev: dev})
	}
	if len(out) > 0 && dev == "" {
		out = nil
	}
	if err := a.Net.UPnPApply(out); err != nil {
		a.upnp.tbl.OnEvent("error", "could not apply port changes: "+err.Error())
	}
}

// runUPnP starts the server and makes sure no stale mappings survive a restart.
func (a *App) runUPnP(ctx context.Context) {
	a.upnpInit()
	if a.Net != nil {
		a.Net.UPnPApply(nil)
	}
	a.upnp.srv.Run(ctx)
}

type upnpStatus struct {
	Active     bool           `json:"active"`
	Why        string         `json:"why"`
	ExtIP      string         `json:"ext_ip"`
	Private    bool           `json:"private_ext"`
	Networks   []string       `json:"networks"`
	Mappings   []upnp.Mapping `json:"mappings"`
	Log        []UPnPEvent    `json:"log"`
	Restrict   bool           `json:"restrict"`
	LeaseHours int            `json:"max_lease_hours"`
	Allowed    []upnpAllowRow `json:"allowed"`
}

type upnpAllowRow struct {
	MAC  string `json:"mac"`
	Note string `json:"note"`
}

func (a *App) upnpStatus() upnpStatus {
	a.upnpInit()
	cfg := a.upnpConfig()
	st := upnpStatus{Mappings: a.upnp.tbl.List(), ExtIP: cfg.ExtIP, Allowed: []upnpAllowRow{}, Networks: []string{}}
	st.Restrict = a.setting("upnp.restrict", "0") == "1"
	st.LeaseHours, _ = strconv.Atoi(a.setting("upnp.max_lease_hours", "24"))
	if st.LeaseHours == 0 {
		st.LeaseHours = 24
	}
	for _, l := range cfg.LANs {
		st.Networks = append(st.Networks, l.Iface)
	}
	st.Active = cfg.WanDev != "" && len(cfg.LANs) > 0
	switch {
	case a.Net == nil:
		st.Why = "The network helper is not running."
	case st.Active:
		st.Why = "On"
	case cfg.WanDev == "":
		st.Why = "Off. Switch it on for an internet connection and for a network, and make sure that connection is up."
	default:
		st.Why = "Off. No network has \"devices may open ports\" switched on."
	}
	if ip, err := netip.ParseAddr(cfg.ExtIP); err == nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast() || (ip.Is4() && ip.As4()[0] == 100 && ip.As4()[1]&0xc0 == 64)) {
		st.Private = true
	}
	a.upnp.mu.Lock()
	st.Log = append([]UPnPEvent{}, a.upnp.log...)
	a.upnp.mu.Unlock()
	if rows, err := a.Config.Query(`SELECT mac,note FROM upnp_allow ORDER BY mac`); err == nil {
		for rows.Next() {
			var r upnpAllowRow
			rows.Scan(&r.MAC, &r.Note)
			st.Allowed = append(st.Allowed, r)
		}
		rows.Close()
	}
	return st
}

func (a *App) registerUPnPRoutes() {
	s := a.API
	s.Handle("GET /api/upnp", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.upnpStatus())
	})
	s.HandleAdmin("DELETE /api/upnp/mappings/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.upnpInit()
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if !a.upnp.tbl.Revoke(id) {
			http.Error(w, "that port is already closed", 404)
			return
		}
		ok(w)
	})
	s.HandleAdmin("DELETE /api/upnp/mappings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.upnpInit()
		a.upnp.tbl.Clear("closed from the console")
		ok(w)
	})
	s.HandleAdmin("PUT /api/upnp/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Restrict   bool           `json:"restrict"`
			LeaseHours int            `json:"max_lease_hours"`
			Allowed    []upnpAllowRow `json:"allowed"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.LeaseHours < 1 || in.LeaseHours > 168 {
			http.Error(w, "Choose between 1 hour and 7 days.", 400)
			return
		}
		macs := map[string]string{}
		for _, x := range in.Allowed {
			m, err := net.ParseMAC(strings.TrimSpace(x.MAC))
			if err != nil || len(m) != 6 {
				http.Error(w, "\""+x.MAC+"\" is not a device address.", 400)
				return
			}
			if len(x.Note) > 80 {
				x.Note = x.Note[:80]
			}
			macs[strings.ToLower(m.String())] = x.Note
		}
		tx, err := a.Config.Begin()
		if err != nil {
			fail(w, err)
			return
		}
		tx.Exec(`DELETE FROM upnp_allow`)
		for m, n := range macs {
			tx.Exec(`INSERT INTO upnp_allow(mac,note) VALUES(?,?)`, m, n)
		}
		if err := tx.Commit(); err != nil {
			fail(w, err)
			return
		}
		a.Config.SetSetting("upnp.restrict", strconv.Itoa(b2i(in.Restrict)))
		a.Config.SetSetting("upnp.max_lease_hours", strconv.Itoa(in.LeaseHours))
		a.upnpInit()
		a.upnp.tbl.SetPolicy(a.upnpPolicy())
		ok(w)
	})
}

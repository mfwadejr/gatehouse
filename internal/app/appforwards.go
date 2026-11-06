package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/fw"
)

// Port forwarding by app and device. A person picks an app (a short built-in list plus their own on
// the Services page), a device, and the internet connections. NopeWarden writes one forward for each
// port the app uses, keeps them pointing at the device's current address, and says in one line what
// is open.

// forwardApps is deliberately short. Anything else is added as the person's own app.
var forwardApps = map[string][]string{
	"Plex":                {"tcp/32400"},
	"Jellyfin":            {"tcp/8096"},
	"Home Assistant":      {"tcp/8123"},
	"Minecraft (Java)":    {"tcp/25565"},
	"Minecraft (Bedrock)": {"udp/19132"},
	"Web server":          {"tcp/80", "tcp/443"},
	"WireGuard server":    {"udp/51820"},
}

// appPort is one port of an app. Port is the port people connect to from the internet (the outside
// port); Inside is the port on the device when that differs; Std is the app's own standard port,
// which is the key the outside and inside overrides are given by.
type appPort struct {
	Proto  string `json:"proto"`
	Port   string `json:"port"`
	Inside string `json:"inside,omitempty"`
	Std    string `json:"std,omitempty"`
}

type appChoice struct {
	Name   string   `json:"name"`
	Ports  []string `json:"ports"`
	Custom bool     `json:"custom"`
}

func specText(sp fw.PortSpec) appPort {
	p := strconv.Itoa(sp.Lo)
	if sp.Hi != sp.Lo {
		p += "-" + strconv.Itoa(sp.Hi)
	}
	return appPort{Proto: sp.Proto, Port: p}
}

// appPorts finds the ports of an app by name: the built-in list first, then the person's own.
func (a *App) appPorts(name string) ([]appPort, error) {
	var texts []string
	if t, ok := forwardApps[name]; ok {
		texts = t
	} else {
		lists, _ := fw.PortLists(a.Config)
		for _, l := range lists {
			if l.Name == name {
				texts = l.Ports
			}
		}
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("NopeWarden does not know an app called %q. Add it as a new app first", name)
	}
	var out []appPort
	for _, t := range texts {
		specs, err := fw.ParseService(t)
		if err != nil {
			return nil, err
		}
		for _, sp := range specs {
			if sp.Proto != "tcp" && sp.Proto != "udp" {
				continue
			}
			out = append(out, specText(sp))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%q has no TCP or UDP ports to open", name)
	}
	return out, nil
}

// insideOf is the port on the device when it differs from the outside port, otherwise "".
func insideOf(ext, int string) string {
	if int == "" || int == ext {
		return ""
	}
	return int
}

// parsePortSpec reads "32400" or "27000-27050" into its two ends.
func parsePortSpec(t string) (lo, hi int, err error) {
	t = strings.TrimSpace(t)
	a, b, isRange := strings.Cut(t, "-")
	if lo, err = strconv.Atoi(strings.TrimSpace(a)); err != nil {
		return 0, 0, fmt.Errorf("%q is not a port number", t)
	}
	hi = lo
	if isRange {
		if hi, err = strconv.Atoi(strings.TrimSpace(b)); err != nil {
			return 0, 0, fmt.Errorf("%q is not a port number", t)
		}
	}
	if lo < 1 || hi > 65535 || hi < lo {
		return 0, 0, fmt.Errorf("%q is not a valid port. Use a number from 1 to 65535", t)
	}
	return lo, hi, nil
}

// errPortConflict is returned when an outside port is already used. The screen shows it as a
// question ("which port should this one use?"), never as a silent change.
type errPortConflict struct {
	Proto  string `json:"proto"`
	Port   string `json:"port"`
	UsedBy string `json:"used_by"`
}

func (e *errPortConflict) Error() string {
	return fmt.Sprintf("%s port %s is already used by %s. Enter a different outside port for this one", strings.ToUpper(e.Proto), e.Port, e.UsedBy)
}

// forwardPortUse says what already uses an outside port range, or "" when it is free. Ports used by
// the group being edited do not count. It looks at the VPN servers and every other forward.
func (a *App) forwardPortUse(proto string, lo, hi int, ownGroup string) string {
	// The console, DNS, DHCP and remote login only answer on your own networks, never on the internet
	// side, so a forward from the internet may use the same numbers. A VPN server does listen on the
	// internet side, so its port is taken.
	for _, q := range []string{`SELECT listen_port FROM vpn_wg`, `SELECT port FROM vpn_ovpn`} {
		if rows, err := a.Config.Query(q); err == nil {
			for rows.Next() {
				var n int
				rows.Scan(&n)
				if n >= lo && n <= hi {
					rows.Close()
					return "a VPN"
				}
			}
			rows.Close()
		}
	}
	rows, err := a.Config.Query(`SELECT name,grp,app,mac,int_addr,ext_port FROM nat_forwards WHERE proto=?`, proto)
	if err != nil {
		return ""
	}
	type used struct{ name, grp, app, mac, addr, ext string }
	var all []used
	for rows.Next() {
		var u used
		rows.Scan(&u.name, &u.grp, &u.app, &u.mac, &u.addr, &u.ext)
		all = append(all, u)
	}
	rows.Close()
	for _, u := range all {
		if ownGroup != "" && u.grp == ownGroup {
			continue
		}
		l, h, err := parsePortSpec(u.ext)
		if err != nil || h < lo || l > hi {
			continue
		}
		if u.grp == "" {
			return "the port forward " + u.name
		}
		return fmt.Sprintf("%s on %s", orWord(u.app, u.grp), a.fwdDeviceName(u.mac, u.addr))
	}
	return ""
}

func (a *App) appChoices() []appChoice {
	out := []appChoice{}
	for n, p := range forwardApps {
		out = append(out, appChoice{Name: n, Ports: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if lists, err := fw.PortLists(a.Config); err == nil {
		for _, l := range lists {
			out = append(out, appChoice{Name: l.Name, Ports: l.Ports, Custom: true})
		}
	}
	return out
}

// deviceIP finds a device's address now: a reservation first, then its latest lease.
func (a *App) deviceIP(mac string) string {
	var ip string
	if a.Config.QueryRow(`SELECT ip FROM dhcp_reservations WHERE mac=? LIMIT 1`, mac).Scan(&ip) == nil && ip != "" {
		return ip
	}
	if a.Config.QueryRow(`SELECT ip FROM dhcp_leases WHERE mac=? AND state='active' ORDER BY expires DESC LIMIT 1`, mac).Scan(&ip) == nil {
		return ip
	}
	return ""
}

// wanDevices maps an internet connection's name to the device the firewall knows it by.
func (a *App) fwdWanDevices() (byName map[string]string, byDev map[string]string, order []string) {
	byName, byDev = map[string]string{}, map[string]string{}
	rows, err := a.Config.Query(`SELECT name,iface,type,vlan_id FROM wan_links ORDER BY priority,id`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var n, i, t string
		var v int
		rows.Scan(&n, &i, &t, &v)
		d := fw.WanDevice(i, t, v, n)
		byName[n], byDev[d] = d, n
		order = append(order, n)
	}
	return
}

type forwardView struct {
	Group        string       `json:"group"`
	ID           int64        `json:"id,omitempty"` // legacy single forwards only
	Legacy       bool         `json:"legacy"`
	App          string       `json:"app"`
	Ports        []appPort    `json:"ports"`
	MAC          string       `json:"mac"`
	IP           string       `json:"ip"`
	DeviceName   string       `json:"device_name"`
	Connections  []string     `json:"connections"` // empty means all
	Src          []string     `json:"src"`
	Countries    []string     `json:"countries"`
	Enabled      bool         `json:"enabled"`
	PausedUntil  int64        `json:"paused_until"`
	Sched        *fw.Schedule `json:"sched"`
	Text         string       `json:"text"`
	Reserved     bool         `json:"reserved"`
	ReserveScope int64        `json:"reserve_scope,omitempty"`
	Blocked      string       `json:"blocked,omitempty"` // why it cannot work from outside, if so
}

func (a *App) fwdDeviceName(mac, ip string) string {
	var h string
	if mac != "" {
		a.Config.QueryRow(`SELECT hostname FROM dhcp_leases WHERE mac=? AND hostname<>'' ORDER BY expires DESC LIMIT 1`, mac).Scan(&h)
		if h == "" {
			a.Config.QueryRow(`SELECT hostname FROM dhcp_reservations WHERE mac=? AND hostname<>'' LIMIT 1`, mac).Scan(&h)
		}
	}
	if h == "" {
		h = ip
	}
	return h
}

func (a *App) forwardViews() []forwardView {
	rows, _ := fw.Forwards(a.Config)
	_, byDev, _ := a.fwdWanDevices()
	nat := map[string]natLink{}
	for _, n := range a.natReport() {
		nat[n.Name] = n
	}
	groups := map[string]*forwardView{}
	order := []string{}
	out := []forwardView{}
	conns := func(wan string) []string {
		var c []string
		for _, d := range strings.Split(wan, ",") {
			if d = strings.TrimSpace(d); d == "" {
				continue
			}
			if n, ok := byDev[d]; ok {
				c = append(c, n)
			} else {
				c = append(c, d)
			}
		}
		return c
	}
	blocked := func(c []string) string {
		check := c
		if len(check) == 0 {
			for n := range nat {
				check = append(check, n)
			}
			sort.Strings(check)
		}
		var bad []string
		for _, n := range check {
			if l, ok := nat[n]; ok && !l.Blocked {
				return ""
			} else if ok {
				bad = append(bad, n)
			}
		}
		if len(bad) == 0 {
			return ""
		}
		return "Connections from outside cannot reach " + strings.Join(bad, ", ") + " (see the Internet page)."
	}
	for _, f := range rows {
		if f.Group == "" {
			v := forwardView{Group: f.Name, ID: f.ID, Legacy: true, Ports: []appPort{{Proto: f.Proto, Port: f.ExtPort, Inside: insideOf(f.ExtPort, f.IntPort)}}, IP: f.IntAddr, Connections: conns(f.WAN),
				Src: f.Src, Countries: f.Countries, Enabled: f.Enabled, PausedUntil: f.PausedUntil, Sched: f.Sched}
			v.Text = fmt.Sprintf("%s: %s port %s goes to %s%s.", f.Name, strings.ToUpper(f.Proto), f.ExtPort, f.IntAddr, orWord(map[bool]string{true: ":" + f.IntPort}[f.IntPort != "" && f.IntPort != f.ExtPort], ""))
			v.Blocked = blocked(v.Connections)
			out = append(out, v)
			continue
		}
		g := groups[f.Group]
		if g == nil {
			g = &forwardView{Group: f.Group, App: f.App, MAC: f.MAC, IP: f.IntAddr, Connections: conns(f.WAN), Src: f.Src, Countries: f.Countries, Enabled: f.Enabled, PausedUntil: f.PausedUntil, Sched: f.Sched}
			groups[f.Group] = g
			order = append(order, f.Group)
		}
		g.Ports = append(g.Ports, appPort{Proto: f.Proto, Port: f.ExtPort, Inside: insideOf(f.ExtPort, f.IntPort)})
	}
	for _, k := range order {
		g := groups[k]
		g.DeviceName = a.fwdDeviceName(g.MAC, g.IP)
		through := "every internet connection"
		if len(g.Connections) > 0 {
			through = strings.Join(g.Connections, " and ")
		}
		// The app's own standard ports, so the screen can key the outside and inside overrides.
		if std, err := a.appPorts(g.App); err == nil && len(std) == len(g.Ports) {
			for i := range g.Ports {
				g.Ports[i].Std = std[i].Proto + "/" + std[i].Port
			}
		}
		var ps []string
		for _, p := range g.Ports {
			if p.Inside != "" {
				ps = append(ps, p.Port+" (to port "+p.Inside+" on the device)")
			} else {
				ps = append(ps, p.Port)
			}
		}
		g.Text = fmt.Sprintf("%s on %s is open on port %s through %s.", orWord(g.App, g.Group), g.DeviceName, strings.Join(ps, ", "), through)
		if !g.Enabled {
			g.Text = fmt.Sprintf("%s on %s is turned off.", orWord(g.App, g.Group), g.DeviceName)
		}
		g.Blocked = blocked(g.Connections)
		if g.MAC != "" {
			var n int
			a.Config.QueryRow(`SELECT COUNT(*) FROM dhcp_reservations WHERE mac=?`, g.MAC).Scan(&n)
			g.Reserved = n > 0
			if !g.Reserved {
				if ip, err := netip.ParseAddr(g.IP); err == nil {
					srows, _ := a.Config.Query(`SELECT id,subnet FROM dhcp_scopes WHERE mode='serve'`)
					for srows != nil && srows.Next() {
						var id int64
						var sub string
						srows.Scan(&id, &sub)
						if p, e := netip.ParsePrefix(sub); e == nil && p.Contains(ip) {
							g.ReserveScope = id
						}
					}
					if srows != nil {
						srows.Close()
					}
				}
			}
		}
		out = append(out, *g)
	}
	return out
}

type appForwardIn struct {
	Group       string   `json:"group"` // the existing group being edited, empty for a new one
	Name        string   `json:"name"`
	App         string   `json:"app"`
	MAC         string   `json:"mac"`
	IP          string   `json:"ip"`
	Connections []string `json:"connections"`
	Src         []string `json:"src"`
	Countries   []string `json:"countries"`
	Enabled     bool     `json:"enabled"`
	// Sched limits the forward to certain hours; nil means always.
	Sched *fw.Schedule `json:"sched"`
	// Outside and Inside change a port from the app's standard one. They are keyed by the standard
	// port, such as "tcp/32400": Outside is the port people connect to from the internet, Inside is
	// the port on the device. Nothing is chosen for the person; a port in use is refused.
	Outside map[string]string `json:"outside"`
	Inside  map[string]string `json:"inside"`
}

func cleanList(in []string, upper bool) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range in {
		x = strings.TrimSpace(x)
		if upper {
			x = strings.ToUpper(x)
		}
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func (a *App) saveAppForward(in appForwardIn) error {
	_, err := a.saveAppForwardNotes(in)
	return err
}

// routerPortNotes says, in plain words, when an outside port is one the firewall's own services use
// on your own networks. It never refuses: from the internet side those services are closed, so the
// forward is allowed, but the person should know the number is shared and what opening it means.
func (a *App) routerPortNotes(proto string, lo, hi int, device string) []string {
	has := func(p int) bool { return p >= lo && p <= hi }
	var out []string
	if proto == "tcp" {
		for _, p := range a.consolePorts() {
			if has(p) {
				out = append(out, fmt.Sprintf("Port %d is also where the NopeWarden console answers on your own networks. From the internet it will go to %s. The console itself stays closed to the internet.", p, device))
				break
			}
		}
		if has(22) {
			out = append(out, fmt.Sprintf("Port 22 is remote login. From the internet it will go to %s, so anyone on the internet can try to sign in to it. Use strong passwords or keys, and limit it to a country or address.", device))
		}
	}
	if has(53) {
		out = append(out, fmt.Sprintf("Port 53 is DNS, which NopeWarden also answers on your own networks. From the internet it will go to %s, so anyone could use it. Only go on if you mean to run a public DNS server.", device))
	}
	if proto == "udp" && (has(67) || has(68)) {
		out = append(out, "Ports 67 and 68 are DHCP, which hands out addresses on your own networks. Opening them to the internet is almost never what you want.")
	}
	return out
}

func (a *App) saveAppForwardNotes(in appForwardIn) ([]string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = strings.TrimSpace(in.App)
	}
	if len(in.Name) > 24 {
		return nil, errors.New("use a shorter name, up to 24 characters")
	}
	ports, err := a.appPorts(in.App)
	if err != nil {
		return nil, err
	}
	mac := ""
	ip := strings.TrimSpace(in.IP)
	if m := strings.TrimSpace(in.MAC); m != "" {
		hw, err := net.ParseMAC(m)
		if err != nil || len(hw) != 6 {
			return nil, errors.New("that is not a valid device address (MAC)")
		}
		mac = strings.ToLower(hw.String())
		if cur := a.deviceIP(mac); cur != "" {
			ip = cur
		}
	}
	if ip == "" {
		return nil, errors.New("NopeWarden has not seen that device yet. Turn it on and connect it, then try again")
	}
	if net.ParseIP(ip) == nil {
		return nil, errors.New("choose the device this should go to")
	}
	byName, _, _ := a.fwdWanDevices()
	devs := []string{}
	for _, c := range in.Connections {
		d, ok := byName[c]
		if !ok {
			return nil, fmt.Errorf("%q is not one of your internet connections", c)
		}
		devs = append(devs, d)
	}
	wan := strings.Join(devs, ",")
	src, cc := cleanList(in.Src, false), cleanList(in.Countries, true)
	// Work out the outside and inside port of every port of the app, and make sure nothing clashes.
	type pm struct{ out, in string }
	maps := make([]pm, len(ports))
	ownGroup := in.Group
	if ownGroup == "" {
		ownGroup = in.Name
	}
	for i, p := range ports {
		std := p.Proto + "/" + p.Port
		out, inn := p.Port, p.Port
		if v := strings.TrimSpace(in.Outside[std]); v != "" {
			out = v
		}
		if v := strings.TrimSpace(in.Inside[std]); v != "" {
			inn = v
		}
		olo, ohi, err := parsePortSpec(out)
		if err != nil {
			return nil, err
		}
		ilo, ihi, err := parsePortSpec(inn)
		if err != nil {
			return nil, err
		}
		if ohi-olo != ihi-ilo {
			return nil, fmt.Errorf("the outside port %s and the port on the device %s must cover the same number of ports", out, inn)
		}
		if why := a.forwardPortUse(p.Proto, olo, ohi, ownGroup); why != "" {
			return nil, &errPortConflict{Proto: p.Proto, Port: out, UsedBy: why}
		}
		maps[i] = pm{out, inn}
	}
	err = fw.Mutate(a.Config, func(tx *sql.Tx) error {
		old := in.Group
		if old == "" {
			old = in.Name
			var n int
			tx.QueryRow(`SELECT COUNT(*) FROM nat_forwards WHERE grp=? OR name=?`, in.Name, in.Name).Scan(&n)
			if n > 0 {
				return errors.New("a forward with that name already exists. Choose another name")
			}
		}
		// An edit keeps a pause that is running.
		var paused int64
		tx.QueryRow(`SELECT COALESCE(MAX(paused_until),0) FROM nat_forwards WHERE grp=?`, old).Scan(&paused)
		if in.Sched != nil && (in.Sched.Mode == "" || in.Sched.Mode == "always") {
			in.Sched = nil
		}
		if in.Sched != nil {
			if err := in.Sched.Check(time.UTC); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM nat_forwards WHERE grp=?`, old); err != nil {
			return err
		}
		for i, p := range ports {
			name := in.Name
			if i > 0 {
				name = fmt.Sprintf("%s %d", in.Name, i+1)
			}
			intPort := ""
			if maps[i].in != maps[i].out {
				intPort = maps[i].in
			}
			if _, err := fw.SaveForward(tx, fw.ForwardRow{Name: name, WAN: wan, Proto: p.Proto, ExtPort: maps[i].out, IntPort: intPort, IntAddr: ip, Enabled: in.Enabled,
				Group: in.Name, App: in.App, MAC: mac, Src: src, Countries: cc, PausedUntil: paused, Sched: in.Sched}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(cc) > 0 {
		a.geoEnsure()
	}
	a.markDirty()
	a.refreshVPN()
	var notes []string
	for i, p := range ports {
		if lo, hi, err := parsePortSpec(maps[i].out); err == nil {
			notes = append(notes, a.routerPortNotes(p.Proto, lo, hi, a.fwdDeviceName(mac, ip))...)
		}
	}
	return notes, nil
}

// syncForwardDevices points forwards that follow a device at its current address.
func (a *App) syncForwardDevices() {
	rows, err := a.Config.Query(`SELECT DISTINCT grp,mac,int_addr FROM nat_forwards WHERE grp<>'' AND mac<>''`)
	if err != nil {
		return
	}
	type ch struct{ grp, mac, from, to string }
	type seen struct{ grp, mac, ip string }
	var all []seen
	for rows.Next() {
		var x seen
		rows.Scan(&x.grp, &x.mac, &x.ip)
		all = append(all, x)
	}
	rows.Close() // the database allows one connection at a time, so this must end before the next query
	var todo []ch
	for _, x := range all {
		if cur := a.deviceIP(x.mac); cur != "" && cur != x.ip {
			todo = append(todo, ch{x.grp, x.mac, x.ip, cur})
		}
	}
	for _, c := range todo {
		err := fw.Mutate(a.Config, func(tx *sql.Tx) error {
			_, e := tx.Exec(`UPDATE nat_forwards SET int_addr=? WHERE grp=?`, c.to, c.grp)
			return e
		})
		if err != nil {
			continue
		}
		a.event("system", "info", "Port forward followed a device", fmt.Sprintf("%s now goes to %s (it was %s). Apply the firewall to use the new address.", c.grp, c.to, c.from))
		a.markDirty()
	}
}

func (a *App) forwardSyncLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.syncForwardDevices()
		}
	}
}

func (a *App) registerAppForwardRoutes() {
	s := a.API
	s.Handle("GET /api/fw/appforwards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		_, _, order := a.fwdWanDevices()
		api.JSON(w, 200, map[string]any{"forwards": a.forwardViews(), "apps": a.appChoices(), "connections": order})
	})
	s.HandleAdmin("PUT /api/fw/appforwards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in appForwardIn
		if !api.Decode(w, r, &in) {
			return
		}
		notes, err := a.saveAppForwardNotes(in)
		if err != nil {
			var pc *errPortConflict
			if errors.As(err, &pc) {
				api.JSON(w, http.StatusConflict, map[string]any{"error": pc.Error(), "conflict": pc})
				return
			}
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"status": "ok", "notes": notes})
	})
	// Live check while a port is typed: what uses it, and a free port to offer if asked.
	s.Handle("GET /api/fw/portcheck", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		proto := r.URL.Query().Get("proto")
		if proto != "tcp" && proto != "udp" {
			fail(w, errors.New("choose tcp or udp"))
			return
		}
		lo, hi, err := parsePortSpec(r.URL.Query().Get("port"))
		if err != nil {
			api.JSON(w, 200, map[string]any{"used_by": "", "error": err.Error()})
			return
		}
		group := r.URL.Query().Get("group")
		out := map[string]any{"used_by": a.forwardPortUse(proto, lo, hi, group), "notes": a.routerPortNotes(proto, lo, hi, "the device")}
		if r.URL.Query().Get("suggest") == "1" {
			width := hi - lo
			for p, tries := lo+1, 0; p+width <= 65535 && tries < 2000; p, tries = p+1, tries+1 {
				if a.forwardPortUse(proto, p, p+width, group) == "" {
					out["suggest"] = strconv.Itoa(p)
					break
				}
			}
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("POST /api/fw/appforwards/enable", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Group   string `json:"group"`
			Enabled bool   `json:"enabled"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error {
			_, e := tx.Exec(`UPDATE nat_forwards SET enabled=? WHERE grp=?`, b2i(in.Enabled), in.Group)
			return e
		}); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		ok(w)
	})
	// Pause a forward for a while, as routes and devices do: minutes 0 resumes, -1 pauses until
	// resumed. The change is staged like any firewall change.
	s.HandleAdmin("POST /api/fw/appforwards/pause", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Group   string `json:"group"`
			Minutes int    `json:"minutes"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		until, err := pausePick(in.Minutes)
		if err != nil {
			fail(w, err)
			return
		}
		var n int64
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error {
			res, e := tx.Exec(`UPDATE nat_forwards SET paused_until=? WHERE (grp<>'' AND grp=?) OR (grp='' AND name=?)`, until, in.Group, in.Group)
			if e == nil {
				n, _ = res.RowsAffected()
			}
			return e
		}); err != nil {
			fail(w, err)
			return
		}
		if n == 0 {
			fail(w, errors.New("that port forward no longer exists"))
			return
		}
		a.markDirty()
		ok(w)
	})
	s.HandleAdmin("DELETE /api/fw/appforwards/{group}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		g := r.PathValue("group")
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error {
			_, e := tx.Exec(`DELETE FROM nat_forwards WHERE grp=? AND grp<>''`, g)
			return e
		}); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		a.refreshVPN()
		ok(w)
	})
}

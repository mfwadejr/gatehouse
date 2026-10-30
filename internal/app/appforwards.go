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

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// Port forwarding by app and device. A person picks an app (a short built-in list plus their own on
// the Services page), a device, and the internet connections. Gatehouse writes one forward for each
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

type appPort struct {
	Proto string `json:"proto"`
	Port  string `json:"port"`
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
		return nil, fmt.Errorf("Gatehouse does not know an app called %q. Add it as a new app first", name)
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
	Group        string    `json:"group"`
	ID           int64     `json:"id,omitempty"` // legacy single forwards only
	Legacy       bool      `json:"legacy"`
	App          string    `json:"app"`
	Ports        []appPort `json:"ports"`
	MAC          string    `json:"mac"`
	IP           string    `json:"ip"`
	DeviceName   string    `json:"device_name"`
	Connections  []string  `json:"connections"` // empty means all
	Src          []string  `json:"src"`
	Countries    []string  `json:"countries"`
	Enabled      bool      `json:"enabled"`
	Text         string    `json:"text"`
	Reserved     bool      `json:"reserved"`
	ReserveScope int64     `json:"reserve_scope,omitempty"`
	Blocked      string    `json:"blocked,omitempty"` // why it cannot work from outside, if so
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
			v := forwardView{Group: f.Name, ID: f.ID, Legacy: true, Ports: []appPort{{f.Proto, f.ExtPort}}, IP: f.IntAddr, Connections: conns(f.WAN),
				Src: f.Src, Countries: f.Countries, Enabled: f.Enabled}
			v.Text = fmt.Sprintf("%s: %s port %s goes to %s%s.", f.Name, strings.ToUpper(f.Proto), f.ExtPort, f.IntAddr, orWord(map[bool]string{true: ":" + f.IntPort}[f.IntPort != "" && f.IntPort != f.ExtPort], ""))
			v.Blocked = blocked(v.Connections)
			out = append(out, v)
			continue
		}
		g := groups[f.Group]
		if g == nil {
			g = &forwardView{Group: f.Group, App: f.App, MAC: f.MAC, IP: f.IntAddr, Connections: conns(f.WAN), Src: f.Src, Countries: f.Countries, Enabled: f.Enabled}
			groups[f.Group] = g
			order = append(order, f.Group)
		}
		g.Ports = append(g.Ports, appPort{f.Proto, f.ExtPort})
	}
	for _, k := range order {
		g := groups[k]
		g.DeviceName = a.fwdDeviceName(g.MAC, g.IP)
		through := "every internet connection"
		if len(g.Connections) > 0 {
			through = strings.Join(g.Connections, " and ")
		}
		var ps []string
		for _, p := range g.Ports {
			ps = append(ps, p.Port)
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
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = strings.TrimSpace(in.App)
	}
	if len(in.Name) > 24 {
		return errors.New("use a shorter name, up to 24 characters")
	}
	ports, err := a.appPorts(in.App)
	if err != nil {
		return err
	}
	mac := ""
	ip := strings.TrimSpace(in.IP)
	if m := strings.TrimSpace(in.MAC); m != "" {
		hw, err := net.ParseMAC(m)
		if err != nil || len(hw) != 6 {
			return errors.New("that is not a valid device address (MAC)")
		}
		mac = strings.ToLower(hw.String())
		if cur := a.deviceIP(mac); cur != "" {
			ip = cur
		}
	}
	if ip == "" {
		return errors.New("Gatehouse has not seen that device yet. Turn it on and connect it, then try again")
	}
	if net.ParseIP(ip) == nil {
		return errors.New("choose the device this should go to")
	}
	byName, _, _ := a.fwdWanDevices()
	devs := []string{}
	for _, c := range in.Connections {
		d, ok := byName[c]
		if !ok {
			return fmt.Errorf("%q is not one of your internet connections", c)
		}
		devs = append(devs, d)
	}
	wan := strings.Join(devs, ",")
	src, cc := cleanList(in.Src, false), cleanList(in.Countries, true)
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
		if _, err := tx.Exec(`DELETE FROM nat_forwards WHERE grp=?`, old); err != nil {
			return err
		}
		for i, p := range ports {
			name := in.Name
			if i > 0 {
				name = fmt.Sprintf("%s %d", in.Name, i+1)
			}
			if _, err := fw.SaveForward(tx, fw.ForwardRow{Name: name, WAN: wan, Proto: p.Proto, ExtPort: p.Port, IntAddr: ip, Enabled: in.Enabled,
				Group: in.Name, App: in.App, MAC: mac, Src: src, Countries: cc}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(cc) > 0 {
		a.geoEnsure()
	}
	a.markDirty()
	a.refreshVPN()
	return nil
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
		if err := a.saveAppForward(in); err != nil {
			fail(w, err)
			return
		}
		ok(w)
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

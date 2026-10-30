package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/devices"
	"gatehouse/internal/fw"
)

// The Devices page: every device with its maker, where it is connected, what network it is on, how
// much it is using, and an icon.

type station struct {
	Iface   string `json:"iface"`
	Signal  int    `json:"signal"`  // dBm
	Band    string `json:"band"`    // 2.4, 5 or 6 (GHz)
	Channel int    `json:"channel"` //
	Rate    string `json:"rate"`    // for example "866 Mb/s"
}

var (
	reStation = regexp.MustCompile(`^Station ([0-9a-f:]{17}) `)
	reSignal  = regexp.MustCompile(`^\s*signal:\s*(-?\d+)`)
	reTxRate  = regexp.MustCompile(`^\s*tx bitrate:\s*([\d.]+) MBit/s`)
	reChannel = regexp.MustCompile(`channel (\d+) \((\d+) MHz\)`)
)

// ParseStationDump reads `iw dev X station dump`: the devices connected to the card, with their
// signal and speed.
func ParseStationDump(out string) map[string]station {
	res := map[string]station{}
	var cur string
	var st station
	flush := func() {
		if cur != "" {
			res[cur] = st
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if m := reStation.FindStringSubmatch(l); m != nil {
			flush()
			cur, st = m[1], station{}
			continue
		}
		if m := reSignal.FindStringSubmatch(l); m != nil {
			st.Signal, _ = strconv.Atoi(m[1])
		} else if m := reTxRate.FindStringSubmatch(l); m != nil {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				st.Rate = fmt.Sprintf("%.0f Mb/s", f)
			}
		}
	}
	flush()
	return res
}

// ParseChannel reads the channel and band from `iw dev X info`.
func ParseChannel(out string) (band string, channel int) {
	m := reChannel.FindStringSubmatch(out)
	if m == nil {
		return "", 0
	}
	channel, _ = strconv.Atoi(m[1])
	mhz, _ := strconv.Atoi(m[2])
	switch {
	case mhz < 3000:
		band = "2.4"
	case mhz < 5925:
		band = "5"
	default:
		band = "6"
	}
	return band, channel
}

type stationCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[string]station
}

// stations lists the devices on this machine's own Wi-Fi cards. Other access points and wired
// devices are not visible this way.
func (a *App) stations() map[string]station {
	c := &a.stationCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data != nil && time.Since(c.at) < 5*time.Second {
		return c.data
	}
	out := map[string]station{}
	rows, err := a.Config.Query(`SELECT iface FROM wifi_radios WHERE mode='ap' AND enabled=1`)
	if err == nil {
		var ifs []string
		for rows.Next() {
			var n string
			rows.Scan(&n)
			ifs = append(ifs, n)
		}
		rows.Close()
		for _, n := range ifs {
			b, err := exec.Command("iw", "dev", n, "station", "dump").Output()
			if err != nil {
				continue
			}
			var band string
			var ch int
			if ib, err := exec.Command("iw", "dev", n, "info").Output(); err == nil {
				band, ch = ParseChannel(string(ib))
			}
			for mac, st := range ParseStationDump(string(b)) {
				st.Iface, st.Band, st.Channel = n, band, ch
				out[mac] = st
			}
		}
	}
	c.data, c.at = out, time.Now()
	return out
}

type deviceRow struct {
	devices.Device
	Vendor    string   `json:"vendor"`
	IconGuess string   `json:"icon_guess"`
	Source    string   `json:"source"`  // "device" or "vpn"
	Conn      string   `json:"conn"`    // wired, wifi, vpn, or empty when not known
	Port      string   `json:"port"`    // the port, card or tunnel
	Network   string   `json:"network"` // the firewall network
	Signal    int      `json:"signal,omitempty"`
	Band      string   `json:"band,omitempty"`
	Channel   int      `json:"channel,omitempty"`
	Rate      string   `json:"rate,omitempty"`
	DownBps   float64  `json:"down_bps"`
	UpBps     float64  `json:"up_bps"`
	TodayDown int64    `json:"today_down"`
	TodayUp   int64    `json:"today_up"`
	Groups    []string `json:"groups"`
	Reserved  bool     `json:"reserved"`
	Override  string   `json:"override"`         // the VLAN network this device is placed on, if any
	OverNet   string   `json:"override_network"` // that network's name
	Approval  string   `json:"approval"`         // ok, pending or blocked
}

// zoneByIface maps a port to the name of its firewall network.
func (a *App) zoneByIface() map[string]string {
	out := map[string]string{}
	if rows, err := a.Config.Query(`SELECT i.name,z.name FROM interfaces i JOIN zones z ON z.id=i.zone_id`); err == nil {
		for rows.Next() {
			var i, z string
			rows.Scan(&i, &z)
			out[i] = z
		}
		rows.Close()
	}
	return out
}

func (a *App) deviceRows() ([]deviceRow, error) {
	list, err := a.Devices.List()
	if err != nil {
		return nil, err
	}
	zones := a.zoneByIface()
	arp := arpTable()
	st := a.stations()
	rates := a.devRates()
	today := a.todayBytes()
	scopeIface := map[string]string{}
	if rows, err := a.Config.Query(`SELECT name,iface FROM dhcp_scopes`); err == nil {
		for rows.Next() {
			var n, i string
			rows.Scan(&n, &i)
			scopeIface[n] = i
		}
		rows.Close()
	}
	leaseScope := map[string]string{}
	if ls, err := a.DHCP.Leases(); err == nil {
		for _, l := range ls {
			leaseScope[strings.ToLower(l.MAC)] = l.Scope
		}
	}
	groups := map[string][]string{}
	if rows, err := a.Config.Query(`SELECT lower(m.mac),g.name FROM device_group_members m JOIN device_groups g ON g.id=m.group_id ORDER BY g.name`); err == nil {
		for rows.Next() {
			var m, g string
			rows.Scan(&m, &g)
			groups[m] = append(groups[m], g)
		}
		rows.Close()
	}
	reserved := map[string]bool{}
	if rows, err := a.Config.Query(`SELECT lower(mac) FROM dhcp_reservations`); err == nil {
		for rows.Next() {
			var m string
			rows.Scan(&m)
			reserved[m] = true
		}
		rows.Close()
	}
	ovOf, apOf := a.overrideMaps()
	out := make([]deviceRow, 0, len(list)+8)
	for _, d := range list {
		mac := strings.ToLower(d.MAC)
		r := deviceRow{Device: d, Override: ovOf[mac].Iface, OverNet: ovOf[mac].Network, Approval: apOf[mac], Source: "device", Vendor: devices.Vendor(mac), Groups: groups[mac], Reserved: reserved[mac]}
		if r.Groups == nil {
			r.Groups = []string{}
		}
		r.IconGuess = devices.GuessIcon(r.Vendor, d.Name, d.Hostname)
		iface := scopeIface[leaseScope[mac]]
		if s, ok := st[mac]; ok && d.Online {
			r.Conn, r.Port, r.Signal, r.Band, r.Channel, r.Rate = "wifi", s.Iface, s.Signal, s.Band, s.Channel, s.Rate
			if iface == "" {
				iface = s.Iface
			}
		} else if d.Online {
			if e, ok := arp[d.IP]; ok && e.MAC == mac {
				r.Conn, r.Port = "wired", e.Dev
				if iface == "" {
					iface = e.Dev
				}
			}
			if r.Conn == "" && iface != "" {
				r.Conn, r.Port = "wired", iface
			}
		}
		r.Network = zones[iface]
		if rate, ok := rates[d.IP]; ok && d.Online {
			r.DownBps, r.UpBps = rate.Down*8, rate.Up*8
		}
		if t, ok := today[mac]; ok {
			r.TodayUp, r.TodayDown = t[0], t[1]
		}
		out = append(out, r)
	}
	// People connected by VPN are not devices on a port, but they belong in the list.
	for _, v := range a.vpnSnapshot().Views {
		if v.Role != "server" || v.Kind != "wireguard" && v.Kind != "openvpn" {
			continue
		}
		for _, p := range v.Peers {
			if p.State == "revoked" {
				continue
			}
			name := p.Name
			if name == "" {
				name = "VPN device"
			}
			out = append(out, deviceRow{Device: devices.Device{ID: -p.ID, Name: name + " (" + v.Label + ")", IP: strings.Split(p.Address, "/")[0],
				Online: p.State == "connected", LastSeen: p.LastSeen, Internet: true}, Source: "vpn", Conn: "vpn", Port: v.Dev, Network: "VPN", Groups: []string{}, IconGuess: "phone"})
		}
	}
	return out, nil
}

var (
	pngMagic  = []byte("\x89PNG\r\n\x1a\n")
	jpegMagic = []byte("\xff\xd8\xff")
)

const maxIconBytes = 100 << 10

// iconMime says what kind of picture the bytes are. Only the formats that cannot carry a script.
func iconMime(b []byte) string {
	switch {
	case bytes.HasPrefix(b, pngMagic):
		return "image/png"
	case bytes.HasPrefix(b, jpegMagic):
		return "image/jpeg"
	case len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func (a *App) registerDeviceViewRoutes() {
	s := a.API
	s.Handle("GET /api/devices", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rows, err := a.deviceRows()
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"devices": rows, "icons": devices.Icons})
	})

	// Uploaded icons.
	s.HandleAdmin("POST /api/device-icons", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Name string `json:"name"`
			Data string `json:"data"` // base64
		}
		if !api.Decode(w, r, &in) {
			return
		}
		b, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil || len(b) == 0 {
			fail(w, errors.New("that file could not be read"))
			return
		}
		if len(b) > maxIconBytes {
			fail(w, errors.New("the picture is too big; keep it under 100 KB"))
			return
		}
		mime := iconMime(b)
		if mime == "" {
			fail(w, errors.New("use a PNG, JPEG or WebP picture"))
			return
		}
		name := strings.TrimSpace(in.Name)
		if len(name) > 60 {
			name = name[:60]
		}
		res, err := a.Config.Exec(`INSERT INTO device_icons(name,mime,data,created_at) VALUES(?,?,?,?)`, name, mime, b, time.Now().Unix())
		if err != nil {
			fail(w, err)
			return
		}
		id, _ := res.LastInsertId()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.Handle("GET /api/device-icons/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		var mime string
		var data []byte
		if err := a.Config.QueryRow(`SELECT mime,data FROM device_icons WHERE id=?`, id).Scan(&mime, &data); err != nil {
			api.Error(w, http.StatusNotFound, "no such picture")
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Write(data)
	})
	s.HandleAdmin("DELETE /api/device-icons/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`UPDATE devices SET icon_id=0 WHERE icon_id=?`, id)
		a.Config.Exec(`DELETE FROM device_icons WHERE id=?`, id)
		ok(w)
	})

	// The details panel: what rules mention the device, its address reservation, a day of traffic.
	s.Handle("GET /api/devices/{id}/details", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		var mac string
		if err := a.Config.QueryRow(`SELECT lower(mac) FROM devices WHERE id=?`, id).Scan(&mac); err != nil {
			api.Error(w, http.StatusNotFound, "no such device")
			return
		}
		rules := fw.RulesUsing(a.Config, "dev:"+mac)
		var groupNames []string
		if rows, err := a.Config.Query(`SELECT g.name FROM device_group_members m JOIN device_groups g ON g.id=m.group_id WHERE lower(m.mac)=?`, mac); err == nil {
			for rows.Next() {
				var n string
				rows.Scan(&n)
				groupNames = append(groupNames, n)
				for _, rn := range fw.RulesUsing(a.Config, "grp:"+n) {
					rules = append(rules, rn+" (through "+n+")")
				}
			}
			rows.Close()
		}
		sort.Strings(rules)
		res := map[string]any{"rules": append([]string{}, rules...)}
		var scope int64
		var rip string
		if a.Config.QueryRow(`SELECT scope_id,ip FROM dhcp_reservations WHERE lower(mac)=? LIMIT 1`, mac).Scan(&scope, &rip) == nil {
			res["reservation"] = map[string]any{"scope_id": scope, "ip": rip}
		}
		var pu int64
		a.Config.QueryRow(`SELECT paused_until FROM devices WHERE id=?`, id).Scan(&pu)
		res["paused_until"] = pu
		_, from, to, pts, _ := a.trafficSeries("1d", []string{mac})
		res["traffic"] = map[string]any{"from": from, "to": to, "step": 900, "points": pts}
		api.JSON(w, 200, res)
	})
	// Fix an address for a device (or take the fix away) from its details.
	s.HandleAdmin("PUT /api/devices/{id}/reservation", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		var in struct {
			Keep bool   `json:"keep"`
			IP   string `json:"ip"`
		}
		if !good || !api.Decode(w, r, &in) {
			return
		}
		var mac, name, lastIP string
		if err := a.Config.QueryRow(`SELECT lower(mac),name,last_ip FROM devices WHERE id=?`, id).Scan(&mac, &name, &lastIP); err != nil {
			api.Error(w, http.StatusNotFound, "no such device")
			return
		}
		if !in.Keep {
			a.Config.Exec(`DELETE FROM dhcp_reservations WHERE lower(mac)=?`, mac)
			a.DHCP.Reload()
			a.fwSync()
			ok(w)
			return
		}
		ip := strings.TrimSpace(in.IP)
		if ip == "" {
			ip = lastIP
		}
		var scope int64
		if scope == 0 {
			// The scope whose network holds the address.
			rows, err := a.Config.Query(`SELECT id,subnet FROM dhcp_scopes WHERE mode='serve'`)
			if err == nil {
				for rows.Next() {
					var sid int64
					var sub string
					rows.Scan(&sid, &sub)
					if subnetHas(sub, ip) {
						scope = sid
					}
				}
				rows.Close()
			}
		}
		if scope == 0 {
			fail(w, errors.New("that address is not on a network Gatehouse hands out addresses for"))
			return
		}
		if _, err := a.Config.Exec(`INSERT INTO dhcp_reservations(scope_id,mac,ip,hostname) VALUES(?,?,?,?)
			ON CONFLICT(scope_id,mac) DO UPDATE SET ip=excluded.ip`, scope, mac, ip, name); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				fail(w, errors.New("another device already has that address reserved"))
				return
			}
			fail(w, err)
			return
		}
		a.DHCP.Reload()
		a.fwSync()
		ok(w)
	})

	// Per-person view settings (columns, rows per page), kept on the machine under the person.
	s.Handle("GET /api/prefs/{key}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		key, good := prefKey(r.PathValue("key"), c)
		if !good {
			fail(w, errors.New("unknown setting"))
			return
		}
		v := a.Config.Setting(key, "{}")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(v))
	})
	s.Handle("PUT /api/prefs/{key}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		key, good := prefKey(r.PathValue("key"), c)
		if !good {
			fail(w, errors.New("unknown setting"))
			return
		}
		var v map[string]any
		if !api.Decode(w, r, &v) {
			return
		}
		b, _ := json.Marshal(v)
		if len(b) > 4096 {
			fail(w, errors.New("too much to remember"))
			return
		}
		a.Config.SetSetting(key, string(b))
		ok(w)
	})
}

func prefKey(name string, c *api.Ctx) (string, bool) {
	switch name {
	case "devices", "logs":
		return "prefs." + strconv.FormatInt(c.User.ID, 10) + "." + name, true
	}
	return "", false
}

func subnetHas(subnet, ip string) bool {
	p, err := netipParsePrefix(subnet)
	if err != nil {
		return false
	}
	a, err := netipParseAddr(ip)
	return err == nil && p.Contains(a)
}

func netipParsePrefix(s string) (netip.Prefix, error) { return netip.ParsePrefix(s) }
func netipParseAddr(s string) (netip.Addr, error)     { return netip.ParseAddr(s) }

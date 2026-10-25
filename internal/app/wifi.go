package app

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
)

// Wireless cards. Each card either joins somebody else's network (and can then be added as an
// internet connection) or broadcasts a network of its own (and can then be a LAN port, alone or
// in a bridge). The person setting up the device decides.

// WifiChannel is one channel as the driver reports it under the country in force now.
type WifiChannel struct {
	Num      int    `json:"num"`
	MHz      int    `json:"mhz"`
	Band     string `json:"band"`     // "2.4", "5" or "6"
	Disabled bool   `json:"disabled"` // not allowed at all
	NoIR     bool   `json:"no_ir"`    // may listen but not start a network ("no initiating radiation")
	Radar    bool   `json:"radar"`    // shared with weather radar: needs radar detection and waiting
}

// Usable says whether a network can be broadcast on the channel without radar detection.
func (c WifiChannel) Usable() bool { return !c.Disabled && !c.NoIR && !c.Radar }

// WifiCaps is what a card can do, read from the driver.
type WifiCaps struct {
	AP    bool     `json:"ap"`
	Bands []string `json:"bands"` // bands the card has any allowed channel in: it can join networks there
	// ApBands are the bands it can actually broadcast on right now, under the country in force.
	// A band the card lists but may not start a network on (most 5 GHz channels until a country is
	// set) is in Bands but not here.
	ApBands []string `json:"ap_bands"`
	// Listed are the bands the hardware has at all, whatever the country allows.
	Listed   []string      `json:"listed"`
	VHT      bool          `json:"vht"` // 802.11ac on 5 GHz
	Std      string        `json:"std"` // fastest standard: n, ac, ax or be
	Channels []WifiChannel `json:"channels"`
}

// ParseWifiCaps reads `iw phy <name> info`.
func ParseWifiCaps(info string) WifiCaps {
	var c WifiCaps
	inModes := false
	rank := map[string]int{"": 0, "n": 1, "ac": 2, "ax": 3, "be": 4}
	std := func(s string) {
		if rank[s] > rank[c.Std] {
			c.Std = s
		}
	}
	for _, line := range strings.Split(info, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "Supported interface modes:"):
			inModes = true
			continue
		case strings.HasPrefix(l, "Band "):
			inModes = false
		case !strings.HasPrefix(l, "*"):
			inModes = false
		}
		if inModes && l == "* AP" {
			c.AP = true
		}
		switch {
		case strings.HasPrefix(l, "HT Capabilities"):
			std("n")
		case strings.HasPrefix(l, "VHT Capabilities"):
			c.VHT = true
			std("ac")
		case strings.HasPrefix(l, "HE Iftypes"), strings.HasPrefix(l, "HE Capabilities"), strings.HasPrefix(l, "HE MAC Capabilities"):
			std("ax")
		case strings.HasPrefix(l, "EHT Iftypes"), strings.HasPrefix(l, "EHT Capabilities"), strings.HasPrefix(l, "EHT MAC Capabilities"):
			std("be")
		}
		// Frequencies tell the bands apart; a band's header does not say which it is.
		if strings.HasPrefix(l, "* ") && strings.Contains(l, " MHz [") {
			f := strings.Fields(l)
			if len(f) < 4 {
				continue
			}
			var mhz, num int
			fmt.Sscanf(f[1], "%d", &mhz)
			fmt.Sscanf(strings.Trim(f[3], "[]"), "%d", &num)
			band := ""
			switch {
			case mhz >= 2400 && mhz < 2500:
				band = "2.4"
			case mhz >= 5000 && mhz < 5900:
				band = "5"
			case mhz >= 5925 && mhz <= 7125:
				band = "6"
			}
			if band == "" {
				continue
			}
			ch := WifiChannel{Num: num, MHz: mhz, Band: band}
			if i := strings.Index(l, "dBm)"); i >= 0 {
				flags := l[i+4:]
				ch.Disabled = strings.Contains(flags, "disabled")
				ch.NoIR = strings.Contains(flags, "no IR")
				ch.Radar = strings.Contains(flags, "radar detection")
			} else {
				ch.Disabled = strings.Contains(l, "disabled")
			}
			c.Channels = append(c.Channels, ch)
			if !hasStr(c.Listed, band) {
				c.Listed = append(c.Listed, band)
			}
			if !ch.Disabled && !hasStr(c.Bands, band) {
				c.Bands = append(c.Bands, band)
			}
			// 6 GHz broadcasting is not offered yet: only 2.4 and 5 GHz networks are generated.
			if ch.Usable() && band != "6" && !hasStr(c.ApBands, band) {
				c.ApBands = append(c.ApBands, band)
			}
		}
	}
	sort.Strings(c.Bands)
	sort.Strings(c.ApBands)
	sort.Strings(c.Listed)
	return c.normal()
}

// normal makes every list a real list, so the console never meets a null.
func (c WifiCaps) normal() WifiCaps {
	if c.Bands == nil {
		c.Bands = []string{}
	}
	if c.ApBands == nil {
		c.ApBands = []string{}
	}
	if c.Listed == nil {
		c.Listed = []string{}
	}
	if c.Channels == nil {
		c.Channels = []WifiChannel{}
	}
	return c
}

// RegCountry reads the country the system applies to a card, from `iw reg get`. A card that
// manages its own regulatory rules (some Realtek and Intel cards) has its own entry.
func RegCountry(out, phy string) string {
	global, own := "", ""
	section := ""
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "global"):
			section = "global"
		case strings.HasPrefix(l, "phy#"):
			section = "phy"
			if n := strings.TrimPrefix(strings.SplitN(l, " ", 2)[0], "phy#"); phy == "phy"+n {
				section = "own"
			}
		case strings.HasPrefix(l, "country "):
			f := strings.Fields(strings.TrimSuffix(l, ":"))
			if len(f) >= 2 {
				cc := strings.TrimSuffix(f[1], ":")
				if section == "global" && global == "" {
					global = cc
				} else if section == "own" && own == "" {
					own = cc
				}
			}
		}
	}
	if own != "" {
		return own
	}
	return global
}

func hasStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// countryFromZone suggests the regulatory country for the device's time zone.
func countryFromZone(zone string) string {
	for _, f := range []string{"/usr/share/zoneinfo/zone1970.tab", "/usr/share/zoneinfo/zone.tab"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			p := strings.Split(l, "\t")
			if len(p) >= 3 && !strings.HasPrefix(l, "#") && p[2] == zone {
				return strings.SplitN(p[0], ",", 2)[0]
			}
		}
	}
	return ""
}

// WifiLive is what a card is doing right now, as opposed to what it was set up to do.
type WifiLive struct {
	Linked bool     `json:"linked"` // joined a network, or broadcasting one
	SSID   string   `json:"ssid"`
	Signal int      `json:"signal"` // dBm, joined networks only
	Addrs  []string `json:"addrs"`
	// Serves is, for a card that broadcasts, the network it carries. The address belongs to that
	// network (or to the bridge it is part of), never to the card itself.
	Serves *WifiServes `json:"serves,omitempty"`
}

// WifiServes says which network a broadcasting card carries.
type WifiServes struct {
	Name string `json:"name"`           // the port or bridge that has the address, for example br-lan
	Zone string `json:"zone,omitempty"` // the firewall network it belongs to, for example Home
	Addr string `json:"addr,omitempty"` // the router's address on it, for example 192.168.50.1/24
	Via  string `json:"via,omitempty"`  // set when the card is a member of the bridge Name
}

// wifiServes finds the network a broadcasting card is attached to: its own network, or the bridge
// it belongs to. It returns nil when the card has no network, which is a setup that needs fixing.
func wifiServes(q querier, iface string) *WifiServes {
	rows, err := q.Query(`SELECT i.name,i.kind,i.members,i.ipv4_cidr,COALESCE(z.name,'') FROM interfaces i LEFT JOIN zones z ON z.id=i.zone_id WHERE i.enabled=1 ORDER BY i.id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var via *WifiServes
	for rows.Next() {
		var name, kind, mem, cidr, zone string
		if rows.Scan(&name, &kind, &mem, &cidr, &zone) != nil {
			continue
		}
		if name == iface {
			return &WifiServes{Name: name, Zone: zone, Addr: cidr}
		}
		if kind == "bridge" {
			for _, m := range strings.Split(mem, ",") {
				if strings.TrimSpace(m) == iface {
					via = &WifiServes{Name: name, Zone: zone, Addr: cidr, Via: name}
				}
			}
		}
	}
	return via
}

// ParseIwLink reads `iw dev <name> link`.
func ParseIwLink(out string) (ssid string, signal int, ok bool) {
	if !strings.HasPrefix(strings.TrimSpace(out), "Connected to") {
		return "", 0, false
	}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "SSID:"):
			ssid = strings.TrimSpace(strings.TrimPrefix(l, "SSID:"))
		case strings.HasPrefix(l, "signal:"):
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(l, "signal:")), "%d", &signal)
		}
	}
	return ssid, signal, true
}

// ifaceAddrs lists the addresses a port has: IPv4 with its prefix, and global IPv6.
func ifaceAddrs(name string) []string {
	out := []string{}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	as, _ := ifi.Addrs()
	for _, a := range as {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil || ip.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, a.String())
	}
	return out
}

func liveWifi(name, mode string) WifiLive {
	l := WifiLive{Addrs: ifaceAddrs(name)}
	switch mode {
	case "ap":
		if b, err := exec.Command("iw", "dev", name, "info").Output(); err == nil {
			l.Linked = strings.Contains(string(b), "type AP")
		}
	default:
		if b, err := exec.Command("iw", "dev", name, "link").Output(); err == nil {
			l.SSID, l.Signal, l.Linked = ParseIwLink(string(b))
		}
	}
	return l
}

type wifiCard struct {
	Live   *WifiLive `json:"live,omitempty"`
	Iface  string    `json:"iface"`
	MAC    string    `json:"mac"`
	Driver string    `json:"driver"`
	USB    bool      `json:"usb"`
	Caps   WifiCaps  `json:"caps"`
	// Reg is the country the system applies to this card right now. The channel list is for that
	// country, and is read again after a different one is applied.
	Reg     string   `json:"reg"`
	Saved   *WifiRow `json:"saved"`
	Present bool     `json:"present"`
}

// WifiRow is a card's saved setup. The password is write-only.
type WifiRow struct {
	Iface   string `json:"iface"`
	Mode    string `json:"mode"`
	SSID    string `json:"ssid"`
	PSK     string `json:"psk,omitempty"`
	HasPSK  bool   `json:"has_psk"`
	Country string `json:"country"`
	Band    string `json:"band"`
	Channel int    `json:"channel"`
	Hidden  bool   `json:"hidden"`
	WPA3    bool   `json:"wpa3"`
	Fast    bool   `json:"fast"`
	Enabled bool   `json:"enabled"`
}

func (a *App) listWifi(q querier) ([]WifiRow, error) {
	rows, err := q.Query(`SELECT iface,mode,ssid,psk,country,band,channel,hidden,wpa3,fast,enabled FROM wifi_radios ORDER BY iface`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WifiRow{}
	for rows.Next() {
		var r WifiRow
		var psk string
		var h, w3, f, en int
		if err := rows.Scan(&r.Iface, &r.Mode, &r.SSID, &psk, &r.Country, &r.Band, &r.Channel, &h, &w3, &f, &en); err != nil {
			return nil, err
		}
		r.HasPSK = psk != ""
		r.Hidden, r.WPA3, r.Fast, r.Enabled = h == 1, w3 == 1, f == 1, en == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (a *App) addWifiToPlan(q querier, p *netd.Plan) error {
	rows, err := q.Query(`SELECT iface,mode,ssid,psk,country,band,channel,hidden,wpa3,fast,enabled FROM wifi_radios ORDER BY iface`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var w netcfg.Wifi
		var sealed string
		var h, w3, f, en int
		if err := rows.Scan(&w.Iface, &w.Mode, &w.SSID, &sealed, &w.Country, &w.Band, &w.Channel, &h, &w3, &f, &en); err != nil {
			return err
		}
		if sealed != "" {
			if w.PSK, err = a.Box.Open(sealed); err != nil {
				return fmt.Errorf("the saved Wi-Fi password for %s cannot be read; enter it again", w.Iface)
			}
		}
		w.Hidden, w.WPA3, w.Fast, w.Enabled = h == 1, w3 == 1, f == 1, en == 1
		p.Config.Wifi = append(p.Config.Wifi, w)
	}
	return rows.Err()
}

// wifiCards finds the wireless cards in the machine.
func (a *App) wifiCards() []wifiCard {
	ents, _ := os.ReadDir("/sys/class/net")
	saved, _ := a.listWifi(a.Config)
	out := []wifiCard{}
	for _, e := range ents {
		n := e.Name()
		base := "/sys/class/net/" + n
		phy, err := os.ReadFile(base + "/phy80211/name")
		if err != nil {
			continue
		}
		real, _ := filepath.EvalSymlinks(base)
		c := wifiCard{Iface: n, Present: true, USB: strings.Contains(real, "/usb"), Caps: WifiCaps{}.normal()}
		if m, err := os.ReadFile(base + "/address"); err == nil {
			c.MAC = strings.TrimSpace(string(m))
		}
		if d, err := filepath.EvalSymlinks(base + "/device/driver"); err == nil {
			c.Driver = filepath.Base(d)
		}
		if b, err := exec.Command("iw", "phy", strings.TrimSpace(string(phy)), "info").Output(); err == nil {
			c.Caps = ParseWifiCaps(string(b))
		}
		if b, err := exec.Command("iw", "reg", "get").Output(); err == nil {
			c.Reg = RegCountry(string(b), strings.TrimSpace(string(phy)))
		}
		for i := range saved {
			if saved[i].Iface == n {
				c.Saved = &saved[i]
			}
		}
		mode := "client"
		if c.Saved != nil {
			mode = c.Saved.Mode
		}
		lv := liveWifi(n, mode)
		if mode == "ap" && c.Saved != nil && c.Saved.Enabled {
			lv.Serves = wifiServes(a.Config, n)
		}
		c.Live = &lv
		out = append(out, c)
	}
	return out
}

func (a *App) registerWifiRoutes() {
	s := a.API
	s.Handle("GET /api/net/wifi", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		cards := a.wifiCards()
		zone := a.Config.Setting("system.timezone", "")
		if zone == "" {
			if b, err := os.ReadFile("/etc/timezone"); err == nil {
				zone = strings.TrimSpace(string(b))
			}
		}
		api.JSON(w, 200, map[string]any{"cards": cards, "country": countryFromZone(zone)})
	})
	s.HandleAdmin("PUT /api/net/wifi", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in WifiRow
		if !api.Decode(w, r, &in) {
			return
		}
		in.SSID = strings.TrimSpace(in.SSID)
		in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
		// A card that cannot do something is told so, instead of failing when the service starts.
		for _, card := range a.wifiCards() {
			if card.Iface != in.Iface {
				continue
			}
			if in.Mode == "ap" && card.Caps.AP == false && len(card.Caps.Bands) > 0 {
				fail(w, fmt.Errorf("this card cannot broadcast a network; use it to join one instead"))
				return
			}
			if in.Mode == "ap" && len(card.Caps.Listed) > 0 && !hasStr(card.Caps.Listed, in.Band) {
				fail(w, fmt.Errorf("this card does not support %s GHz", in.Band))
				return
			}
			// The channel list is for the country in force now. Only when the chosen country is that
			// same country can it be trusted; a new country is checked again once it is applied.
			if in.Mode == "ap" && card.Reg != "" && in.Country == card.Reg && len(card.Caps.Channels) > 0 && !hasStr(card.Caps.ApBands, in.Band) {
				fail(w, fmt.Errorf("this card may not broadcast on %s GHz in %s. Choose %s, or another band", in.Band, in.Country, otherBand(card.Caps.ApBands, in.Band)))
				return
			}
			if in.Mode == "ap" && in.Channel != 0 && card.Reg != "" && in.Country == card.Reg {
				ok := false
				for _, ch := range card.Caps.Channels {
					if ch.Num == in.Channel && ch.Band == in.Band && ch.Usable() {
						ok = true
					}
				}
				if !ok {
					fail(w, fmt.Errorf("channel %d is not available for broadcasting in %s; use Automatic or pick another channel", in.Channel, in.Country))
					return
				}
			}
			in.Fast = card.Caps.VHT && in.Band == "5"
		}
		err := a.mutateNet(func(tx *sql.Tx) error {
			var sealed string
			tx.QueryRow(`SELECT psk FROM wifi_radios WHERE iface=?`, in.Iface).Scan(&sealed)
			if in.PSK != "" {
				sealed = a.Box.Seal(in.PSK)
			}
			_, err := tx.Exec(`INSERT INTO wifi_radios(iface,mode,ssid,psk,country,band,channel,hidden,wpa3,fast,enabled) VALUES(?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(iface) DO UPDATE SET mode=excluded.mode,ssid=excluded.ssid,psk=excluded.psk,country=excluded.country,band=excluded.band,
				channel=excluded.channel,hidden=excluded.hidden,wpa3=excluded.wpa3,fast=excluded.fast,enabled=excluded.enabled`,
				in.Iface, in.Mode, in.SSID, sealed, in.Country, in.Band, in.Channel, b2i(in.Hidden), b2i(in.WPA3), b2i(in.Fast), b2i(in.Enabled))
			return err
		})
		if err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		ok(w)
	})
	s.HandleAdmin("DELETE /api/net/wifi/{iface}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		err := a.mutateNet(func(tx *sql.Tx) error {
			_, err := tx.Exec(`DELETE FROM wifi_radios WHERE iface=?`, r.PathValue("iface"))
			return err
		})
		if err != nil {
			fail(w, err)
			return
		}
		a.publishNet()
		ok(w)
	})
	s.HandleAdmin("GET /api/net/wifi/{iface}/scan", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, fmt.Errorf("the network helper is not running"))
			return
		}
		nets, err := a.Net.WifiScan(r.PathValue("iface"))
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, nets)
	})
}

// otherBand names the band to suggest when the asked one is not allowed.
func otherBand(ap []string, asked string) string {
	for _, b := range ap {
		if b != asked {
			return b + " GHz"
		}
	}
	return "another card"
}

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

// WifiCaps is what a card can do, read from the driver.
type WifiCaps struct {
	AP    bool     `json:"ap"`
	Bands []string `json:"bands"` // "2.4", "5"
	VHT   bool     `json:"vht"`   // 802.11ac on 5 GHz
}

// ParseWifiCaps reads `iw phy <name> info`.
func ParseWifiCaps(info string) WifiCaps {
	var c WifiCaps
	inModes := false
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
		if strings.HasPrefix(l, "VHT Capabilities") {
			c.VHT = true
		}
		// Frequencies tell the bands apart; a band's header does not say which it is.
		if strings.HasPrefix(l, "* ") && strings.Contains(l, " MHz [") {
			f := strings.Fields(l)
			if len(f) > 1 {
				var mhz int
				fmt.Sscanf(f[1], "%d", &mhz)
				band := ""
				switch {
				case mhz >= 2400 && mhz < 2500:
					band = "2.4"
				case mhz >= 5000 && mhz < 5900:
					band = "5"
				}
				if band != "" && !hasStr(c.Bands, band) {
					c.Bands = append(c.Bands, band)
				}
			}
		}
	}
	sort.Strings(c.Bands)
	return c
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
	Live    *WifiLive `json:"live,omitempty"`
	Iface   string    `json:"iface"`
	MAC     string    `json:"mac"`
	Driver  string    `json:"driver"`
	USB     bool      `json:"usb"`
	Caps    WifiCaps  `json:"caps"`
	Saved   *WifiRow  `json:"saved"`
	Present bool      `json:"present"`
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
		c := wifiCard{Iface: n, Present: true, USB: strings.Contains(real, "/usb")}
		if m, err := os.ReadFile(base + "/address"); err == nil {
			c.MAC = strings.TrimSpace(string(m))
		}
		if d, err := filepath.EvalSymlinks(base + "/device/driver"); err == nil {
			c.Driver = filepath.Base(d)
		}
		if b, err := exec.Command("iw", "phy", strings.TrimSpace(string(phy)), "info").Output(); err == nil {
			c.Caps = ParseWifiCaps(string(b))
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
			if in.Mode == "ap" && len(card.Caps.Bands) > 0 && !hasStr(card.Caps.Bands, in.Band) {
				fail(w, fmt.Errorf("this card does not support %s GHz", in.Band))
				return
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

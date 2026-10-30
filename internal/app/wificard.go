package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"gatehouse/internal/api"
)

// Wi-Fi join cards. A QR code that joins a Wi-Fi network when scanned, and a print-ready card with
// the name and password. Gatehouse knows the password of the Wi-Fi it broadcasts itself; for any
// other access point (UniFi, a spare one) the name and password are typed once and kept sealed,
// like the other saved passwords.

type cardSrc struct {
	Key    string `json:"key"` // radio:wlan0 or card:3
	Title  string `json:"title"`
	SSID   string `json:"ssid"`
	Hidden bool   `json:"hidden"`
	Own    bool   `json:"own"`          // broadcast by Gatehouse
	HasPSK bool   `json:"has_psk"`      // a password is stored
	Edit   int64  `json:"id,omitempty"` // for saved cards
}

// wifiEscape quotes the characters that have a meaning inside a Wi-Fi join code.
func wifiEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`)
	return r.Replace(s)
}

// wifiPayload is the standard text a phone reads from a Wi-Fi QR code.
func wifiPayload(ssid, psk string, hidden bool) string {
	t := "WPA"
	if psk == "" {
		t = "nopass"
	}
	s := "WIFI:T:" + t + ";S:" + wifiEscape(ssid) + ";"
	if psk != "" {
		s += "P:" + wifiEscape(psk) + ";"
	}
	if hidden {
		s += "H:true;"
	}
	return s + ";"
}

func (a *App) cardSources() []cardSrc {
	out := []cardSrc{}
	type row struct {
		iface, ssid, psk string
		hidden           int
	}
	var radios []row
	if rows, err := a.Config.Query(`SELECT iface,ssid,psk,hidden FROM wifi_radios WHERE mode='ap' AND enabled=1 AND ssid<>''`); err == nil {
		for rows.Next() {
			var r row
			if rows.Scan(&r.iface, &r.ssid, &r.psk, &r.hidden) == nil {
				radios = append(radios, r)
			}
		}
		rows.Close()
	}
	for _, r := range radios {
		out = append(out, cardSrc{Key: "radio:" + r.iface, Title: r.ssid, SSID: r.ssid, Hidden: r.hidden == 1, Own: true, HasPSK: r.psk != ""})
	}
	var cards []cardSrc
	if rows, err := a.Config.Query(`SELECT id,title,ssid,psk,hidden FROM wifi_cards ORDER BY id`); err == nil {
		for rows.Next() {
			var c cardSrc
			var psk string
			var h int
			if rows.Scan(&c.Edit, &c.Title, &c.SSID, &psk, &h) == nil {
				c.Key, c.Hidden, c.HasPSK = "card:"+strconv.FormatInt(c.Edit, 10), h == 1, psk != ""
				cards = append(cards, c)
			}
		}
		rows.Close()
	}
	return append(out, cards...)
}

// cardSecret returns the name and password behind a source key.
func (a *App) cardSecret(key string) (ssid, psk string, hidden bool, err error) {
	kind, id, _ := strings.Cut(key, ":")
	var sealed string
	var h int
	switch kind {
	case "radio":
		err = a.Config.QueryRow(`SELECT ssid,psk,hidden FROM wifi_radios WHERE iface=? AND mode='ap'`, id).Scan(&ssid, &sealed, &h)
	case "card":
		n, _ := strconv.ParseInt(id, 10, 64)
		err = a.Config.QueryRow(`SELECT ssid,psk,hidden FROM wifi_cards WHERE id=?`, n).Scan(&ssid, &sealed, &h)
	default:
		err = errors.New("unknown Wi-Fi network")
	}
	if err != nil {
		return "", "", false, errors.New("that Wi-Fi network was not found")
	}
	if sealed != "" {
		if psk, err = a.Box.Open(sealed); err != nil {
			return "", "", false, errors.New("the saved password cannot be read; enter it again")
		}
	}
	return ssid, psk, h == 1, nil
}

func (a *App) registerWifiCardRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/wificards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.cardSources())
	})
	s.HandleAdmin("PUT /api/wificards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			SSID   string `json:"ssid"`
			PSK    string `json:"psk"`
			Hidden bool   `json:"hidden"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.SSID = strings.TrimSpace(in.SSID)
		if in.SSID == "" || len(in.SSID) > 32 {
			fail(w, errors.New("enter the Wi-Fi name (up to 32 characters)"))
			return
		}
		if in.PSK != "" && (len(in.PSK) < 8 || len(in.PSK) > 63) {
			fail(w, errors.New("a Wi-Fi password is 8 to 63 characters"))
			return
		}
		if strings.TrimSpace(in.Title) == "" {
			in.Title = in.SSID
		}
		if in.ID == 0 {
			if _, err := a.Config.Exec(`INSERT INTO wifi_cards(title,ssid,psk,hidden) VALUES(?,?,?,?)`, in.Title, in.SSID, sealIf(a, in.PSK), b2i(in.Hidden)); err != nil {
				fail(w, err)
				return
			}
		} else {
			// An empty password on an edit keeps the saved one.
			q, args := `UPDATE wifi_cards SET title=?,ssid=?,hidden=? WHERE id=?`, []any{in.Title, in.SSID, b2i(in.Hidden), in.ID}
			if in.PSK != "" {
				q, args = `UPDATE wifi_cards SET title=?,ssid=?,hidden=?,psk=? WHERE id=?`, []any{in.Title, in.SSID, b2i(in.Hidden), a.Box.Seal(in.PSK), in.ID}
			}
			if _, err := a.Config.Exec(q, args...); err != nil {
				fail(w, err)
				return
			}
		}
		ok(w)
	})
	s.HandleAdmin("DELETE /api/wificards/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM wifi_cards WHERE id=?`, id)
		ok(w)
	})
	s.HandleAdmin("GET /api/wificards/qr", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		ssid, psk, hidden, err := a.cardSecret(r.URL.Query().Get("src"))
		if err != nil {
			fail(w, err)
			return
		}
		png, err := qrcode.Encode(wifiPayload(ssid, psk, hidden), qrcode.Medium, 400)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	})
	// The printable card. It carries the password on purpose: that is what a guest card is.
	s.HandleAdmin("GET /api/wificards/print", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		ssid, psk, hidden, err := a.cardSecret(r.URL.Query().Get("src"))
		if err != nil {
			fail(w, err)
			return
		}
		png, err := qrcode.Encode(wifiPayload(ssid, psk, hidden), qrcode.Medium, 360)
		if err != nil {
			fail(w, err)
			return
		}
		pw := "<p class=n>Open network: no password.</p>"
		if psk != "" {
			pw = "<p class=l>Password</p><p class=v>" + html.EscapeString(psk) + "</p>"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, `<!doctype html><html lang=en><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>Wi-Fi card</title>
<style>body{font:18px system-ui,sans-serif;margin:0;padding:2rem;display:flex;justify-content:center}.card{border:2px solid #222;border-radius:16px;padding:2rem 2.5rem;text-align:center;max-width:26rem}h1{margin:.2rem 0 1rem;font-size:1.6rem}.l{margin:.8rem 0 0;color:#555;font-size:.85rem;text-transform:uppercase;letter-spacing:.08em}.v{margin:.1rem 0;font-size:1.5rem;font-weight:600;word-break:break-all}img{width:15rem;height:15rem;margin-top:1rem}.n{color:#555}.s{color:#555;font-size:.9rem;margin-top:1rem}.tip{margin-top:1.5rem;color:#777;font-size:.85rem}@media print{.tip{display:none}}</style>
<div class=card><h1>Join our Wi-Fi</h1><p class=l>Network</p><p class=v>%s</p>%s<img alt="QR code to join" src="data:image/png;base64,%s"><p class=s>Point your phone camera at the code to join.</p><p class=tip>Print this page with Ctrl+P or Cmd+P.</p></div></html>`,
			html.EscapeString(ssid), pw, base64.StdEncoding.EncodeToString(png))
	})
}

func sealIf(a *App, v string) string {
	if v == "" {
		return ""
	}
	return a.Box.Seal(v)
}

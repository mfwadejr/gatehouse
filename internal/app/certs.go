package app

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"nopewarden/internal/api"
)

// Certificate countdowns: how many days each certificate NopeWarden holds has left. The levels are
// blue (a suggestion) from 60 days out, amber from 30, and red once it has expired. The console's own
// certificate renews itself each time NopeWarden starts, so it is listed only when it is inside its
// last 30 days, which means that renewal has not happened.

type certInfo struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Expires int64  `json:"expires"`
	Days    int    `json:"days"`
	Level   string `json:"level"` // "" (fine), info, warn or bad
	Advice  string `json:"advice"`
}

func certLevel(days int) string {
	switch {
	case days < 0:
		return "bad"
	case days <= 30:
		return "warn"
	case days <= 60:
		return "info"
	}
	return ""
}

func certNotAfter(p string) (time.Time, bool) {
	b, _ := pem.Decode([]byte(p))
	if b == nil {
		return time.Time{}, false
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return time.Time{}, false
	}
	return c.NotAfter, true
}

func certDays(end, now time.Time) int {
	d := end.Sub(now)
	days := int(d / (24 * time.Hour))
	if d < 0 {
		days = -int((-d + 24*time.Hour - 1) / (24 * time.Hour))
	}
	return days
}

func (a *App) certReport(now time.Time) []certInfo {
	var out []certInfo
	add := func(id, label, kind, advice string, end time.Time) {
		days := certDays(end, now)
		out = append(out, certInfo{ID: id, Label: label, Kind: kind, Expires: end.Unix(), Days: days, Level: certLevel(days), Advice: advice})
	}
	if rows, err := a.Config.Query(`SELECT id, label, name, ca_cert, srv_cert FROM vpn_ovpn WHERE mode='server'`); err == nil {
		type vrow struct {
			id        int64
			label, nm string
			ca, srv   string
		}
		var vs []vrow
		for rows.Next() {
			var v vrow
			rows.Scan(&v.id, &v.label, &v.nm, &v.ca, &v.srv)
			vs = append(vs, v)
		}
		rows.Close()
		for _, v := range vs {
			name := v.label
			if name == "" {
				name = v.nm
			}
			if end, ok := certNotAfter(v.ca); ok {
				add(fmt.Sprintf("ovpn-ca:%d", v.id), name+": certificate authority", "OpenVPN",
					"Everyone's profiles for this VPN stop working when it expires. Set the VPN up again with a new authority and give people new profiles.", end)
			}
			if end, ok := certNotAfter(v.srv); ok {
				add(fmt.Sprintf("ovpn-srv:%d", v.id), name+": server certificate", "OpenVPN",
					"People cannot connect once it expires. Set the VPN up again to make a new one.", end)
			}
			if us, err := a.Config.Query(`SELECT id, name, cert FROM vpn_ovpn_users WHERE ovpn_id=? AND revoked_at=0`, v.id); err == nil {
				for us.Next() {
					var id int64
					var un, cert string
					us.Scan(&id, &un, &cert)
					if end, ok := certNotAfter(cert); ok {
						add(fmt.Sprintf("ovpn-user:%d", id), name+": "+un, "OpenVPN user",
							"This person cannot connect once it expires. Remove them and add them again to give them a new profile.", end)
					}
				}
				us.Close()
			}
		}
	}
	if s := a.haLoad(); s != nil && s.Self.CertPEM != "" {
		if end, ok := certNotAfter(s.Self.CertPEM); ok {
			add("ha-link", "High availability link", "High availability",
				"The two machines cannot talk once it expires. Remove the pairing and pair them again.", end)
		}
	}
	if b, err := os.ReadFile(filepath.Join(a.Cfg.DataDir, "tls", "console.crt")); err == nil {
		if end, ok := certNotAfter(string(b)); ok && certDays(end, now) <= 30 {
			add("console", "NopeWarden's console", "Console",
				"It makes a new one each time NopeWarden starts, so it has not restarted in a long while. Restart NopeWarden from Settings, Power.", end)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Expires < out[j].Expires })
	return out
}

func (a *App) registerCertRoutes() {
	a.API.Handle("GET /api/certs", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"certs": a.certReport(time.Now())})
	})
}

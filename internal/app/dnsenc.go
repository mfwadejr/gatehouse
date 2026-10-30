package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"gatehouse/internal/api"
)

// Encrypted DNS as one switch for Gatehouse's own lookups (what it asks the internet when a device
// asks it). It is a front for the upstream list that already exists: choosing a mode writes that
// list, and "Custom" leaves your own list alone. The previous list is kept so the change can be
// taken back in one click.

type encUp struct {
	Name, Addr, Proto, TLS string
}

type encProvider struct {
	ID    string
	Label string
	Desc  string
	Ups   []encUp
}

var encProviders = []encProvider{
	{"cloudflare", "Cloudflare", "Fast and private.", []encUp{{"Cloudflare", "1.1.1.1:853", "tls", "cloudflare-dns.com"}, {"Cloudflare (second)", "1.0.0.1:853", "tls", "cloudflare-dns.com"}}},
	{"quad9", "Quad9", "Also blocks known malware sites.", []encUp{{"Quad9", "9.9.9.9:853", "tls", "dns.quad9.net"}, {"Quad9 (second)", "149.112.112.112:853", "tls", "dns.quad9.net"}}},
	{"google", "Google", "Fast, with Google's privacy terms.", []encUp{{"Google", "8.8.8.8:853", "tls", "dns.google"}, {"Google (second)", "8.8.4.4:853", "tls", "dns.google"}}},
	{"adguard", "AdGuard", "Also blocks ads and trackers.", []encUp{{"AdGuard", "94.140.14.14:853", "tls", "dns.adguard-dns.com"}, {"AdGuard (second)", "94.140.15.15:853", "tls", "dns.adguard-dns.com"}}},
}

var encAutomatic = []encUp{{"Cloudflare", "1.1.1.1:853", "tls", "cloudflare-dns.com"}, {"Quad9", "9.9.9.9:853", "tls", "dns.quad9.net"}}
var encPlain = []encUp{{"Cloudflare (not encrypted)", "1.1.1.1:53", "udp", ""}, {"Quad9 (not encrypted)", "9.9.9.9:53", "udp", ""}}

func encKey(l []encUp) string {
	var k []string
	for _, u := range l {
		k = append(k, u.Proto+"|"+u.Addr)
	}
	sort.Strings(k)
	b, _ := json.Marshal(k)
	return string(b)
}

func (a *App) currentUps() []encUp {
	rows, err := a.Config.Query(`SELECT name,address,proto,tls_name FROM dns_upstreams WHERE enabled=1 ORDER BY position,id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []encUp
	for rows.Next() {
		var u encUp
		if rows.Scan(&u.Name, &u.Addr, &u.Proto, &u.TLS) == nil {
			out = append(out, u)
		}
	}
	return out
}

func (a *App) encMode() string {
	cur := encKey(a.currentUps())
	switch {
	case cur == encKey(encAutomatic):
		return "automatic"
	case cur == encKey(encPlain):
		return "off"
	}
	for _, p := range encProviders {
		if cur == encKey(p.Ups) {
			return "provider:" + p.ID
		}
	}
	return "custom"
}

func (a *App) setUps(l []encUp) error {
	tx, err := a.Config.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM dns_upstreams`); err != nil {
		return err
	}
	for i, u := range l {
		if _, err := tx.Exec(`INSERT INTO dns_upstreams(name,address,proto,tls_name,position) VALUES(?,?,?,?,?)`, u.Name, u.Addr, u.Proto, u.TLS, i+1); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.loadUpstreams()
}

func (a *App) registerDNSEncRoutes() {
	s := a.API
	s.Handle("GET /api/dns/encrypted", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		provs := []map[string]string{}
		for _, p := range encProviders {
			provs = append(provs, map[string]string{"id": p.ID, "label": p.Label, "desc": p.Desc})
		}
		api.JSON(w, 200, map[string]any{"mode": a.encMode(), "providers": provs, "can_undo": a.setting("dns.upstreams_prev", "") != ""})
	})
	s.HandleAdmin("PUT /api/dns/encrypted", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Mode string `json:"mode"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		var target []encUp
		switch {
		case in.Mode == "automatic":
			target = encAutomatic
		case in.Mode == "off":
			target = encPlain
		case in.Mode == "custom":
			ok(w)
			return
		case len(in.Mode) > 9 && in.Mode[:9] == "provider:":
			for _, p := range encProviders {
				if p.ID == in.Mode[9:] {
					target = p.Ups
				}
			}
		}
		if target == nil {
			fail(w, errors.New("choose Off, Automatic, a provider or Custom"))
			return
		}
		if encKey(a.currentUps()) == encKey(target) {
			ok(w)
			return
		}
		prev, _ := json.Marshal(a.currentUps())
		if err := a.setUps(target); err != nil {
			fail(w, err)
			return
		}
		a.Config.SetSetting("dns.upstreams_prev", string(prev))
		a.event(c.User.Username, "info", "Encrypted DNS changed to "+in.Mode, "The previous list of DNS servers is kept; Undo on the DNS page puts it back.")
		ok(w)
	})
	s.HandleAdmin("POST /api/dns/encrypted/undo", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var prev []encUp
		if json.Unmarshal([]byte(a.setting("dns.upstreams_prev", "")), &prev) != nil || len(prev) == 0 {
			fail(w, errors.New("there is nothing to undo"))
			return
		}
		cur, _ := json.Marshal(a.currentUps())
		if err := a.setUps(prev); err != nil {
			fail(w, err)
			return
		}
		a.Config.SetSetting("dns.upstreams_prev", string(cur))
		ok(w)
	})
}

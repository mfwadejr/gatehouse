package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
)

// Easy remote access: a web name that always points at this router (dynamic DNS), so a phone can
// reach home with the VPN from anywhere. Two kinds of service are supported: DuckDNS, and the
// standard "dyndns2" protocol that No-IP, Dynu and many others speak. The provider reads the
// router's public address from the request itself, so Gatehouse never has to guess it.

type ddnsCfg struct {
	Provider string `json:"provider"` // "", duckdns, dyndns2
	Host     string `json:"host"`     // the web name, e.g. myhome.duckdns.org
	User     string `json:"user"`     // dyndns2 only
	Server   string `json:"server"`   // dyndns2 update server, e.g. dynupdate.no-ip.com
}

type ddnsState struct {
	mu       sync.Mutex
	lastOK   time.Time
	lastTry  time.Time
	lastErr  string
	duckBase string // test hook; empty means DuckDNS itself
	kick     chan struct{}
}

var reDDNSHost = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
var reDuckName = regexp.MustCompile(`^[a-z0-9-]{1,63}$`)

func (a *App) ddnsConfig() ddnsCfg {
	var c ddnsCfg
	if raw := a.setting("ddns.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	return c
}

func (a *App) ddnsSecret() string {
	s := a.setting("ddns.secret", "")
	if s == "" {
		return ""
	}
	v, err := a.Box.Open(s)
	if err != nil {
		return ""
	}
	return v
}

// ddnsUpdate asks the service to point the name at this router's current public address.
func (a *App) ddnsUpdate(ctx context.Context) error {
	c := a.ddnsConfig()
	secret := a.ddnsSecret()
	if c.Provider == "" || c.Host == "" || secret == "" {
		return errors.New("not set up")
	}
	var req *http.Request
	var err error
	switch c.Provider {
	case "duckdns":
		base := "https://www.duckdns.org"
		a.ddns.mu.Lock()
		if a.ddns.duckBase != "" {
			base = a.ddns.duckBase
		}
		a.ddns.mu.Unlock()
		name := strings.TrimSuffix(c.Host, ".duckdns.org")
		u := base + "/update?domains=" + url.QueryEscape(name) + "&token=" + url.QueryEscape(secret) + "&ip="
		req, err = http.NewRequestWithContext(ctx, "GET", u, nil)
	case "dyndns2":
		u := "https://" + c.Server + "/nic/update?hostname=" + url.QueryEscape(c.Host)
		req, err = http.NewRequestWithContext(ctx, "GET", u, nil)
		if err == nil {
			req.SetBasicAuth(c.User, secret)
			req.Header.Set("User-Agent", "Gatehouse")
		}
	default:
		return errors.New("unknown service")
	}
	if err != nil {
		return err
	}
	cl := &http.Client{Timeout: 15 * time.Second}
	resp, err := cl.Do(req)
	var out string
	if err == nil {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		out = strings.TrimSpace(string(b))
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			err = errors.New("the service refused the sign-in; check the user name and password or token")
		} else if resp.StatusCode >= 400 {
			err = fmt.Errorf("the service answered %d", resp.StatusCode)
		}
	}
	if err == nil {
		low := strings.ToLower(out)
		okRes := strings.HasPrefix(low, "ok") || strings.HasPrefix(low, "good") || strings.HasPrefix(low, "nochg")
		if !okRes {
			err = fmt.Errorf("the service said %q; check the name and token", out)
		}
	}
	a.ddns.mu.Lock()
	a.ddns.lastTry = time.Now()
	if err != nil {
		a.ddns.lastErr = err.Error()
	} else {
		a.ddns.lastOK, a.ddns.lastErr = time.Now(), ""
	}
	a.ddns.mu.Unlock()
	return err
}

func (a *App) ddnsLoop(ctx context.Context) {
	a.ddns.kick = make(chan struct{}, 1)
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.ddns.kick:
		}
		if a.ddnsConfig().Provider != "" {
			cx, cancel := context.WithTimeout(ctx, 30*time.Second)
			a.ddnsUpdate(cx)
			cancel()
		}
	}
}

func (a *App) registerDDNSRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/remote", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		cfg := a.ddnsConfig()
		a.ddns.mu.Lock()
		st := map[string]any{"last_ok": a.ddns.lastOK.Unix(), "error": a.ddns.lastErr}
		a.ddns.mu.Unlock()
		if a.ddns.lastOK.IsZero() {
			st["last_ok"] = 0
		}
		var servers int
		a.Config.QueryRow(`SELECT count(*) FROM vpn_wg WHERE mode='server' AND enabled=1 AND site=0`).Scan(&servers)
		blocked := []string{}
		for _, l := range a.natReport() {
			if l.Blocked {
				blocked = append(blocked, l.Name)
			}
		}
		api.JSON(w, 200, map[string]any{"config": cfg, "has_secret": a.ddnsSecret() != "", "status": st,
			"vpn_server": servers > 0, "endpoint": a.setting(vpnEndpointSetting, ""), "blocked": blocked})
	})
	s.HandleAdmin("PUT /api/remote", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			ddnsCfg
			Secret string `json:"secret"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Host = strings.ToLower(strings.TrimSpace(in.Host))
		in.Server = strings.ToLower(strings.TrimSpace(in.Server))
		switch in.Provider {
		case "":
			a.Config.SetSetting("ddns.config", "")
			a.Config.SetSetting("ddns.secret", "")
			ok(w)
			return
		case "duckdns":
			name := strings.TrimSuffix(in.Host, ".duckdns.org")
			if !reDuckName.MatchString(name) {
				fail(w, errors.New("enter your DuckDNS name, like myhome or myhome.duckdns.org"))
				return
			}
			in.Host, in.User, in.Server = name+".duckdns.org", "", ""
		case "dyndns2":
			if !reDDNSHost.MatchString(in.Host) {
				fail(w, errors.New("enter the full web name, like myhome.ddns.net"))
				return
			}
			if !reDDNSHost.MatchString(in.Server) || net.ParseIP(in.Server) != nil {
				fail(w, errors.New("enter the update server name your service gave you, like dynupdate.no-ip.com"))
				return
			}
			if strings.TrimSpace(in.User) == "" {
				fail(w, errors.New("enter your user name for the service"))
				return
			}
		default:
			fail(w, errors.New("choose DuckDNS or another service"))
			return
		}
		if in.Secret == "" && a.ddnsSecret() == "" {
			fail(w, errors.New("enter the token (DuckDNS) or password"))
			return
		}
		b, _ := json.Marshal(in.ddnsCfg)
		a.Config.SetSetting("ddns.config", string(b))
		if in.Secret != "" {
			a.Config.SetSetting("ddns.secret", a.Box.Seal(in.Secret))
		}
		// Devices reach home through this name, so it becomes the VPN's address too.
		a.Config.SetSetting(vpnEndpointSetting, in.Host)
		a.refreshVPN()
		if err := a.ddnsUpdate(r.Context()); err != nil {
			api.JSON(w, 200, map[string]any{"status": "saved", "warning": "Saved, but the first update failed: " + err.Error()})
			return
		}
		api.JSON(w, 200, map[string]any{"status": "ok"})
	})
}

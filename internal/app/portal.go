package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// Guest sign-in page (captive portal). On the networks it is switched on for, a device that has not
// accepted the page can only reach the page (and DNS, so names resolve); the firewall redirects its
// web requests here and blocks everything else until it accepts. Acceptance lasts a set number of
// hours. It never applies to a management network, an internet port or the setup port.

type portalCfg struct {
	On     bool     `json:"on"`
	Ifaces []string `json:"ifaces"`
	Title  string   `json:"title"`
	Terms  string   `json:"terms"`
	Hours  int      `json:"hours"`
}

type portalState struct {
	mu       sync.Mutex
	srv      *http.Server
	ln       net.Listener
	sessions map[string]int64 // "mac:aa:bb:.." or "ip:1.2.3.4" -> unix expiry
	loaded   bool
	last     string
}

func (a *App) portalConfig() portalCfg {
	c := portalCfg{Title: "Welcome", Hours: 8}
	if raw := a.setting("portal.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.Hours < 1 || c.Hours > 720 {
		c.Hours = 8
	}
	return c
}

func (a *App) portalLoad() {
	p := &a.portal
	if p.loaded {
		return
	}
	p.loaded = true
	p.sessions = map[string]int64{}
	if raw := a.setting("portal.sessions", ""); raw != "" {
		json.Unmarshal([]byte(raw), &p.sessions)
	}
}

// portalSave drops sessions that have ended and keeps the rest across restarts. Call with mu held.
func (a *App) portalSave() {
	now := time.Now().Unix()
	for k, exp := range a.portal.sessions {
		if exp <= now {
			delete(a.portal.sessions, k)
		}
	}
	b, _ := json.Marshal(a.portal.sessions)
	a.Config.SetSetting("portal.sessions", string(b))
}

// portalNets maps each switched-on network to its address range, using only the networks the
// firewall will really cover.
func (a *App) portalPrefixes(c portalCfg) []netip.Prefix {
	var out []netip.Prefix
	for _, n := range c.Ifaces {
		var cidr string
		if a.Config.QueryRow(`SELECT ipv4_cidr FROM interfaces WHERE name=? AND zone_id IS NOT NULL`, n).Scan(&cidr) != nil {
			continue
		}
		if p, err := netip.ParsePrefix(cidr); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}

func (a *App) portalMACFor(ip string) string {
	ls, err := a.DHCP.Leases()
	if err != nil {
		return ""
	}
	for _, l := range ls {
		if l.IP == ip && l.MAC != "" {
			return strings.ToLower(l.MAC)
		}
	}
	return ""
}

// portalAccept signs a device in. The address must belong to a network the page covers.
func (a *App) portalAccept(ipStr string) error {
	c := a.portalConfig()
	if !c.On {
		return errors.New("the sign-in page is off")
	}
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return errors.New("bad address")
	}
	inside := false
	for _, p := range a.portalPrefixes(c) {
		inside = inside || p.Contains(ip)
	}
	if !inside {
		return errors.New("this device is not on a network that uses the sign-in page")
	}
	key := "ip:" + ip.String()
	if mac := a.portalMACFor(ip.String()); mac != "" {
		key = "mac:" + mac
	}
	a.portal.mu.Lock()
	a.portalLoad()
	a.portal.sessions[key] = time.Now().Add(time.Duration(c.Hours) * time.Hour).Unix()
	a.portalSave()
	a.portal.mu.Unlock()
	a.fwSync()
	return nil
}

// portalFill is what the firewall's signed-in set should hold now: the addresses of every device
// whose sign-in has not ended, each with the time it has left.
func (a *App) portalFill(leases map[string][]string) fw.SetFill {
	a.portal.mu.Lock()
	defer a.portal.mu.Unlock()
	a.portalLoad()
	now := time.Now().Unix()
	seen := map[string]bool{}
	f := fw.SetFill{Replace: true}
	keys := make([]string, 0, len(a.portal.sessions))
	for k := range a.portal.sessions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		left := a.portal.sessions[k] - now
		if left < 1 {
			continue
		}
		var ips []string
		switch {
		case strings.HasPrefix(k, "mac:"):
			ips = leases[strings.TrimPrefix(k, "mac:")]
		case strings.HasPrefix(k, "ip:"):
			ips = []string{strings.TrimPrefix(k, "ip:")}
		}
		for _, ip := range ips {
			if addr, err := netip.ParseAddr(ip); err != nil || !addr.Is4() || seen[ip] {
				continue
			}
			seen[ip] = true
			if left > 9999999 {
				left = 9999999
			}
			f.Elems = append(f.Elems, fmt.Sprintf("%s timeout %ds", ip, left))
		}
	}
	return f
}

const portalPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{font:17px/1.5 -apple-system,system-ui,Segoe UI,Roboto,sans-serif;margin:0;background:#f4f5f7;color:#1c1f23}
main{max-width:30rem;margin:8vh auto;padding:0 1.25rem}.card{background:#fff;border-radius:14px;padding:1.5rem;box-shadow:0 2px 12px rgba(0,0,0,.08)}
h1{font-size:1.5rem;margin:.2rem 0 1rem}.terms{max-height:14rem;overflow:auto;font-size:.9rem;color:#4a5058;border:1px solid #e1e4e8;border-radius:8px;padding:.6rem .8rem;margin:0 0 1rem}
button{width:100%%;font:inherit;font-weight:600;padding:.8rem;border:0;border-radius:10px;background:#1f6feb;color:#fff}p{margin:.5rem 0}</style></head>
<body><main><div class="card"><h1>%s</h1>%s<form method="post" action="/accept"><button type="submit">%s</button></form></div></main></body></html>`

const portalDone = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Connected</title>
<style>body{font:17px/1.5 -apple-system,system-ui,sans-serif;margin:0;background:#f4f5f7;color:#1c1f23}main{max-width:30rem;margin:8vh auto;padding:0 1.25rem}.card{background:#fff;border-radius:14px;padding:1.5rem;box-shadow:0 2px 12px rgba(0,0,0,.08)}</style></head>
<body><main><div class="card"><h1>You are connected</h1><p>You can close this page and use the internet.</p></div></main></body></html>`

func portalBody(c portalCfg) string {
	title := html.EscapeString(strings.TrimSpace(c.Title))
	if title == "" {
		title = "Welcome"
	}
	var terms, btn string
	if t := strings.TrimSpace(c.Terms); t != "" {
		var b strings.Builder
		for _, para := range strings.Split(strings.ReplaceAll(t, "\r", ""), "\n\n") {
			b.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(strings.TrimSpace(para)), "\n", "<br>") + "</p>")
		}
		terms = `<div class="terms">` + b.String() + `</div>`
		btn = "I agree and connect"
	} else {
		btn = "Connect"
	}
	return fmt.Sprintf(portalPage, title, title, terms, btn)
}

func (a *App) portalHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if r.Method == http.MethodPost && r.URL.Path == "/accept" {
			if err := a.portalAccept(host); err != nil {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, "<!doctype html><meta charset=utf-8><p>"+html.EscapeString(err.Error()))
				return
			}
			fmt.Fprint(w, portalDone)
			return
		}
		// Every other address gets the page itself, which is what phones look for.
		fmt.Fprint(w, portalBody(a.portalConfig()))
	})
}

// portalSync starts or stops the page's listener to match the setting.
func (a *App) portalSync() error {
	c := a.portalConfig()
	a.portal.mu.Lock()
	defer a.portal.mu.Unlock()
	a.portalLoad()
	if a.portal.srv != nil {
		a.portal.srv.Close()
		a.portal.srv, a.portal.ln = nil, nil
	}
	if !c.On {
		return nil
	}
	addr := a.Cfg.PortalAddr
	if addr == "" {
		addr = fmt.Sprintf(":%d", fw.PortalPort)
	}
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: a.portalHandler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second}
	a.portal.srv, a.portal.ln = srv, ln
	go srv.Serve(ln)
	return nil
}

func (a *App) portalLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}
	if err := a.portalSync(); err != nil {
		a.portal.mu.Lock()
		a.portal.last = err.Error()
		a.portal.mu.Unlock()
	}
	<-ctx.Done()
	a.portal.mu.Lock()
	if a.portal.srv != nil {
		a.portal.srv.Close()
	}
	a.portal.mu.Unlock()
}

type portalNet struct {
	Iface   string `json:"iface"`
	Name    string `json:"name"`
	CIDR    string `json:"cidr"`
	Allowed bool   `json:"allowed"`
	Why     string `json:"why,omitempty"`
}

// portalChoices lists the networks the person can pick, saying why one cannot be used.
func (a *App) portalChoices() []portalNet {
	m, err := fw.Load(a.Config)
	if err != nil {
		return nil
	}
	rows, err := a.Config.Query(`SELECT i.name,COALESCE(z.name,i.name),i.ipv4_cidr FROM interfaces i JOIN zones z ON z.id=i.zone_id WHERE i.ipv4_cidr<>'' AND i.enabled=1 ORDER BY i.name`)
	if err != nil {
		return nil
	}
	var out []portalNet
	for rows.Next() {
		var n portalNet
		if rows.Scan(&n.Iface, &n.Name, &n.CIDR) == nil {
			out = append(out, n)
		}
	}
	rows.Close()
	ok := map[string]bool{}
	probe := *m
	for i := range out {
		probe.PortalIfaces = []string{out[i].Iface}
		ok[out[i].Iface] = len(probe.PortalNets()) == 1
	}
	for i := range out {
		out[i].Allowed = ok[out[i].Iface]
		if !out[i].Allowed {
			out[i].Why = "This network manages Gatehouse or faces the internet, so it can never be put behind a sign-in page."
		}
	}
	return out
}

func (a *App) registerPortalRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/portal", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.portal.mu.Lock()
		a.portalLoad()
		now := time.Now().Unix()
		signed := 0
		for _, exp := range a.portal.sessions {
			if exp > now {
				signed++
			}
		}
		listening, last := a.portal.srv != nil, a.portal.last
		a.portal.mu.Unlock()
		api.JSON(w, 200, map[string]any{"config": a.portalConfig(), "networks": a.portalChoices(), "signed_in": signed, "listening": listening, "error": last})
	})
	s.HandleAdmin("PUT /api/portal", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in portalCfg
		if !api.Decode(w, r, &in) {
			return
		}
		in.Title, in.Terms = strings.TrimSpace(in.Title), strings.TrimSpace(in.Terms)
		if len(in.Title) > 80 || len(in.Terms) > 8000 {
			fail(w, errors.New("the title can be up to 80 letters and the terms up to 8000"))
			return
		}
		if in.Hours < 1 || in.Hours > 720 {
			fail(w, errors.New("choose how long a sign-in lasts: 1 to 720 hours"))
			return
		}
		okNets := map[string]bool{}
		for _, n := range a.portalChoices() {
			if n.Allowed {
				okNets[n.Iface] = true
			}
		}
		var keep []string
		for _, d := range in.Ifaces {
			if !okNets[d] {
				fail(w, errors.New(d+" cannot be put behind a sign-in page (it manages Gatehouse, faces the internet, or does not exist)"))
				return
			}
			keep = append(keep, d)
		}
		in.Ifaces = keep
		if in.On && len(keep) == 0 {
			fail(w, errors.New("choose at least one guest network"))
			return
		}
		old := a.setting("portal.config", "")
		b, _ := json.Marshal(in)
		a.Config.SetSetting("portal.config", string(b))
		revert := func() {
			a.Config.SetSetting("portal.config", old)
			a.portalSync()
		}
		if err := a.portalSync(); err != nil {
			revert()
			fail(w, errors.New("the sign-in page could not start: "+err.Error()))
			return
		}
		a.portal.mu.Lock()
		a.portal.last = ""
		a.portal.mu.Unlock()
		if err := a.refreshFirewallQuiet(c.User.Username); err != nil {
			revert()
			a.refreshFirewallQuiet(c.User.Username)
			fail(w, errors.New("the firewall could not be updated: "+err.Error()))
			return
		}
		a.fwSync()
		a.event(c.User.Username, "info", "The guest sign-in page settings were changed", "")
		a.publishFW()
		ok(w)
	})
	s.HandleAdmin("POST /api/portal/clear", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.portal.mu.Lock()
		a.portalLoad()
		a.portal.sessions = map[string]int64{}
		a.portalSave()
		a.portal.mu.Unlock()
		a.fwSync()
		ok(w)
	})
}

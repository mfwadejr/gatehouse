package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
	"gatehouse/internal/upnp"
)

// The console's port can be changed from the console itself. The new port is opened next to the
// old one and the firewall allows both. Nothing is final until someone has reached the console on
// the new port and confirmed; if nobody does, the new port is closed again and the old one never
// stopped working.

const (
	consolePortSetting = "console.port"
	pendingPortSetting = "console.port.pending"
	portTrial          = 120 * time.Second
)

type consoleSrv struct {
	port int
	srv  *http.Server
	done chan struct{}
}

type consoles struct {
	mu      sync.Mutex
	ctx     context.Context
	srvs    map[int]*consoleSrv
	timer   *time.Timer
	primary int // the port the console started on, and the one confirmed since
}

func (c *consoles) begin(ctx context.Context) {
	c.mu.Lock()
	c.ctx = ctx
	c.srvs = map[int]*consoleSrv{}
	c.mu.Unlock()
}

func (c *consoles) add(s *consoleSrv) {
	c.mu.Lock()
	c.srvs[s.port] = s
	c.mu.Unlock()
}

func (c *consoles) take(port int) *consoleSrv {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.srvs[port]
	delete(c.srvs, port)
	return s
}

func (c *consoles) waitAll() {
	c.mu.Lock()
	var all []*consoleSrv
	for _, s := range c.srvs {
		all = append(all, s)
	}
	c.mu.Unlock()
	for _, s := range all {
		<-s.done
	}
}

func (s *consoleSrv) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
	<-s.done
}

// consoleAddr is where the console listens. The default address follows the saved port; an
// address given explicitly (tests, a custom start-up) is used as it is.
func (a *App) consoleAddr() string {
	if a.Cfg.WebAddr == DefaultConfig().WebAddr {
		return ":" + strconv.Itoa(a.savedPort())
	}
	return a.Cfg.WebAddr
}

// currentPort is the port the console is really answering on, apart from any trial.
func (a *App) currentPort() int {
	a.con.mu.Lock()
	defer a.con.mu.Unlock()
	if a.con.primary > 0 {
		return a.con.primary
	}
	return a.savedPort()
}

func (a *App) savedPort() int {
	if n, err := strconv.Atoi(strings.TrimSpace(a.Config.Setting(consolePortSetting, ""))); err == nil && n >= 1 && n <= 65535 {
		return n
	}
	return 443
}

func (a *App) startConsole(ctx context.Context, addr string) (*consoleSrv, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	_, ps, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(ps)
	srv := &http.Server{Handler: a.API.Handler(), ConnContext: api.ConnContext, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	cs := &consoleSrv{port: port, srv: srv, done: make(chan struct{})}
	log.Printf("console listening on %s", l.Addr())
	go func() {
		defer close(cs.done)
		if err := api.Serve(ctx, srv, l, a.Cfg.TLSCert, a.Cfg.TLSKey); err != nil {
			log.Printf("console on %s stopped: %v", addr, err)
		}
	}()
	return cs, nil
}

// startupPort forgets any trial that was running when the program stopped and keeps the copy of
// the saved port that the machine's screens and the starting firewall read.
func (a *App) startupPort() {
	a.Config.Exec(`DELETE FROM settings WHERE key=?`, pendingPortSetting)
	a.writePortFile(a.savedPort())
}

func (a *App) writePortFile(port int) {
	os.WriteFile(filepath.Join(a.Cfg.DataDir, "console.port"), []byte(strconv.Itoa(port)+"\n"), 0o644)
}

// consolePorts are the ports the console answers on right now.
func (a *App) consolePorts() []int { return fw.ConsolePortsFrom(a.Config, time.Now()) }

func localTCPPort(r *http.Request) int {
	if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if _, ps, err := net.SplitHostPort(la.String()); err == nil {
			n, _ := strconv.Atoi(ps)
			return n
		}
	}
	return 0
}

var reservedPorts = map[int]string{
	22: "remote login (SSH)", 53: "DNS", 67: "DHCP", 68: "DHCP", 123: "the clock (NTP)", 853: "secure DNS",
	upnp.ControlPort: "UPnP",
}

// event records something worth keeping in the Gatehouse log.
func (a *App) event(actor, sev, title, detail string) {
	if a.Logs != nil {
		a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, time.Now().Unix(), "system", sev, actor, title, detail)
	}
}

// portInUse says what already uses a port, or "" when it is free to choose.
func (a *App) portInUse(port int) string {
	if why, ok := reservedPorts[port]; ok {
		return why
	}
	for _, q := range []struct{ sql, what string }{
		{`SELECT listen_port FROM vpn_wg`, "a VPN"}, {`SELECT port FROM vpn_ovpn`, "a VPN"},
	} {
		if rows, err := a.Config.Query(q.sql); err == nil {
			for rows.Next() {
				var n int
				rows.Scan(&n)
				if n == port {
					rows.Close()
					return q.what
				}
			}
			rows.Close()
		}
	}
	if rows, err := a.Config.Query(`SELECT name,ext_port FROM nat_forwards`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, ext string
			rows.Scan(&name, &ext)
			lo, hi, _ := strings.Cut(strings.TrimSpace(ext), "-")
			l, e1 := strconv.Atoi(lo)
			h := l
			if hi != "" {
				h, _ = strconv.Atoi(hi)
			}
			if e1 == nil && port >= l && port <= h {
				return "the port forward " + name
			}
		}
	}
	return ""
}

func (a *App) pendingPort() (port int, deadline int64, ok bool) {
	p, d, found := strings.Cut(a.Config.Setting(pendingPortSetting, ""), "|")
	if !found {
		return 0, 0, false
	}
	pn, e1 := strconv.Atoi(p)
	dl, e2 := strconv.ParseInt(d, 10, 64)
	if e1 != nil || e2 != nil || time.Now().Unix() >= dl {
		return 0, 0, false
	}
	return pn, dl, true
}

func (a *App) refreshFirewallQuiet(author string) error {
	err := a.applyFirewall(author)
	if errors.Is(err, errNoHelper) {
		return nil
	}
	return err
}

// dropTrial closes a port that was being tried and removes it from the firewall.
func (a *App) dropTrial(author string) {
	a.con.mu.Lock()
	if a.con.timer != nil {
		a.con.timer.Stop()
		a.con.timer = nil
	}
	a.con.mu.Unlock()
	if port, _, ok := strings.Cut(a.Config.Setting(pendingPortSetting, ""), "|"); ok {
		a.Config.Exec(`DELETE FROM settings WHERE key=?`, pendingPortSetting)
		if n, err := strconv.Atoi(port); err == nil && n != a.currentPort() {
			if s := a.con.take(n); s != nil {
				go s.stop()
			}
		}
		if err := a.refreshFirewallQuiet(author); err != nil {
			log.Printf("console port: firewall refresh: %v", err)
		}
	}
}

func (a *App) registerPortRoutes() {
	s := a.API
	s.Handle("GET /api/system/console-port", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		out := map[string]any{"port": a.currentPort(), "here": localTCPPort(r)}
		if p, dl, ok := a.pendingPort(); ok {
			out["pending"] = map[string]any{"port": p, "deadline": dl}
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("POST /api/system/console-port", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Port int `json:"port"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Port < 1 || in.Port > 65535 {
			fail(w, errors.New("enter a port number from 1 to 65535"))
			return
		}
		if in.Port == a.currentPort() {
			fail(w, errors.New("the console already uses that port"))
			return
		}
		if why := a.portInUse(in.Port); why != "" {
			fail(w, fmt.Errorf("port %d is used by %s; choose another", in.Port, why))
			return
		}
		a.dropTrial(c.User.Username)
		a.con.mu.Lock()
		ctx := a.con.ctx
		a.con.mu.Unlock()
		if ctx == nil {
			fail(w, errors.New("the console is not running"))
			return
		}
		cs, err := a.startConsole(ctx, ":"+strconv.Itoa(in.Port))
		if err != nil {
			fail(w, fmt.Errorf("port %d is already in use on this machine; choose another", in.Port))
			return
		}
		a.con.add(cs)
		deadline := time.Now().Add(portTrial)
		a.Config.SetSetting(pendingPortSetting, fmt.Sprintf("%d|%d", in.Port, deadline.Unix()))
		if err := a.refreshFirewallQuiet(c.User.Username); err != nil {
			a.dropTrial(c.User.Username)
			fail(w, fmt.Errorf("the firewall would not open the new port: %v", err))
			return
		}
		a.con.mu.Lock()
		a.con.timer = time.AfterFunc(portTrial+2*time.Second, func() { a.dropTrial("system") })
		a.con.mu.Unlock()
		a.event("system", "info", "Trying a new console port", fmt.Sprintf("Port %d is open next to %d until confirmed.", in.Port, a.currentPort()))
		api.JSON(w, 200, map[string]any{"port": in.Port, "seconds": int(portTrial / time.Second), "deadline": deadline.Unix()})
	})
	s.HandleAdmin("POST /api/system/console-port/confirm", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		p, _, ok := a.pendingPort()
		if !ok {
			api.Error(w, http.StatusConflict, "there is no port change waiting, or the time ran out")
			return
		}
		if localTCPPort(r) != p {
			api.Error(w, http.StatusConflict, fmt.Sprintf("confirm from the new address, on port %d; that proves it works", p))
			return
		}
		old := a.currentPort()
		a.con.mu.Lock()
		a.con.primary = p
		if a.con.timer != nil {
			a.con.timer.Stop()
			a.con.timer = nil
		}
		a.con.mu.Unlock()
		a.Config.SetSetting(consolePortSetting, strconv.Itoa(p))
		a.Config.Exec(`DELETE FROM settings WHERE key=?`, pendingPortSetting)
		a.writePortFile(p)
		if err := a.refreshFirewallQuiet(c.User.Username); err != nil {
			log.Printf("console port: firewall refresh: %v", err)
		}
		a.event("system", "info", "Console port changed", fmt.Sprintf("The console now answers on port %d instead of %d.", p, old))
		if s := a.con.take(old); s != nil {
			go func() { time.Sleep(1500 * time.Millisecond); s.stop() }()
		}
		ok2(w)
	})
	s.HandleAdmin("POST /api/system/console-port/cancel", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.dropTrial(c.User.Username)
		ok2(w)
	})
}

func ok2(w http.ResponseWriter) { api.JSON(w, 200, map[string]bool{"ok": true}) }

// LogEvent records something worth keeping in the Gatehouse log (used by the watchdog).
func (a *App) LogEvent(actor, sev, title, detail string) { a.event(actor, sev, title, detail) }

func regexpMust(s string) *regexp.Regexp { return regexp.MustCompile(s) }

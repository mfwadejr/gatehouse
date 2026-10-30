package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
)

// Send logs elsewhere: Gatehouse's own events, and optionally every firewall decision, to a syslog
// server on your network. Off by default. Messages are queued and dropped when the queue is full,
// so a dead log server can never slow the router down. Flow export (NetFlow/IPFIX) and SNMP are not
// part of this.

type syslogCfg struct {
	On       bool   `json:"on"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Proto    string `json:"proto"` // udp or tcp
	Firewall bool   `json:"firewall"`
}

type syslogOut struct {
	mu    sync.Mutex
	q     chan string
	cfg   syslogCfg
	conn  net.Conn
	sent  uint64
	drops uint64
	last  string // last error, plain
}

func (s *syslogOut) init() {
	if s.q == nil {
		s.q = make(chan string, 512)
		go s.run()
	}
}

func (a *App) syslogConfig() syslogCfg {
	c := syslogCfg{Port: 514, Proto: "udp"}
	if raw := a.setting("syslog.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	return c
}

// syslogLine builds an RFC 5424 message, facility local0.
func syslogLine(sev, host, msg string, at time.Time) string {
	n := map[string]int{"error": 3, "warning": 4, "info": 6, "debug": 7}[sev]
	if n == 0 {
		n = 6
	}
	if host == "" {
		host = "-"
	}
	msg = strings.NewReplacer("\r", " ", "\n", " ").Replace(msg)
	return fmt.Sprintf("<%d>1 %s %s gatehouse - - - %s", 16*8+n, at.UTC().Format(time.RFC3339), host, msg)
}

// send queues a line; it never blocks.
func (a *App) syslogSend(sev, msg string) {
	c := a.sysout.cfgNow()
	if !c.On || c.Host == "" {
		return
	}
	a.sysout.init()
	host, _ := a.hostnameCached()
	select {
	case a.sysout.q <- syslogLine(sev, host, msg, time.Now()):
	default:
		a.sysout.mu.Lock()
		a.sysout.drops++
		a.sysout.mu.Unlock()
	}
}

func (s *syslogOut) cfgNow() syslogCfg {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *syslogOut) run() {
	for line := range s.q {
		c := s.cfgNow()
		if !c.On || c.Host == "" {
			continue
		}
		if s.conn == nil {
			cn, err := net.DialTimeout(c.Proto, net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), 3*time.Second)
			if err != nil {
				s.mu.Lock()
				s.last, s.drops = "Cannot reach "+c.Host+": "+err.Error(), s.drops+1
				s.mu.Unlock()
				continue
			}
			s.conn = cn
		}
		s.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		out := line
		if c.Proto == "tcp" {
			out += "\n"
		}
		if _, err := s.conn.Write([]byte(out)); err != nil {
			s.conn.Close()
			s.conn = nil
			s.mu.Lock()
			s.last, s.drops = "Sending failed: "+err.Error(), s.drops+1
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		s.sent++
		s.last = ""
		s.mu.Unlock()
	}
}

func (s *syslogOut) reset(c syslogCfg) {
	s.mu.Lock()
	s.cfg = c
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	s.mu.Unlock()
}

// syslogFW sends firewall decisions when the person asked for them.
func (a *App) syslogFW(evs []netd.FWEvent) {
	if !a.sysout.cfgNow().Firewall {
		return
	}
	for _, e := range evs {
		a.syslogSend("info", fmt.Sprintf("firewall %s %s %s:%d -> %s:%d rule=%s", e.Verdict, e.Proto, e.Src, e.SPort, e.Dst, e.DPort, e.Tag))
	}
}

func (a *App) hostnameCached() (string, error) {
	h := a.setting("system.hostname", "")
	if h == "" {
		h = "gatehouse"
	}
	return h, nil
}

func (a *App) loadSyslog() { a.sysout.reset(a.syslogConfig()) }

func (a *App) registerSyslogRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/syslog", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.sysout.mu.Lock()
		st := map[string]any{"sent": a.sysout.sent, "dropped": a.sysout.drops, "error": a.sysout.last}
		a.sysout.mu.Unlock()
		api.JSON(w, 200, map[string]any{"config": a.syslogConfig(), "status": st})
	})
	s.HandleAdmin("PUT /api/syslog", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in syslogCfg
		if !api.Decode(w, r, &in) {
			return
		}
		in.Host = strings.TrimSpace(in.Host)
		if in.Proto != "udp" && in.Proto != "tcp" {
			fail(w, errors.New("choose UDP or TCP"))
			return
		}
		if in.Port < 1 || in.Port > 65535 {
			fail(w, errors.New("the port must be from 1 to 65535"))
			return
		}
		if in.On && (in.Host == "" || strings.ContainsAny(in.Host, " /\\:@") && net.ParseIP(in.Host) == nil) {
			fail(w, errors.New("enter the address or name of your log server, such as 192.168.1.20"))
			return
		}
		b, _ := json.Marshal(in)
		a.Config.SetSetting("syslog.config", string(b))
		a.loadSyslog()
		if in.On {
			a.syslogSend("info", "Gatehouse is now sending its logs here")
		}
		ok(w)
	})
}

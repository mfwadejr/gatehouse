package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/flowexp"
)

// Flow export: tells a collector on your network (a traffic-analysis tool) about every connection
// that crosses the router, as IPFIX or NetFlow v9. Off by default. Sending is best effort and never
// slows the router.

type flowCfg struct {
	On     bool   `json:"on"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Format string `json:"format"` // ipfix or netflow9
}

type flowOut struct {
	mu      sync.Mutex
	conn    net.Conn
	enc     *flowexp.Encoder
	tr      flowexp.Tracker
	cfg     flowCfg
	records uint64
	packets uint64
	last    string
	accOn   bool
}

func (a *App) flowConfig() flowCfg {
	c := flowCfg{Port: 4739, Format: "ipfix"}
	if raw := a.setting("flow.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	return c
}

func (f *flowOut) reset(c flowCfg) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = c
	f.enc = nil
	f.tr = flowexp.Tracker{}
	if f.conn != nil {
		f.conn.Close()
		f.conn = nil
	}
}

// flowStep does one look at the connection list and sends what is due.
func (a *App) flowStep(now time.Time) {
	f := &a.flow
	f.mu.Lock()
	c := f.cfg
	f.mu.Unlock()
	if !c.On || c.Host == "" || a.Net == nil {
		f.mu.Lock()
		was := f.accOn
		f.accOn = false
		f.mu.Unlock()
		if was {
			a.Net.FlowAcct(false)
		}
		return
	}
	f.mu.Lock()
	need := !f.accOn
	f.mu.Unlock()
	if need {
		if err := a.Net.FlowAcct(true); err != nil {
			f.mu.Lock()
			f.last = err.Error()
			f.mu.Unlock()
			return
		}
		f.mu.Lock()
		f.accOn = true
		f.mu.Unlock()
	}
	text, err := a.Net.ConntrackDump()
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.last = err.Error()
		return
	}
	recs := f.tr.Poll(now, flowexp.ParseConntrack(text))
	if len(recs) == 0 {
		return
	}
	if f.enc == nil {
		fm := flowexp.IPFIX
		if c.Format == "netflow9" {
			fm = flowexp.V9
		}
		f.enc = &flowexp.Encoder{Format: fm, DomainID: 1, Boot: now}
	}
	pk, err := f.enc.Encode(recs, now)
	if err != nil {
		f.last = err.Error()
		return
	}
	if f.conn == nil {
		cn, err := net.DialTimeout("udp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), 3*time.Second)
		if err != nil {
			f.last = "Cannot reach " + c.Host + ": " + err.Error()
			return
		}
		f.conn = cn
	}
	for _, p := range pk {
		f.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err := f.conn.Write(p); err != nil {
			f.conn.Close()
			f.conn = nil
			f.last = "Sending failed: " + err.Error()
			return
		}
		f.packets++
	}
	f.records += uint64(len(recs))
	f.last = ""
}

func (a *App) flowLoop(ctx context.Context) {
	a.flow.reset(a.flowConfig())
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			a.flowStep(now)
		}
	}
}

func (a *App) registerFlowRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/flows", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.flow.mu.Lock()
		st := map[string]any{"records": a.flow.records, "packets": a.flow.packets, "error": a.flow.last, "open": a.flow.tr.Open()}
		a.flow.mu.Unlock()
		api.JSON(w, 200, map[string]any{"config": a.flowConfig(), "status": st})
	})
	s.HandleAdmin("PUT /api/flows", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in flowCfg
		if !api.Decode(w, r, &in) {
			return
		}
		in.Host = strings.TrimSpace(in.Host)
		if in.Format != "ipfix" && in.Format != "netflow9" {
			fail(w, errors.New("choose IPFIX or NetFlow v9"))
			return
		}
		if in.Port < 1 || in.Port > 65535 {
			fail(w, errors.New("the port must be from 1 to 65535"))
			return
		}
		if in.On && (in.Host == "" || strings.ContainsAny(in.Host, " /\\:@") && net.ParseIP(in.Host) == nil) {
			fail(w, errors.New("enter the address or name of your flow collector, such as 192.168.1.20"))
			return
		}
		b, _ := json.Marshal(in)
		a.Config.SetSetting("flow.config", string(b))
		a.flow.reset(in)
		ok(w)
	})
}

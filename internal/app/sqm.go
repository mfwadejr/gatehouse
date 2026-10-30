package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
)

// Bufferbloat control (smart queues). For each internet connection the person turns it on for, the
// connection is held to 95 % of the plan speed (the "Speed test" screen holds the plan speeds), in
// both directions, so delay stays low when the line is busy. Off by default, and a failure leaves
// the connection exactly as it was.

const sqmShare = 0.95

type sqmCfg struct {
	Links map[string]bool `json:"links"`
}

type sqmState struct {
	mu      sync.Mutex
	hash    [32]byte
	have    bool
	at      time.Time
	disc    string
	lastErr string
	devs    map[string]bool // ports that may carry shaping now
}

func (a *App) sqmConfig() sqmCfg {
	c := sqmCfg{Links: map[string]bool{}}
	if raw := a.setting("sqm.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.Links == nil {
		c.Links = map[string]bool{}
	}
	return c
}

// sqmPlan builds the plan from the setting and the plan speeds. A connection with no plan speed
// set cannot be shaped, so it is left out.
func (a *App) sqmPlan() netd.SQMPlan {
	cfg, sp, devs := a.sqmConfig(), a.speedConfig(), a.wanDevices()
	var plan netd.SQMPlan
	seen := map[string]bool{}
	names := make([]string, 0, len(devs))
	for n := range devs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		dev := devs[n]
		if !seen[dev] {
			seen[dev] = true
			plan.Known = append(plan.Known, dev)
		}
		l := sp.Links[n]
		if !cfg.Links[n] || l.DownMbps <= 0 || l.UpMbps <= 0 {
			continue
		}
		plan.Links = append(plan.Links, netd.SQMLink{Iface: dev, DownKbit: int(l.DownMbps * 1000 * sqmShare), UpKbit: int(l.UpMbps * 1000 * sqmShare)})
	}
	a.sqm.mu.Lock()
	for d := range a.sqm.devs {
		if !seen[d] {
			plan.Known = append(plan.Known, d)
		}
	}
	a.sqm.mu.Unlock()
	sort.Strings(plan.Known)
	return plan
}

// sqmSync makes the queues match the setting. With force it re-applies even if nothing changed.
func (a *App) sqmSync(force bool) error {
	if a.Net == nil {
		return nil
	}
	plan := a.sqmPlan()
	h := sha256.Sum256([]byte(fmt.Sprintf("%v", plan)))
	st := &a.sqm
	st.mu.Lock()
	defer st.mu.Unlock()
	if !force && st.have && st.hash == h && time.Since(st.at) < 10*time.Minute {
		return nil
	}
	if len(plan.Links) == 0 && !st.have && !force {
		st.hash, st.have, st.at = h, true, time.Now()
		return nil // nothing was ever applied and nothing is wanted
	}
	disc, err := a.Net.SQMApply(plan)
	if err != nil {
		st.lastErr, st.have = err.Error(), false
		return err
	}
	st.hash, st.have, st.at, st.disc, st.lastErr = h, true, time.Now(), disc, ""
	st.devs = map[string]bool{}
	for _, l := range plan.Links {
		st.devs[l.Iface] = true
	}
	return nil
}

func (a *App) sqmLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	for {
		a.sqmSync(false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

func (a *App) registerSQMRoutes() {
	s := a.API
	s.Handle("GET /api/sqm", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		cfg, sp := a.sqmConfig(), a.speedConfig()
		type row struct {
			Name     string  `json:"name"`
			On       bool    `json:"on"`
			Ready    bool    `json:"ready"` // plan speeds are filled in
			DownMbps float64 `json:"down_mbps"`
			UpMbps   float64 `json:"up_mbps"`
			HoldDown float64 `json:"hold_down_mbps"`
			HoldUp   float64 `json:"hold_up_mbps"`
		}
		rows := []row{}
		if wans, err := a.listWans(a.Config); err == nil {
			for _, w := range wans {
				if !w.Enabled {
					continue
				}
				l := sp.Links[w.Name]
				rows = append(rows, row{Name: w.Name, On: cfg.Links[w.Name], Ready: l.DownMbps > 0 && l.UpMbps > 0, DownMbps: l.DownMbps, UpMbps: l.UpMbps,
					HoldDown: l.DownMbps * sqmShare, HoldUp: l.UpMbps * sqmShare})
			}
		}
		a.sqm.mu.Lock()
		out := map[string]any{"links": rows, "queue": a.sqm.disc, "error": a.sqm.lastErr, "share": int(sqmShare * 100)}
		a.sqm.mu.Unlock()
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("PUT /api/sqm", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Links map[string]bool `json:"links"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		sp := a.speedConfig()
		names := map[string]bool{}
		if wans, err := a.listWans(a.Config); err == nil {
			for _, w := range wans {
				names[w.Name] = true
			}
		}
		n := sqmCfg{Links: map[string]bool{}}
		for k, v := range in.Links {
			if !names[k] {
				fail(w, errors.New("there is no internet connection called "+k))
				return
			}
			if v {
				if l := sp.Links[k]; l.DownMbps <= 0 || l.UpMbps <= 0 {
					fail(w, fmt.Errorf("enter the download and upload speed your plan promises for %s first (in the Speed test box above)", k))
					return
				}
			}
			n.Links[k] = v
		}
		old := a.setting("sqm.config", "")
		b, _ := json.Marshal(n)
		a.Config.SetSetting("sqm.config", string(b))
		if err := a.sqmSync(true); err != nil {
			a.Config.SetSetting("sqm.config", old)
			a.sqmSync(true)
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Speed limits for smooth connections were changed", "")
		ok(w)
	})
}

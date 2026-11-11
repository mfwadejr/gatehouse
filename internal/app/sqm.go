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

	"nopewarden/internal/api"
	"nopewarden/internal/netd"
)

// Bufferbloat control (smart queues). For each internet connection the person turns it on for, the
// connection is held to 95 % of the plan speed (the "Speed test" screen holds the plan speeds), in
// both directions, so delay stays low when the line is busy. Off by default, and a failure leaves
// the connection exactly as it was.

const sqmShare = 0.95

type sqmFixed struct {
	DownMbps float64 `json:"down_mbps"`
	UpMbps   float64 `json:"up_mbps"`
}

type sqmCfg struct {
	Links map[string]bool     `json:"links"`
	Mode  map[string]string   `json:"mode,omitempty"`  // per connection: follow (the default) or fixed
	Fixed map[string]sqmFixed `json:"fixed,omitempty"` // the numbers used in fixed mode
	Auto  bool                `json:"auto,omitempty"`  // automatic tests
}

func (c sqmCfg) mode(link string) string {
	if c.Mode[link] == "fixed" {
		return "fixed"
	}
	return "follow"
}

type sqmState struct {
	mu      sync.Mutex
	hash    [32]byte
	have    bool
	at      time.Time
	disc    string
	lastErr string
	devs    map[string]bool // ports that may carry shaping now
	lifted  map[string]bool // ports whose shaping is off for the length of one test
	giveUp  map[string]time.Time
	tryFrom map[string]time.Time
	check   sqmCheckState
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

// sqmPlan builds the plan from the setting. A connection that has no speed to hold to yet (no plan
// speed and no test) cannot be shaped, so it is left out. A connection whose shaping is lifted for
// one test is named but not shaped.
func (a *App) sqmPlan() netd.SQMPlan {
	cfg, devs := a.sqmConfig(), a.wanDevices()
	a.sqm.mu.Lock()
	lifted := map[string]bool{}
	for d := range a.sqm.lifted {
		lifted[d] = true
	}
	a.sqm.mu.Unlock()
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
		if !cfg.Links[n] || lifted[dev] {
			continue
		}
		down, up := a.sqmShapingNow(n)
		if down <= 0 || up <= 0 {
			continue
		}
		plan.Links = append(plan.Links, netd.SQMLink{Iface: dev, DownKbit: int(down * 1000), UpKbit: int(up * 1000)})
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
		a.sqmRetune(time.Now(), false)
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
		api.JSON(w, 200, a.sqmView())
	})
	s.HandleAdmin("PUT /api/sqm", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Links map[string]bool     `json:"links"`
			Mode  map[string]string   `json:"mode"`
			Fixed map[string]sqmFixed `json:"fixed"`
			Auto  *bool               `json:"auto"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		names := map[string]bool{}
		if wans, err := a.listWans(a.Config); err == nil {
			for _, w := range wans {
				names[w.Name] = true
			}
		}
		old := a.sqmConfig()
		oldRaw, oldShape := a.setting("sqm.config", ""), a.setting("sqm.shape", "")
		n := sqmCfg{Links: map[string]bool{}, Mode: map[string]string{}, Fixed: map[string]sqmFixed{}, Auto: old.Auto}
		for k, v := range old.Links {
			n.Links[k] = v
		}
		for k, v := range old.Mode {
			n.Mode[k] = v
		}
		for k, v := range old.Fixed {
			n.Fixed[k] = v
		}
		if in.Links != nil {
			n.Links = map[string]bool{}
			for k, v := range in.Links {
				n.Links[k] = v
			}
		}
		for k, v := range in.Mode {
			if v != "follow" && v != "fixed" {
				fail(w, errors.New("choose follow my measurements, or fixed numbers"))
				return
			}
			n.Mode[k] = v
		}
		for k, v := range in.Fixed {
			if v.DownMbps < 0.1 || v.UpMbps < 0.1 || v.DownMbps > 100000 || v.UpMbps > 100000 {
				fail(w, errors.New("the fixed speeds are not sensible"))
				return
			}
			n.Fixed[k] = v
		}
		if in.Auto != nil {
			n.Auto = *in.Auto
		}
		sp := a.speedConfig()
		for k := range n.Links {
			if !names[k] {
				fail(w, errors.New("there is no internet connection called "+k))
				return
			}
		}
		for k, v := range n.Links {
			if !v {
				continue
			}
			l := sp.Links[k]
			if l.DownMbps <= 0 || l.UpMbps <= 0 {
				if _, has := n.Fixed[k]; !(n.Mode[k] != "fixed" && a.sqmMeasure(k, time.Now()).Tests > 0) && !has {
					fail(w, fmt.Errorf("enter the download and upload speed your plan promises for %s first (in the Speed test box above), or run a speed test", k))
					return
				}
			}
		}
		// A link whose switch, mode or numbers changed starts again from the right speed at once.
		shape := a.sqmShapes()
		for k := range names {
			if n.Links[k] != old.Links[k] || n.mode(k) != old.mode(k) || n.Fixed[k] != old.Fixed[k] {
				delete(shape, k)
			}
		}
		a.saveSqmShapes(shape)
		b, _ := json.Marshal(n)
		a.Config.SetSetting("sqm.config", string(b))
		if err := a.sqmSync(true); err != nil {
			a.Config.SetSetting("sqm.config", oldRaw)
			a.Config.SetSetting("sqm.shape", oldShape)
			a.sqmSync(true)
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Speed limits for smooth connections were changed", "")
		ok(w)
	})
	s.HandleAdmin("POST /api/sqm/check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Link string `json:"link"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.startSqmCheck(in.Link); err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"ok": true, "started": true})
	})
}

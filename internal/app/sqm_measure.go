package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// Keep delay low, from what the line really delivers.
//
// Each connection has three speeds. Promised is what the plan says (typed in on the Speed test box).
// Measured typical is worked out from the last 7 days of good full tests. Shaping speed is the one
// the queue is actually held to: the measured typical when the connection follows its measurements,
// or the numbers the person typed when it is fixed.
//
// Why these numbers:
//   - the 25th percentile, not the average: steady delay matters more than the last bit of speed, and
//     the evening slow-downs are what the queue has to protect against;
//   - trimmed by 3 %, so the queue stays just under what the line can do;
//   - 7 days, so a weekly pattern is included and one bad day does not move it far;
//   - at most 10 % a day upwards and 20 % a day downwards, so one odd week cannot swing it, while a
//     line that got worse is followed sooner than one that got better;
//   - tests only count when the line was quiet, the test did not go through the queue, it was a full
//     test, it worked, and no outage was going on.
const (
	sqmPercentile  = 0.25
	sqmTrim        = 0.97
	sqmWindow      = 7 * 24 * time.Hour
	sqmLearnTests  = 8
	sqmLearnSpan   = 48 * time.Hour
	sqmLearnShare  = 0.85 // while learning: this share of the best good test
	sqmUpPerDay    = 0.10
	sqmDownPerDay  = 0.20
	sqmDeadband    = 0.02
	sqmRetuneEvery = time.Hour
	sqmAutoEvery   = 4 * time.Hour
	sqmAutoWindow  = time.Hour
	sqmQuietFor    = 60 // seconds of samples
)

// percentile is the p-th value (0 to 1) of vals, by straight-line steps between neighbours.
func percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	v := append([]float64(nil), vals...)
	sort.Float64s(v)
	if len(v) == 1 {
		return v[0]
	}
	pos := p * float64(len(v)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return v[lo] + (v[hi]-v[lo])*(pos-float64(lo))
}

type sqmMeasured struct {
	Down, Up         float64 // Mbps, the typical (trimmed 25th percentile)
	BestDown, BestUp float64
	Tests            int
	SpanDays         float64
	Learning         bool
	Trend            string // rising, steady, falling or empty
}

type goodTest struct {
	ts       int64
	down, up float64
}

// goodTests are the tests that say what the line can do: full, working, on a quiet line, not through
// the queue and not during an outage.
func (a *App) goodTests(link string, from, to time.Time) []goodTest {
	rows, err := a.Logs.Query(`SELECT ts,down_bps,up_bps FROM speedtests WHERE link=? AND ts>=? AND ts<? AND error='' AND quick=0 AND busy=0 AND shaped=0 AND down_bps>0 AND up_bps>0 ORDER BY ts`, link, from.Unix(), to.Unix())
	if err != nil {
		return nil
	}
	var out []goodTest
	for rows.Next() {
		var g goodTest
		if rows.Scan(&g.ts, &g.down, &g.up) == nil {
			g.down, g.up = g.down/1e6, g.up/1e6
			out = append(out, g)
		}
	}
	rows.Close()
	if len(out) == 0 {
		return nil
	}
	type span struct{ s, e int64 }
	var gaps []span
	if or, err := a.Logs.Query(`SELECT start,end FROM outages WHERE start<? AND (end=0 OR end>?)`, to.Unix(), from.Unix()); err == nil {
		for or.Next() {
			var s, e int64
			if or.Scan(&s, &e) == nil {
				if e == 0 {
					e = to.Unix()
				}
				gaps = append(gaps, span{s, e + 120})
			}
		}
		or.Close()
	}
	keep := out[:0]
	for _, g := range out {
		bad := false
		for _, x := range gaps {
			if g.ts >= x.s && g.ts <= x.e {
				bad = true
			}
		}
		if !bad {
			keep = append(keep, g)
		}
	}
	return keep
}

func (a *App) sqmMeasure(link string, now time.Time) sqmMeasured {
	var m sqmMeasured
	cur := a.goodTests(link, now.Add(-sqmWindow), now.Add(time.Second))
	m.Tests = len(cur)
	if m.Tests == 0 {
		m.Learning = true
		return m
	}
	var d, u []float64
	for _, g := range cur {
		d, u = append(d, g.down), append(u, g.up)
		m.BestDown, m.BestUp = math.Max(m.BestDown, g.down), math.Max(m.BestUp, g.up)
	}
	m.Down, m.Up = percentile(d, sqmPercentile)*sqmTrim, percentile(u, sqmPercentile)*sqmTrim
	m.SpanDays = float64(cur[len(cur)-1].ts-cur[0].ts) / 86400
	m.Learning = m.Tests < sqmLearnTests || time.Duration(cur[len(cur)-1].ts-cur[0].ts)*time.Second < sqmLearnSpan
	prev := a.goodTests(link, now.Add(-2*sqmWindow), now.Add(-sqmWindow))
	if len(prev) >= 4 && len(cur) >= 4 {
		var pd []float64
		for _, g := range prev {
			pd = append(pd, g.down)
		}
		if p := percentile(pd, sqmPercentile); p > 0 {
			switch r := percentile(d, sqmPercentile) / p; {
			case r > 1.05:
				m.Trend = "rising"
			case r < 0.95:
				m.Trend = "falling"
			default:
				m.Trend = "steady"
			}
		}
	}
	return m
}

// sqmWanted is the speed a connection should be held to now, before the daily limit on change.
func (a *App) sqmWanted(link string, now time.Time) (down, up float64, why string) {
	cfg, sp := a.sqmConfig(), a.speedConfig().Links[link]
	if cfg.mode(link) == "fixed" {
		if f, ok := cfg.Fixed[link]; ok && f.DownMbps > 0 && f.UpMbps > 0 {
			return f.DownMbps, f.UpMbps, "fixed"
		}
		if sp.DownMbps > 0 && sp.UpMbps > 0 {
			return sp.DownMbps * sqmShare, sp.UpMbps * sqmShare, "fixed"
		}
		return 0, 0, ""
	}
	m := a.sqmMeasure(link, now)
	switch {
	case m.Tests == 0:
		if sp.DownMbps > 0 && sp.UpMbps > 0 {
			return sp.DownMbps * sqmShare, sp.UpMbps * sqmShare, "promised"
		}
		return 0, 0, ""
	case m.Learning:
		down, up, why = m.BestDown*sqmLearnShare, m.BestUp*sqmLearnShare, "learning"
	default:
		down, up, why = m.Down, m.Up, "measured"
	}
	if sp.DownMbps > 0 {
		down = math.Min(down, sp.DownMbps*sqmShare)
	}
	if sp.UpMbps > 0 {
		up = math.Min(up, sp.UpMbps*sqmShare)
	}
	return down, up, why
}

type sqmShape struct {
	DownMbps float64 `json:"down_mbps"`
	UpMbps   float64 `json:"up_mbps"`
	At       int64   `json:"at"`      // when it last changed
	Checked  int64   `json:"checked"` // when it was last looked at
	Why      string  `json:"why"`
}

func (a *App) sqmShapes() map[string]sqmShape {
	m := map[string]sqmShape{}
	if raw := a.setting("sqm.shape", ""); raw != "" {
		json.Unmarshal([]byte(raw), &m)
	}
	return m
}

func (a *App) saveSqmShapes(m map[string]sqmShape) {
	b, _ := json.Marshal(m)
	a.Config.SetSetting("sqm.shape", string(b))
}

// stepToward moves cur toward want by at most the daily limit for the time that has passed.
func stepToward(cur, want float64, elapsed time.Duration) float64 {
	if cur <= 0 {
		return want
	}
	if math.Abs(want-cur)/cur < sqmDeadband {
		return cur
	}
	days := math.Min(elapsed.Hours(), 24) / 24
	if want > cur {
		return math.Min(want, cur*(1+sqmUpPerDay*days))
	}
	return math.Max(want, cur*(1-sqmDownPerDay*days))
}

// sqmShapingNow is the speed in use for a connection, in Mbps. The first time it is worked out it
// is taken straight from the wanted speed; after that sqmRetune moves it a little at a time.
func (a *App) sqmShapingNow(link string) (down, up float64) {
	sh := a.sqmShapes()
	if s, ok := sh[link]; ok && s.DownMbps > 0 && s.UpMbps > 0 {
		return s.DownMbps, s.UpMbps
	}
	now := time.Now()
	d, u, why := a.sqmWanted(link, now)
	if d <= 0 || u <= 0 {
		return 0, 0
	}
	sh[link] = sqmShape{DownMbps: d, UpMbps: u, At: now.Unix(), Checked: now.Unix(), Why: why}
	a.saveSqmShapes(sh)
	return d, u
}

// sqmRetune looks at each connection that follows its measurements, once an hour, and moves the
// speed it is held to toward the measured one within the daily limit.
func (a *App) sqmRetune(now time.Time, force bool) {
	cfg := a.sqmConfig()
	sh := a.sqmShapes()
	changed := false
	for link, on := range cfg.Links {
		s, have := sh[link]
		if !on {
			if have {
				delete(sh, link)
				changed = true
			}
			continue
		}
		if !have {
			continue // sqmShapingNow sets it the first time it is needed
		}
		if cfg.mode(link) == "fixed" {
			d, u, why := a.sqmWanted(link, now)
			if d > 0 && (d != s.DownMbps || u != s.UpMbps) {
				sh[link] = sqmShape{DownMbps: d, UpMbps: u, At: now.Unix(), Checked: now.Unix(), Why: why}
				changed = true
			}
			continue
		}
		if !force && now.Sub(time.Unix(s.Checked, 0)) < sqmRetuneEvery {
			continue
		}
		d, u, why := a.sqmWanted(link, now)
		if d <= 0 || u <= 0 {
			continue
		}
		el := now.Sub(time.Unix(s.Checked, 0))
		nd, nu := stepToward(s.DownMbps, d, el), stepToward(s.UpMbps, u, el)
		ns := sqmShape{DownMbps: nd, UpMbps: nu, At: s.At, Checked: now.Unix(), Why: why}
		if nd != s.DownMbps || nu != s.UpMbps {
			ns.At = now.Unix()
		}
		sh[link] = ns
		changed = true
	}
	if changed {
		a.saveSqmShapes(sh)
	}
}

// lineQuiet says whether a connection has been quiet for the last minute: its average speed in both
// directions is a small share of what the line can do. With no readings (not yet sampled) it counts as quiet.
func (a *App) lineQuiet(link string) bool {
	s, ok := a.traffic.snapshot()[link]
	if !ok || len(s.Rx) < 30 {
		return true
	}
	pick := func(v []float64) float64 {
		if len(v) > sqmQuietFor {
			v = v[len(v)-sqmQuietFor:]
		}
		t := 0.0
		for _, x := range v {
			t += x
		}
		return t / float64(len(v))
	}
	sp := a.speedConfig().Links[link]
	m := a.sqmMeasure(link, time.Now())
	down, up := math.Max(sp.DownMbps, m.BestDown), math.Max(sp.UpMbps, m.BestUp)
	thrD, thrU := math.Max(3e6, 0.03*down*1e6), math.Max(1e6, 0.03*up*1e6)
	return pick(s.Rx) < thrD && pick(s.Tx) < thrU
}

// sqmShaped says whether a port is carrying shaping right now.
func (a *App) sqmShaped(dev string) bool {
	a.sqm.mu.Lock()
	defer a.sqm.mu.Unlock()
	return a.sqm.devs[dev] && !a.sqm.lifted[dev]
}

// sqmLift takes the shaping off one port until the returned function is called, so a test on a quiet
// line measures what the line can really do instead of what the queue allows.
func (a *App) sqmLift(dev string) func() {
	a.sqm.mu.Lock()
	if !a.sqm.devs[dev] {
		a.sqm.mu.Unlock()
		return func() {}
	}
	if a.sqm.lifted == nil {
		a.sqm.lifted = map[string]bool{}
	}
	a.sqm.lifted[dev] = true
	a.sqm.mu.Unlock()
	a.sqmSync(true)
	return func() {
		a.sqm.mu.Lock()
		delete(a.sqm.lifted, dev)
		a.sqm.mu.Unlock()
		a.sqmSync(true)
	}
}

// sqmTestLoop runs the automatic tests: about every 4 hours for each connection that keeps delay low
// and follows its measurements, only when the line has been quiet for a minute. If the line stays
// busy for an hour after one is due, it waits for the next period.
func (a *App) sqmTestLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			a.sqmAutoTests(ctx, now)
		}
	}
}

func (a *App) sqmAutoTests(ctx context.Context, now time.Time) {
	cfg := a.sqmConfig()
	if !cfg.Auto {
		return
	}
	wans, err := a.listWans(a.Config)
	if err != nil {
		return
	}
	sc := a.speedConfig()
	for _, w := range wans {
		if ctx.Err() != nil {
			return
		}
		if !w.Enabled || !cfg.Links[w.Name] || cfg.mode(w.Name) != "follow" || a.isMetered(w.Name, w.Iface, sc) {
			continue
		}
		var last int64
		a.Logs.QueryRow(`SELECT COALESCE(MAX(ts),0) FROM speedtests WHERE link=? AND source='auto'`, w.Name).Scan(&last)
		a.sqm.mu.Lock()
		if a.sqm.giveUp == nil {
			a.sqm.giveUp, a.sqm.tryFrom = map[string]time.Time{}, map[string]time.Time{}
		}
		base := time.Unix(last, 0)
		if g := a.sqm.giveUp[w.Name]; g.After(base) {
			base = g
		}
		if now.Before(base.Add(sqmAutoEvery)) {
			a.sqm.mu.Unlock()
			continue
		}
		from := a.sqm.tryFrom[w.Name]
		if from.IsZero() {
			from = now
			a.sqm.tryFrom[w.Name] = now
		}
		if now.Sub(from) > sqmAutoWindow {
			a.sqm.giveUp[w.Name] = now
			delete(a.sqm.tryFrom, w.Name)
			a.sqm.mu.Unlock()
			continue
		}
		a.sqm.mu.Unlock()
		if !a.lineQuiet(w.Name) {
			continue
		}
		a.sqm.mu.Lock()
		delete(a.sqm.tryFrom, w.Name)
		a.sqm.mu.Unlock()
		if _, err := a.runSpeed(w.Name, "auto", true); err == nil {
			a.sqmRetune(time.Now(), true)
		}
	}
}

// ---- before and after ----

type sqmCheckSide struct {
	LoadedMS float64 `json:"loaded_ms"`
	DelayMS  float64 `json:"delay_ms"`
	DownMbps float64 `json:"down_mbps"`
	UpMbps   float64 `json:"up_mbps"`
}

type sqmCheckResult struct {
	Link   string       `json:"link"`
	At     int64        `json:"at"`
	Before sqmCheckSide `json:"before"`
	After  sqmCheckSide `json:"after"`
	Words  string       `json:"words"`
	Error  string       `json:"error,omitempty"`
}

type sqmCheckState struct {
	Running bool
	Link    string
	Step    string
}

func checkWords(b, af sqmCheckSide) string {
	speeds := fmt.Sprintf("Download went from %.0f to %.0f Mbps and upload from %.0f to %.0f Mbps.", b.DownMbps, af.DownMbps, b.UpMbps, af.UpMbps)
	if b.LoadedMS <= 0 || af.LoadedMS <= 0 {
		return "The delay under load could not be measured this time, so there is nothing to compare. " + speeds
	}
	drop := b.LoadedMS - af.LoadedMS
	switch {
	case drop >= 20 && drop >= 0.25*b.LoadedMS:
		return fmt.Sprintf("Better. When the line is busy, answers took %.0f ms without it and %.0f ms with it. %s", b.LoadedMS, af.LoadedMS, speeds)
	case drop <= -20 && -drop >= 0.25*b.LoadedMS:
		return fmt.Sprintf("Worse. When the line is busy, answers took %.0f ms without it and %.0f ms with it. Turn it off, or use lower fixed numbers. %s", b.LoadedMS, af.LoadedMS, speeds)
	}
	return fmt.Sprintf("About the same. When the line is busy, answers took %.0f ms without it and %.0f ms with it, so this line was already steady, or the speed it is held to is still too high. %s", b.LoadedMS, af.LoadedMS, speeds)
}

func (a *App) startSqmCheck(link string) error {
	dev, ok := a.wanDevices()[link]
	if !ok {
		return errors.New("that internet connection is not switched on")
	}
	if !a.sqmConfig().Links[link] {
		return errors.New("turn on Keep delay low for " + link + " first, so there is something to compare")
	}
	a.speed.mu.Lock()
	busy := a.speed.running
	a.speed.mu.Unlock()
	a.sqm.mu.Lock()
	if busy || a.sqm.check.Running {
		a.sqm.mu.Unlock()
		return errSpeedBusy
	}
	a.sqm.check = sqmCheckState{Running: true, Link: link, Step: "Measuring without it"}
	a.sqm.mu.Unlock()
	go func() {
		res := sqmCheckResult{Link: link, At: time.Now().Unix()}
		finish := func() {
			b, _ := json.Marshal(res)
			a.Config.SetSetting("sqm.check."+link, string(b))
			a.sqm.mu.Lock()
			a.sqm.check = sqmCheckState{}
			a.sqm.mu.Unlock()
		}
		restore := a.sqmLift(dev)
		before, err := a.runSpeedOn(link, "check-before", true, "")
		restore()
		if err != nil {
			res.Error = err.Error()
			finish()
			return
		}
		a.sqm.mu.Lock()
		a.sqm.check.Step = "Measuring with it"
		a.sqm.mu.Unlock()
		time.Sleep(3 * time.Second)
		after, err := a.runSpeedOn(link, "check-after", true, "")
		if err != nil {
			res.Error = err.Error()
			finish()
			return
		}
		side := func(r speedRow) sqmCheckSide {
			return sqmCheckSide{LoadedMS: r.LoadedMS, DelayMS: r.DelayMS, DownMbps: r.DownBps / 1e6, UpMbps: r.UpBps / 1e6}
		}
		res.Before, res.After = side(before), side(after)
		res.Words = checkWords(res.Before, res.After)
		finish()
	}()
	return nil
}

// sqmView is the page's picture of every connection.
func (a *App) sqmView() map[string]any {
	cfg, sp := a.sqmConfig(), a.speedConfig()
	now := time.Now()
	type row struct {
		Name       string          `json:"name"`
		On         bool            `json:"on"`
		Ready      bool            `json:"ready"`
		Mode       string          `json:"mode"`
		DownMbps   float64         `json:"down_mbps"` // promised
		UpMbps     float64         `json:"up_mbps"`
		MeasDown   float64         `json:"measured_down_mbps"`
		MeasUp     float64         `json:"measured_up_mbps"`
		Tests      int             `json:"tests"`
		Learning   bool            `json:"learning"`
		Trend      string          `json:"trend,omitempty"`
		HoldDown   float64         `json:"hold_down_mbps"` // in use (or what would be)
		HoldUp     float64         `json:"hold_up_mbps"`
		Why        string          `json:"why,omitempty"`
		Changed    int64           `json:"changed,omitempty"`
		FixedDown  float64         `json:"fixed_down_mbps"`
		FixedUp    float64         `json:"fixed_up_mbps"`
		Metered    bool            `json:"metered"`
		Check      *sqmCheckResult `json:"check,omitempty"`
	}
	rows := []row{}
	shapes := a.sqmShapes()
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			if !w.Enabled {
				continue
			}
			l := sp.Links[w.Name]
			m := a.sqmMeasure(w.Name, now)
			r := row{Name: w.Name, On: cfg.Links[w.Name], Mode: cfg.mode(w.Name), DownMbps: l.DownMbps, UpMbps: l.UpMbps,
				MeasDown: m.Down, MeasUp: m.Up, Tests: m.Tests, Learning: m.Learning, Trend: m.Trend,
				Ready: (l.DownMbps > 0 && l.UpMbps > 0) || m.Tests > 0, Metered: a.isMetered(w.Name, w.Iface, sp)}
			if f, ok := cfg.Fixed[w.Name]; ok {
				r.FixedDown, r.FixedUp = f.DownMbps, f.UpMbps
			}
			if s, ok := shapes[w.Name]; ok && r.On {
				r.HoldDown, r.HoldUp, r.Why, r.Changed = s.DownMbps, s.UpMbps, s.Why, s.At
			} else if d, u, why := a.sqmWanted(w.Name, now); d > 0 {
				r.HoldDown, r.HoldUp, r.Why = d, u, why
			}
			if raw := a.setting("sqm.check."+w.Name, ""); raw != "" {
				var c sqmCheckResult
				if json.Unmarshal([]byte(raw), &c) == nil {
					r.Check = &c
				}
			}
			rows = append(rows, r)
		}
	}
	a.sqm.mu.Lock()
	out := map[string]any{"links": rows, "queue": a.sqm.disc, "error": a.sqm.lastErr, "share": int(sqmShare * 100), "auto": cfg.Auto,
		"check": map[string]any{"running": a.sqm.check.Running, "link": a.sqm.check.Link, "step": a.sqm.check.Step}}
	a.sqm.mu.Unlock()
	return out
}

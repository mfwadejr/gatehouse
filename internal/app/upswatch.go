package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"context"

	"nopewarden/internal/api"
	"nopewarden/internal/netd"
)

// Power: watching a UPS (APC and others, over USB or the network) and choosing, for each thing that
// can happen, what NopeWarden does about it: write it in the log, send an alert, show it on the
// Overview list and the menu dot, and shut the machine down cleanly.

// powerEvents lists the things that can happen, in the order they are shown.
var powerEvents = []struct {
	ID, Label, When string
	CanShutdown     bool
}{
	{"cut", "Power cut", "The power fails and the UPS starts running on its battery.", true},
	{"low", "Battery low", "The battery is down to the charge or time left set above.", true},
	{"replace", "Battery needs replacing", "The UPS says its battery is worn out. Repeated once a day until fixed.", false},
	{"back", "Power is back", "The power returns after a cut.", false},
	{"silent", "UPS not responding", "NopeWarden cannot reach the UPS for a minute, so it would not know if the power failed.", true},
}

type powerAct struct {
	Log      bool `json:"log"`
	Alert    bool `json:"alert"`
	Overview bool `json:"overview"`
	Shutdown bool `json:"shutdown"`
}

type powerCfg struct {
	On         bool                `json:"on"` // a UPS has been set up and is being watched
	Source     string              `json:"source"`
	Host       string              `json:"host"`
	Name       string              `json:"name"`
	LowCharge  int                 `json:"low_charge"`  // percent
	LowMinutes int                 `json:"low_minutes"` // minutes of battery left
	Delay      int                 `json:"delay"`       // seconds between the decision and the shutdown
	CutOutput  bool                `json:"cut_output"`  // tell the UPS to switch off and back on with the power
	Events     map[string]powerAct `json:"events"`
}

func defaultPower() powerCfg {
	return powerCfg{
		Source: "usb", LowCharge: 20, LowMinutes: 5, Delay: 60, CutOutput: true,
		Events: map[string]powerAct{
			"cut":     {Log: true, Alert: true, Overview: true},
			"low":     {Log: true, Alert: true, Overview: true, Shutdown: true},
			"replace": {Log: true, Alert: true, Overview: true},
			"back":    {Log: true, Alert: true},
			"silent":  {Log: true, Alert: true, Overview: true},
		},
	}
}

func (a *App) powerConfig() powerCfg {
	c := defaultPower()
	if raw := a.setting("power.config", ""); raw != "" {
		var in powerCfg
		if json.Unmarshal([]byte(raw), &in) == nil {
			c = in
			if c.Events == nil {
				c.Events = map[string]powerAct{}
			}
			for k, v := range defaultPower().Events {
				if _, ok := c.Events[k]; !ok {
					c.Events[k] = v
				}
			}
		}
	}
	if c.Source != "network" {
		c.Source = "usb"
	}
	if c.LowCharge < 1 || c.LowCharge > 90 {
		c.LowCharge = 20
	}
	if c.LowMinutes < 1 || c.LowMinutes > 60 {
		c.LowMinutes = 5
	}
	if c.Delay < 0 || c.Delay > 600 {
		c.Delay = 60
	}
	for _, e := range powerEvents {
		if !e.CanShutdown {
			x := c.Events[e.ID]
			x.Shutdown = false
			c.Events[e.ID] = x
		}
	}
	return c
}

func (a *App) savePowerConfig(c powerCfg) {
	b, _ := json.Marshal(c)
	a.Config.SetSetting("power.config", string(b))
}

func (c powerCfg) setup() netd.UPSSetup {
	return netd.UPSSetup{Source: c.Source, Host: c.Host, Name: c.Name}
}

// powerLine is one thing shown on the Overview list while it is true.
type powerLine struct {
	ID     string `json:"id"`
	Level  string `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Test   bool   `json:"test,omitempty"`
}

type powerRun struct {
	mu       sync.Mutex
	last     netd.UPSStatus
	have     bool
	outage   time.Time // when the current power cut began; zero while on mains
	lowFired bool
	silent   bool
	fails    int
	lines    map[string]powerLine
	shutAt   time.Time // when a clean shutdown is due; zero when none is waiting
	shutWhy  string
	cancel   chan struct{}
	testing  bool
}

// How long the test waits between the steps it plays, and what one second means for the shutdown
// delay. Both are variables so the tests do not have to wait.
var (
	powerTestPause = 4 * time.Second
	powerSecond    = time.Second
	powerPoll      = 15 * time.Second
)

func outageWord(d time.Duration) string {
	switch {
	case d < 90*time.Second:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	case d < 90*time.Minute:
		return fmt.Sprintf("%d minutes", int(d.Minutes()+0.5))
	}
	return fmt.Sprintf("%.1f hours", d.Hours())
}

func batteryWord(st netd.UPSStatus) string {
	switch {
	case st.Charge >= 0 && st.Runtime >= 0:
		return fmt.Sprintf("%d%% charge, about %s left", st.Charge, outageWord(time.Duration(st.Runtime)*time.Second))
	case st.Charge >= 0:
		return fmt.Sprintf("%d%% charge", st.Charge)
	}
	return "charge not reported"
}

// powerEvent does what is chosen for one event: log it, alert, show it on the Overview, shut down.
// A test does all of that too, marked as a test, but never shuts the machine down unless real is set.
func (a *App) powerEvent(id string, st netd.UPSStatus, test, real bool, extra string) {
	cfg := a.powerConfig()
	act := cfg.Events[id]
	title, body, sev, level := "", "", "info", ""
	switch id {
	case "cut":
		title, sev, level = "Power cut", "warning", "warn"
		body = "The power has failed. NopeWarden is running on the UPS battery (" + batteryWord(st) + ")."
	case "low":
		title, sev, level = "UPS battery low", "error", "bad"
		body = "The UPS battery is low (" + batteryWord(st) + ")."
	case "replace":
		title, sev, level = "UPS battery needs replacing", "warning", "warn"
		body = "The UPS says its battery is worn out, so it may not keep the machine running through a power cut."
	case "back":
		title, sev = "Power is back", "info"
		body = extra
	case "silent":
		title, sev, level = "UPS not responding", "warning", "warn"
		body = "NopeWarden cannot reach the UPS. If the power fails now, it would not know."
	default:
		return
	}
	shown := title
	if test {
		shown = "Test: " + title
	}
	if act.Shutdown && (id == "cut" || id == "low" || id == "silent") {
		if test && !real {
			body += fmt.Sprintf(" This is a test, so the machine would have shut down in %d seconds.", cfg.Delay)
		} else {
			body += fmt.Sprintf(" NopeWarden will shut down cleanly in %d seconds.", cfg.Delay)
		}
	}
	if test {
		body = "This is a test, no real power cut. " + body
	}
	if act.Log {
		a.event("power", sev, shown, body)
	}
	if act.Alert {
		a.alert("power", shown, body)
	}
	a.power.mu.Lock()
	if a.power.lines == nil {
		a.power.lines = map[string]powerLine{}
	}
	switch id {
	case "back":
		delete(a.power.lines, "cut")
		delete(a.power.lines, "low")
	default:
		if act.Overview && level != "" {
			a.power.lines[id] = powerLine{ID: id, Level: level, Title: shown, Detail: body, Test: test}
		}
	}
	a.power.mu.Unlock()
	if act.Shutdown && (id == "cut" || id == "low" || id == "silent") && (!test || real) {
		a.powerShutdownSoon(shown, time.Duration(cfg.Delay)*powerSecond, cfg.CutOutput)
	}
}

// powerShutdownSoon waits the delay and then shuts down cleanly, unless it is cancelled first (the
// power came back, or someone pressed Cancel). One waiting shutdown at a time.
func (a *App) powerShutdownSoon(why string, delay time.Duration, cut bool) {
	a.power.mu.Lock()
	if !a.power.shutAt.IsZero() {
		a.power.mu.Unlock()
		return
	}
	ch := make(chan struct{})
	a.power.cancel, a.power.shutAt, a.power.shutWhy = ch, time.Now().Add(delay), why
	a.power.mu.Unlock()
	go func() {
		select {
		case <-ch:
			return
		case <-time.After(delay):
		}
		a.power.mu.Lock()
		a.power.shutAt = time.Time{}
		a.power.mu.Unlock()
		a.event("power", "warning", "Shutting down cleanly", why+". The UPS will be told to start the machine again when the power returns.")
		if err := a.Net.UPSShutdown(cut); err != nil {
			a.event("power", "error", "The shutdown did not start", err.Error())
		}
	}()
}

// powerCancelShutdown stops a waiting shutdown. It reports whether there was one.
func (a *App) powerCancelShutdown(by string) bool {
	a.power.mu.Lock()
	defer a.power.mu.Unlock()
	if a.power.shutAt.IsZero() {
		return false
	}
	close(a.power.cancel)
	a.power.shutAt = time.Time{}
	a.event("power", "info", "Shutdown cancelled", by)
	return true
}

// powerStep takes one reading of the UPS and acts on what changed.
func (a *App) powerStep(st netd.UPSStatus, now time.Time) {
	cfg := a.powerConfig()
	a.power.mu.Lock()
	a.power.last, a.power.have = st, true
	if a.power.testing {
		a.power.mu.Unlock()
		return
	}
	if !st.Answering {
		a.power.fails++
		first := a.power.fails >= 4 && !a.power.silent // four readings in a row, about a minute
		if first {
			a.power.silent = true
		}
		a.power.mu.Unlock()
		if first {
			a.powerEvent("silent", st, false, false, "")
		}
		return
	}
	a.power.fails = 0
	wasSilent := a.power.silent
	a.power.silent = false
	if wasSilent {
		delete(a.power.lines, "silent")
	}
	outage := a.power.outage
	if outage.IsZero() {
		if s := a.setting("power.outage", ""); s != "" { // the machine restarted during or after a cut
			var t int64
			fmt.Sscan(s, &t)
			if t > 0 {
				outage = time.Unix(t, 0)
				a.power.outage = outage
			}
		}
	}
	cut := st.OnBattery && outage.IsZero()
	back := !st.OnBattery && !outage.IsZero()
	low := st.OnBattery && !a.power.lowFired &&
		(st.LowBattery || (st.Charge >= 0 && st.Charge <= cfg.LowCharge) || (st.Runtime >= 0 && st.Runtime <= cfg.LowMinutes*60))
	if cut {
		a.power.outage = now
		outage = now
	}
	if low {
		a.power.lowFired = true
	}
	if back {
		a.power.outage = time.Time{}
		a.power.lowFired = false
	}
	if !st.Replace {
		delete(a.power.lines, "replace")
	}
	a.power.mu.Unlock()

	if wasSilent {
		a.event("power", "info", "UPS is answering again", "")
	}
	if cut {
		a.Config.SetSetting("power.outage", fmt.Sprint(now.Unix()))
		a.Logs.Exec(`INSERT INTO power_outages(start,end,test) VALUES(?,0,0)`, now.Unix())
		a.powerEvent("cut", st, false, false, "")
	}
	if low {
		a.powerEvent("low", st, false, false, "")
	}
	if back {
		a.Config.SetSetting("power.outage", "")
		a.Logs.Exec(`UPDATE power_outages SET end=? WHERE end=0 AND test=0`, now.Unix())
		if a.powerCancelShutdown("The power came back before the shutdown.") {
			a.event("power", "info", "Power came back in time", "The clean shutdown was cancelled.")
		}
		a.powerEvent("back", st, false, false, "Power was out for "+outageWord(now.Sub(outage))+".")
	}
	if st.Replace {
		at, _ := time.Parse(time.RFC3339, a.setting("power.replace_at", ""))
		if now.Sub(at) >= 24*time.Hour {
			a.Config.SetSetting("power.replace_at", now.Format(time.RFC3339))
			a.powerEvent("replace", st, false, false, "")
		} else {
			a.power.mu.Lock()
			if act := cfg.Events["replace"]; act.Overview {
				a.power.lines["replace"] = powerLine{ID: "replace", Level: "warn", Title: "UPS battery needs replacing", Detail: "The UPS says its battery is worn out."}
			}
			a.power.mu.Unlock()
		}
	}
}

func (a *App) powerLoop(ctx context.Context) {
	t := time.NewTimer(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cfg := a.powerConfig(); cfg.On && a.Net != nil {
				a.powerStep(a.Net.UPSStatus(cfg.setup()), time.Now())
			}
			t.Reset(powerPoll)
		}
	}
}

// powerTest plays a power cut, low battery and power back inside NopeWarden. The alerts and log lines
// are real and marked as a test. The machine shuts down only when real is set, and then the test
// stops after the low battery so the shutdown can run (and be cancelled).
func (a *App) powerTest(real bool) error {
	cfg := a.powerConfig()
	if !cfg.On {
		return errors.New("set up a UPS first")
	}
	a.power.mu.Lock()
	if a.power.testing {
		a.power.mu.Unlock()
		return errors.New("a test is already running")
	}
	a.power.testing = true
	a.power.mu.Unlock()
	st := netd.UPSStatus{Answering: true, Charge: 84, Runtime: 22 * 60, Load: 31, OnBattery: true, Raw: "OB DISCHRG"}
	go func() {
		defer func() {
			a.power.mu.Lock()
			a.power.testing = false
			a.power.mu.Unlock()
		}()
		started := time.Now()
		a.powerEvent("cut", st, true, real, "")
		time.Sleep(powerTestPause)
		st.Charge, st.Runtime, st.LowBattery = cfg.LowCharge, cfg.LowMinutes*60, true
		a.powerEvent("low", st, true, real, "")
		if real {
			return
		}
		time.Sleep(powerTestPause)
		st.OnBattery, st.LowBattery, st.Charge, st.Runtime = false, false, 100, 40*60
		a.powerEvent("back", st, true, false, "Power was out for "+outageWord(time.Since(started))+".")
	}()
	return nil
}

// powerSummaryLines is what the Daily summary says about the power.
func (a *App) powerSummaryLines(from, to int64) []string {
	if !a.powerConfig().On || a.Logs == nil {
		return nil
	}
	var n int
	var down int64
	if rows, err := a.Logs.Query(`SELECT start,end FROM power_outages WHERE test=0 AND start<? AND (end>? OR end=0)`, to, from); err == nil {
		for rows.Next() {
			var s, e int64
			rows.Scan(&s, &e)
			if e == 0 || e > to {
				e = to
			}
			if s < from {
				s = from
			}
			n++
			down += e - s
		}
		rows.Close()
	}
	var out []string
	if n == 0 {
		out = append(out, "The power stayed on all day yesterday.")
	} else {
		out = append(out, fmt.Sprintf("The power failed %d time%s yesterday, %s in total.", n, plural1(n), outageWord(time.Duration(down)*time.Second)))
	}
	a.power.mu.Lock()
	if a.power.last.Replace {
		out = append(out, "The UPS battery needs replacing.")
	}
	a.power.mu.Unlock()
	return out
}

// powerOneLine is a short line about the UPS for other screens, empty when none is watched.
func (a *App) powerOneLine() string {
	if !a.powerConfig().On {
		return ""
	}
	a.power.mu.Lock()
	defer a.power.mu.Unlock()
	st := a.power.last
	switch {
	case !a.power.have:
		return ""
	case !st.Answering:
		return "The UPS is not answering, so a power cut would go unnoticed."
	case st.OnBattery:
		return "The power is out and the machine is running on the UPS battery (" + batteryWord(st) + ")."
	}
	return "The machine is on mains power, with the UPS battery at " + batteryWord(st) + "."
}

func plural1(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

type powerView struct {
	Config  powerCfg           `json:"config"`
	Events  any                `json:"events"`
	Status  netd.UPSStatus     `json:"status"`
	Have    bool               `json:"have"`
	Lines   []powerLine        `json:"lines"`
	Alerts  diskAlerts         `json:"alerts"`
	ShutAt  int64              `json:"shutdown_at"` // when a clean shutdown is due, 0 when none
	ShutWhy string             `json:"shutdown_why"`
	Testing bool               `json:"testing"`
	Past    []map[string]int64 `json:"past"` // recent cuts: start, end
}

func (a *App) powerViewNow() powerView {
	cfg := a.powerConfig()
	c := a.alertConfig()
	v := powerView{Config: cfg, Events: powerEvents, Alerts: diskAlerts{On: c.On["power"], Channel: c.Email.Enabled || c.Ntfy.Enabled}, Lines: []powerLine{}}
	a.power.mu.Lock()
	stale := !a.power.have || time.Since(time.Unix(a.power.last.At, 0)) > time.Minute
	a.power.mu.Unlock()
	if cfg.On && stale && a.Net != nil {
		st := a.Net.UPSStatus(cfg.setup())
		a.power.mu.Lock()
		a.power.last, a.power.have = st, true
		a.power.mu.Unlock()
	} else if !cfg.On && a.Net != nil && stale {
		st := a.Net.UPSStatus(cfg.setup()) // lets the card say whether the UPS tool is installed
		a.power.mu.Lock()
		a.power.last, a.power.have = st, true
		a.power.mu.Unlock()
	}
	a.power.mu.Lock()
	v.Status, v.Have, v.Testing = a.power.last, a.power.have, a.power.testing
	if cfg.On {
		for _, e := range powerEvents {
			if l, ok := a.power.lines[e.ID]; ok {
				v.Lines = append(v.Lines, l)
			}
		}
	}
	if !a.power.shutAt.IsZero() {
		v.ShutAt, v.ShutWhy = a.power.shutAt.Unix(), a.power.shutWhy
	}
	a.power.mu.Unlock()
	v.Past = []map[string]int64{}
	if a.Logs != nil {
		if rows, err := a.Logs.Query(`SELECT start,end FROM power_outages WHERE test=0 ORDER BY start DESC LIMIT 5`); err == nil {
			for rows.Next() {
				var s, e int64
				rows.Scan(&s, &e)
				v.Past = append(v.Past, map[string]int64{"start": s, "end": e})
			}
			rows.Close()
		}
	}
	return v
}

func (a *App) registerUPSRoutes() {
	s := a.API
	s.Handle("GET /api/power", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.powerViewNow())
	})
	// Where the UPS is: installs the UPS software if needed and starts watching.
	s.HandleAdmin("POST /api/power/setup", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in netd.UPSSetup
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.Net.UPSSetupApply(in); err != nil {
			api.Error(w, 400, err.Error())
			return
		}
		cfg := a.powerConfig()
		cfg.On, cfg.Source, cfg.Host, cfg.Name = true, in.Source, in.Host, in.Name
		a.savePowerConfig(cfg)
		a.power.mu.Lock()
		a.power.have = false
		a.power.mu.Unlock()
		a.auditEvent(c.User.Username, "info", "Set up UPS monitoring", in.Source)
		api.JSON(w, 200, a.powerViewNow())
	})
	s.HandleAdmin("POST /api/power/off", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		cfg := a.powerConfig()
		cfg.On = false
		a.savePowerConfig(cfg)
		a.power.mu.Lock()
		a.power.lines = map[string]powerLine{}
		a.power.mu.Unlock()
		a.powerCancelShutdown("UPS monitoring was switched off by " + c.User.Username + ".")
		a.auditEvent(c.User.Username, "info", "Switched UPS monitoring off", "")
		api.JSON(w, 200, a.powerViewNow())
	})
	// What to do for each event, and the levels that count as low.
	s.HandleAdmin("PUT /api/power/config", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in powerCfg
		if !api.Decode(w, r, &in) {
			return
		}
		cfg := a.powerConfig()
		if in.LowCharge < 1 || in.LowCharge > 90 {
			api.Error(w, 400, "the low charge should be between 1 and 90 percent")
			return
		}
		if in.LowMinutes < 1 || in.LowMinutes > 60 {
			api.Error(w, 400, "the time left should be between 1 and 60 minutes")
			return
		}
		if in.Delay < 0 || in.Delay > 600 {
			api.Error(w, 400, "the shutdown delay should be between 0 and 600 seconds")
			return
		}
		cfg.LowCharge, cfg.LowMinutes, cfg.Delay, cfg.CutOutput = in.LowCharge, in.LowMinutes, in.Delay, in.CutOutput
		for _, e := range powerEvents {
			if act, ok := in.Events[e.ID]; ok {
				cfg.Events[e.ID] = act
			}
		}
		a.savePowerConfig(cfg)
		a.auditEvent(c.User.Username, "info", "Changed what happens when the power fails", "")
		api.JSON(w, 200, a.powerViewNow())
	})
	s.HandleAdmin("POST /api/power/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Shutdown bool `json:"shutdown"`
		}
		api.Decode(w, r, &in)
		if err := a.powerTest(in.Shutdown); err != nil {
			api.Error(w, 400, err.Error())
			return
		}
		a.auditEvent(c.User.Username, "info", "Played a test power cut", fmt.Sprintf("shutdown allowed: %v", in.Shutdown))
		api.JSON(w, 200, map[string]bool{"ok": true})
	})
	s.HandleAdmin("POST /api/power/cancel", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if !a.powerCancelShutdown("Cancelled by " + c.User.Username + ".") {
			api.Error(w, 400, "no shutdown is waiting")
			return
		}
		api.JSON(w, 200, a.powerViewNow())
	})
}

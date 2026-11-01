package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/threatcfg"
)

// The owner-facing side of threat watching: install on request, presets, rule updates, limits, and
// a watchdog that switches the engine off if it uses too much of the machine.
//
// Fail open: the engine only reads copies of packets. Whatever it does, traffic is not touched.

const (
	threatPresetSetting = "threat.preset" // light, balanced, strict
	threatCPUSetting    = "threat.cpu"    // processor limit, % of one core
	threatMemSetting    = "threat.mem"    // memory limit, MB
	threatOffSetting    = "threat.off"    // single rules switched off, comma-separated numbers
	threatWhySetting    = "threat.paused" // why Gatehouse switched it off by itself, shown until dismissed
	threatRulesFile     = "/var/lib/suricata/rules/suricata.rules"
)

var threatRulesPath = threatRulesFile

func (a *App) threatPreset() string {
	p := a.Config.Setting(threatPresetSetting, "balanced")
	for _, x := range threatcfg.Presets() {
		if x.Key == p {
			return p
		}
	}
	return "balanced"
}

func (a *App) threatInt(key string, def, lo, hi int) int {
	n, err := strconv.Atoi(a.Config.Setting(key, ""))
	if err != nil || n < lo || n > hi {
		return def
	}
	return n
}

func (a *App) threatOffSIDs() []int {
	var out []int
	for _, f := range strings.Split(a.Config.Setting(threatOffSetting, ""), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func (a *App) setThreatOffSIDs(ids []int) {
	sort.Ints(ids)
	var parts []string
	last := 0
	for _, n := range ids {
		if n != last && n > 0 {
			parts = append(parts, strconv.Itoa(n))
			last = n
		}
	}
	a.Config.SetSetting(threatOffSetting, strings.Join(parts, ","))
}

func (a *App) threatPlan() threatcfg.Plan {
	return threatcfg.Plan{
		Enabled: a.threatMode() == "watch", Ifaces: a.ndpiIfaces(), Preset: a.threatPreset(),
		CPUPercent: a.threatInt(threatCPUSetting, 50, 10, 400), MemoryMB: a.threatInt(threatMemSetting, 2048, 256, 16384),
		OffSIDs: a.threatOffSIDs(),
	}
}

// threatSync makes the engine match the settings.
func (a *App) threatSync() error {
	if a.Net == nil {
		return errNoHelper
	}
	return a.Net.ThreatApply(a.threatPlan())
}

// threatPause switches watching off by itself, and says why.
func (a *App) threatPause(why string) {
	a.Config.SetSetting(threatModeSetting, "off")
	a.Config.SetSetting(threatWhySetting, why)
	if a.Net != nil {
		a.Net.ThreatApply(threatcfg.Plan{Enabled: false})
	}
	a.event("system", "warning", "Threat watching was switched off", why+" Your traffic was never held up. You can switch it back on under Protection.")
}

type threatWatch struct {
	mu      sync.Mutex
	lastNS  uint64
	lastAt  time.Time
	hot     int
	stopped int
}

// threatWatchTick looks at the engine once a minute. If it has used its whole processor limit for ten
// minutes running, watching is switched off and the Logs say why. If it was meant to be running and
// is not, the Logs say so once. Neither touches traffic.
func (a *App) threatWatchTick(info netdThreat, now time.Time) {
	w := &a.threatW
	w.mu.Lock()
	defer w.mu.Unlock()
	if a.threatMode() != "watch" || !info.Installed {
		w.hot, w.stopped, w.lastAt = 0, 0, time.Time{}
		return
	}
	if !info.Running {
		w.hot, w.lastAt = 0, time.Time{}
		w.stopped++
		if w.stopped == 3 {
			a.event("system", "warning", "Threat watching is not running", "The engine stopped. Your traffic is not affected, because it only watches. Gatehouse keeps trying to start it.")
		}
		return
	}
	w.stopped = 0
	if !w.lastAt.IsZero() && info.CPUNSec >= w.lastNS {
		secs := now.Sub(w.lastAt).Seconds()
		if secs > 0 {
			pct := float64(info.CPUNSec-w.lastNS) / 1e9 / secs * 100
			limit := float64(a.threatInt(threatCPUSetting, 50, 10, 400))
			if pct >= limit*0.9 {
				w.hot++
			} else {
				w.hot = 0
			}
		}
	}
	w.lastNS, w.lastAt = info.CPUNSec, now
	if w.hot >= 10 {
		w.hot = 0
		a.threatPause(fmt.Sprintf("It kept using all of its %d%% share of a processor for ten minutes.", a.threatInt(threatCPUSetting, 50, 10, 400)))
	}
}

func (a *App) threatWatchLoop(ctx context.Context) {
	defer func() { recover() }()
	// A moment after start, make sure the engine matches the settings.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	if a.threatMode() == "watch" && a.Net != nil && a.Net.ThreatStatus().Installed {
		a.threatSync()
	}
	last := time.Now()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		if a.Net == nil {
			continue
		}
		a.threatWatchTick(a.Net.ThreatStatus(), time.Now())
		// rules refresh once a day while watching
		if a.threatMode() == "watch" && time.Since(last) >= 24*time.Hour {
			last = time.Now()
			if err := a.Net.ThreatUpdateRules(); err != nil {
				a.event("system", "info", "Threat rules could not be updated", err.Error())
			}
		}
	}
}

var reRuleSID = regexp.MustCompile(`sid:\s*(\d+)\s*;`)
var reRuleMsg = regexp.MustCompile(`msg:\s*"([^"]*)"`)

type threatRule struct {
	SID  int    `json:"sid"`
	Msg  string `json:"msg"`
	On   bool   `json:"on"`
	Hits int    `json:"hits"`
}

// threatSearchRules finds single rules by words in their message or by number.
func (a *App) threatSearchRules(q string, limit int) ([]threatRule, int) {
	f, err := os.Open(threatRulesPath)
	if err != nil {
		return []threatRule{}, 0
	}
	defer f.Close()
	q = strings.ToLower(strings.TrimSpace(q))
	off := map[int]bool{}
	for _, n := range a.threatOffSIDs() {
		off[n] = true
	}
	hits := map[int]int{}
	if rows, err := a.Logs.Query(`SELECT sid,COUNT(*) FROM threat_alerts GROUP BY sid`); err == nil {
		for rows.Next() {
			var sid, n int
			rows.Scan(&sid, &n)
			hits[sid] = n
		}
		rows.Close()
	}
	out := []threatRule{}
	total := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "# ")
		sid := reRuleSID.FindStringSubmatch(line)
		if sid == nil {
			continue
		}
		msg := ""
		if m := reRuleMsg.FindStringSubmatch(line); m != nil {
			msg = m[1]
		}
		n, _ := strconv.Atoi(sid[1])
		if q != "" && !strings.Contains(strings.ToLower(msg), q) && sid[1] != q {
			continue
		}
		total++
		if len(out) < limit {
			out = append(out, threatRule{SID: n, Msg: msg, On: !off[n], Hits: hits[n]})
		}
	}
	return out, total
}

type netdThreat = struct {
	Installed    bool   `json:"installed"`
	Version      string `json:"version"`
	Running      bool   `json:"running"`
	State        string `json:"state"`
	Message      string `json:"message"`
	DiskFreeMB   int64  `json:"disk_free_mb"`
	MemoryMB     int64  `json:"memory_mb"`
	CPUs         int    `json:"cpus"`
	RulesUpdated int64  `json:"rules_updated"`
	RulesCount   int    `json:"rules_count"`
	Why          string `json:"why"`
	HasPrevious  bool   `json:"has_previous"`
	CPUNSec      uint64 `json:"cpu_nsec"`
	MemoryBytes  uint64 `json:"memory_bytes"`
}

func (a *App) threatInfo() netdThreat {
	if a.Net == nil {
		return netdThreat{State: "idle"}
	}
	return netdThreat(a.Net.ThreatStatus())
}

func (a *App) registerThreatEngineRoutes() {
	s := a.API
	s.Handle("GET /api/threat", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		type alert struct {
			ID        int64  `json:"id"`
			TS        int64  `json:"ts"`
			SID       int64  `json:"sid"`
			Signature string `json:"signature"`
			Plain     string `json:"plain"`
			Device    string `json:"device"`
			MAC       string `json:"mac"`
			Src       string `json:"src"`
			Dst       string `json:"dst"`
			Severity  int    `json:"severity"`
		}
		alerts := []alert{}
		if rows, err := a.Logs.Query(`SELECT id,ts,sid,signature,plain,device,mac,src,dst,severity FROM threat_alerts ORDER BY id DESC LIMIT 50`); err == nil {
			for rows.Next() {
				var x alert
				rows.Scan(&x.ID, &x.TS, &x.SID, &x.Signature, &x.Plain, &x.Device, &x.MAC, &x.Src, &x.Dst, &x.Severity)
				alerts = append(alerts, x)
			}
			rows.Close()
		}
		type allowed struct {
			ID     int64  `json:"id"`
			SID    int64  `json:"sid"`
			MAC    string `json:"mac"`
			Device string `json:"device"`
			Until  int64  `json:"until"`
			Note   string `json:"note"`
			Auto   bool   `json:"auto"`
		}
		al := []allowed{}
		if rows, err := a.Config.Query(`SELECT id,sid,mac,until,note,auto FROM threat_allowed ORDER BY id DESC`); err == nil {
			for rows.Next() {
				var x allowed
				var au int
				rows.Scan(&x.ID, &x.SID, &x.MAC, &x.Until, &x.Note, &au)
				x.Auto = au == 1
				if x.MAC != "" {
					x.Device = a.fwdDeviceName(x.MAC, "")
				}
				al = append(al, x)
			}
			rows.Close()
		}
		type trusted struct {
			MAC    string `json:"mac"`
			Device string `json:"device"`
		}
		tr := []trusted{}
		if rows, err := a.Config.Query(`SELECT mac FROM threat_trusted ORDER BY mac`); err == nil {
			for rows.Next() {
				var m string
				rows.Scan(&m)
				tr = append(tr, trusted{m, a.fwdDeviceName(m, "")})
			}
			rows.Close()
		}
		type presetOut struct {
			Key   string `json:"key"`
			Title string `json:"title"`
			Text  string `json:"text"`
		}
		var ps []presetOut
		for _, p := range threatcfg.Presets() {
			ps = append(ps, presetOut{p.Key, p.Title, p.Text})
		}
		info := a.threatInfo()
		api.JSON(w, 200, map[string]any{
			"mode": a.threatMode(), "preset": a.threatPreset(), "presets": ps,
			"cpu_percent": a.threatInt(threatCPUSetting, 50, 10, 400), "memory_mb": a.threatInt(threatMemSetting, 2048, 256, 16384),
			"paused_reason": a.Config.Setting(threatWhySetting, ""),
			"program":       info, "engine": a.threatEngine(), "eve_path": a.threatEvePath(), "default_eve": defaultEve,
			"ports": a.ndpiIfaces(), "alerts": alerts, "allowed": al, "trusted": tr,
			"blocking":  "Blocking is not switched on. Gatehouse watches and tells you; it never holds up your traffic.",
			"fail_open": "If the program stops, crashes or is removed, your traffic is not touched. It only reads copies of what passes by, and Gatehouse switches it off by itself if it uses too much of the machine.",
			"limits":    "Most traffic is encrypted, so this finds known bad servers and attack patterns, not everything.",
		})
	})
	s.HandleAdmin("POST /api/threat/install/preview", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		p, err := a.Net.ThreatInstallPreview()
		if err != nil {
			fail(w, err)
			return
		}
		info := a.threatInfo()
		warns := []string{}
		if need := int64(p.DiskMB) + 200; info.DiskFreeMB > 0 && info.DiskFreeMB < need {
			warns = append(warns, fmt.Sprintf("There is only %d MB of free disk space, and about %d MB is needed.", info.DiskFreeMB, need))
		}
		if info.MemoryMB > 0 && info.MemoryMB < 2048 {
			warns = append(warns, fmt.Sprintf("This machine has %d MB of memory. Watching works best with 2 GB or more; you can lower its memory limit under Advanced.", info.MemoryMB))
		}
		if info.CPUs > 0 && info.CPUs < 2 {
			warns = append(warns, "This machine has one processor core. Watching could slow it down; Gatehouse limits it and switches it off if it struggles.")
		}
		api.JSON(w, 200, map[string]any{"preview": p, "warnings": warns, "disk_free_mb": info.DiskFreeMB, "memory_mb": info.MemoryMB, "cpus": info.CPUs})
	})
	s.HandleAdmin("POST /api/threat/install", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if err := a.Net.ThreatInstallStart(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Started installing the threat-watching program", "")
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/remove", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		a.Config.SetSetting(threatModeSetting, "off")
		if err := a.Net.ThreatRemove(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Removed the threat-watching program", "")
		ok(w)
	})
	s.HandleAdmin("PUT /api/threat/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Mode   *string `json:"mode"`
			Preset *string `json:"preset"`
			CPU    *int    `json:"cpu_percent"`
			Mem    *int    `json:"memory_mb"`
			Eve    *string `json:"eve_path"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Mode != nil && *in.Mode != "off" && *in.Mode != "watch" {
			fail(w, errors.New("choose Off or Watch and tell"))
			return
		}
		if in.Preset != nil {
			good := false
			for _, p := range threatcfg.Presets() {
				good = good || p.Key == *in.Preset
			}
			if !good {
				fail(w, errors.New("choose Light, Balanced or Strict"))
				return
			}
		}
		if in.CPU != nil && (*in.CPU < 10 || *in.CPU > 400) {
			fail(w, errors.New("the processor limit must be between 10% and 400% of one core"))
			return
		}
		if in.Mem != nil && (*in.Mem < 256 || *in.Mem > 16384) {
			fail(w, errors.New("the memory limit must be between 256 MB and 16 GB"))
			return
		}
		if in.Eve != nil {
			e := strings.TrimSpace(*in.Eve)
			if e != "" && (!strings.HasPrefix(e, "/") || strings.Contains(e, "..")) {
				fail(w, errBadPath)
				return
			}
		}
		if in.Mode != nil && *in.Mode == "watch" && !a.threatInfo().Installed {
			fail(w, errors.New("install the threat-watching program first"))
			return
		}
		if in.Preset != nil {
			a.Config.SetSetting(threatPresetSetting, *in.Preset)
		}
		if in.CPU != nil {
			a.Config.SetSetting(threatCPUSetting, strconv.Itoa(*in.CPU))
		}
		if in.Mem != nil {
			a.Config.SetSetting(threatMemSetting, strconv.Itoa(*in.Mem))
		}
		if in.Eve != nil {
			a.Config.SetSetting(threatEveSetting, strings.TrimSpace(*in.Eve))
		}
		if in.Mode != nil {
			a.Config.SetSetting(threatModeSetting, *in.Mode)
			a.Config.SetSetting(threatWhySetting, "")
		}
		if err := a.threatSync(); err != nil && a.threatInfo().Installed {
			// It could not be started: leave it off so the page does not claim it is on.
			if a.threatMode() == "watch" {
				a.Config.SetSetting(threatModeSetting, "off")
			}
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/rules/update", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if err := a.Net.ThreatUpdateRules(); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/rules/rollback", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if err := a.Net.ThreatRollback(); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	// Rule groups and single rules, for the Advanced tab.
	s.Handle("GET /api/threat/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		off := map[string]bool{}
		for _, p := range threatcfg.Presets() {
			if p.Key == a.threatPreset() {
				for _, g := range p.Off {
					off[g] = true
				}
			}
		}
		type g struct {
			Name string `json:"name"`
			Text string `json:"text"`
			On   bool   `json:"on"`
		}
		var out []g
		for _, x := range threatcfg.Groups() {
			out = append(out, g{x.Name, x.Text, !off[x.Name]})
		}
		api.JSON(w, 200, map[string]any{"groups": out, "note": "Which groups are on follows the preset you chose. Change the preset to change them."})
	})
	s.Handle("GET /api/threat/rules", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rules, total := a.threatSearchRules(r.URL.Query().Get("q"), 50)
		api.JSON(w, 200, map[string]any{"rules": rules, "total": total})
	})
	s.HandleAdmin("POST /api/threat/rules/{sid}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		sid, err := strconv.Atoi(r.PathValue("sid"))
		var in struct {
			On bool `json:"on"`
		}
		if err != nil || sid <= 0 || !api.Decode(w, r, &in) {
			if err != nil || sid <= 0 {
				fail(w, errBadID)
			}
			return
		}
		cur := a.threatOffSIDs()
		var next []int
		for _, n := range cur {
			if n != sid {
				next = append(next, n)
			}
		}
		if !in.On {
			next = append(next, sid)
		}
		a.setThreatOffSIDs(next)
		if a.threatMode() == "watch" {
			if err := a.threatSync(); err != nil {
				fail(w, err)
				return
			}
		}
		ok(w)
	})
	// Tuning can be saved and brought back, for example after a reset.
	s.Handle("GET /api/threat/export", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		type allow struct {
			SID   int64  `json:"sid"`
			MAC   string `json:"mac"`
			Until int64  `json:"until"`
			Note  string `json:"note"`
		}
		var al []allow
		if rows, err := a.Config.Query(`SELECT sid,mac,until,note FROM threat_allowed WHERE auto=0`); err == nil {
			for rows.Next() {
				var x allow
				rows.Scan(&x.SID, &x.MAC, &x.Until, &x.Note)
				al = append(al, x)
			}
			rows.Close()
		}
		var tr []string
		if rows, err := a.Config.Query(`SELECT mac FROM threat_trusted`); err == nil {
			for rows.Next() {
				var m string
				rows.Scan(&m)
				tr = append(tr, m)
			}
			rows.Close()
		}
		out, _ := json.MarshalIndent(map[string]any{"gatehouse_threat_tuning": 1, "preset": a.threatPreset(), "cpu_percent": a.threatInt(threatCPUSetting, 50, 10, 400),
			"memory_mb": a.threatInt(threatMemSetting, 2048, 256, 16384), "off_sids": a.threatOffSIDs(), "allowed": al, "trusted": tr}, "", "  ")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="gatehouse-threat-tuning.json"`)
		w.Write(out)
	})
	s.HandleAdmin("POST /api/threat/import", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Version int      `json:"gatehouse_threat_tuning"`
			Preset  string   `json:"preset"`
			CPU     int      `json:"cpu_percent"`
			Mem     int      `json:"memory_mb"`
			Off     []int    `json:"off_sids"`
			Trusted []string `json:"trusted"`
			Allowed []struct {
				SID   int64  `json:"sid"`
				MAC   string `json:"mac"`
				Until int64  `json:"until"`
				Note  string `json:"note"`
			} `json:"allowed"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Version != 1 {
			fail(w, errors.New("that is not a Gatehouse threat tuning file"))
			return
		}
		plan := threatcfg.Plan{Preset: in.Preset, CPUPercent: in.CPU, MemoryMB: in.Mem, OffSIDs: in.Off}
		if err := plan.Check(); err != nil {
			fail(w, err)
			return
		}
		a.Config.SetSetting(threatPresetSetting, plan.Preset)
		a.Config.SetSetting(threatCPUSetting, strconv.Itoa(plan.CPUPercent))
		a.Config.SetSetting(threatMemSetting, strconv.Itoa(plan.MemoryMB))
		a.setThreatOffSIDs(in.Off)
		for _, m := range in.Trusted {
			a.Config.Exec(`INSERT OR IGNORE INTO threat_trusted(mac) VALUES(?)`, strings.ToLower(m))
		}
		for _, x := range in.Allowed {
			if x.SID > 0 {
				a.Config.Exec(`INSERT INTO threat_allowed(sid,mac,until,note) VALUES(?,?,?,?)`, x.SID, strings.ToLower(x.MAC), x.Until, x.Note)
			}
		}
		if a.threatMode() == "watch" {
			a.threatSync()
		}
		ok(w)
	})
}

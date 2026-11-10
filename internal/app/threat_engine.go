package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"nopewarden/internal/sched"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/netd"
	"nopewarden/internal/threatcfg"
)

// The owner-facing side of threat watching: install on request, presets, rule updates, limits, and
// a watchdog that switches the engine off if it uses too much of the machine.
//
// Fail open: the engine only reads copies of packets. Whatever it does, traffic is not touched.

const (
	threatPresetSetting = "threat.preset"     // light, balanced, strict
	threatCPUSetting    = "threat.cpu"        // processor limit, % of one core
	threatMemSetting    = "threat.mem"        // memory limit, MB
	threatOffSetting    = "threat.off"        // single rules switched off, comma-separated numbers
	threatGroupsOnSet   = "threat.groups_on"  // groups switched on although the preset has them off
	threatGroupsOffSet  = "threat.groups_off" // groups switched off although the preset has them on
	threatWhySetting    = "threat.paused"     // why NopeWarden switched it off by itself, shown until dismissed
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

// threatGroupChanges are the person's changes to the preset's groups (custom setup).
func (a *App) threatGroupChanges() (on, off []string) {
	read := func(key string) []string {
		var out []string
		for _, f := range strings.Split(a.Config.Setting(key, ""), ",") {
			if f = strings.TrimSpace(f); f != "" && threatcfg.KnownGroup(f) {
				out = append(out, f)
			}
		}
		return out
	}
	return read(threatGroupsOnSet), read(threatGroupsOffSet)
}

func (a *App) setThreatGroupChanges(on, off []string) {
	a.Config.SetSetting(threatGroupsOnSet, strings.Join(on, ","))
	a.Config.SetSetting(threatGroupsOffSet, strings.Join(off, ","))
}

func (a *App) threatPlan() threatcfg.Plan {
	on, off := a.threatGroupChanges()
	return threatcfg.Plan{
		Enabled: a.threatMode() == "watch", Ifaces: a.ndpiIfaces(), Preset: a.threatPreset(),
		CPUPercent: a.threatInt(threatCPUSetting, 50, 10, 400), MemoryMB: a.threatInt(threatMemSetting, 2048, 256, 16384),
		OffSIDs: a.threatOffSIDs(), GroupsOn: on, GroupsOff: off,
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
	// What the Overview tab shows: when NopeWarden first saw it running, and its latest load.
	runningSince time.Time
	cpuPct       float64
	memMB        int64
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
		w.runningSince, w.cpuPct, w.memMB = time.Time{}, 0, 0
		return
	}
	if !info.Running {
		w.hot, w.lastAt = 0, time.Time{}
		w.runningSince, w.cpuPct, w.memMB = time.Time{}, 0, 0
		w.stopped++
		if w.stopped == 3 {
			a.event("system", "warning", "Threat watching is not running", "The engine stopped. Your traffic is not affected, because it only watches. NopeWarden keeps trying to start it.")
		}
		return
	}
	w.stopped = 0
	if w.runningSince.IsZero() {
		w.runningSince = now
	}
	w.memMB = int64(info.MemoryBytes / (1024 * 1024))
	if !w.lastAt.IsZero() && info.CPUNSec >= w.lastNS {
		secs := now.Sub(w.lastAt).Seconds()
		if secs > 0 {
			pct := float64(info.CPUNSec-w.lastNS) / 1e9 / secs * 100
			w.cpuPct = pct
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
	last, failN := time.Time{}, 0
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
		// rules refresh on the shared schedule while watching; after a failure wait before trying again
		if a.threatMode() == "watch" && a.schedDue("threat") && time.Since(last) >= sched.RetryDelay(failN) {
			last = time.Now()
			if err := a.threatRulesUpdate(); err != nil {
				failN++
				a.event("system", "info", "Threat rules could not be updated", err.Error())
			} else {
				failN = 0
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
	Installed    bool              `json:"installed"`
	Version      string            `json:"version"`
	Running      bool              `json:"running"`
	State        string            `json:"state"`
	Message      string            `json:"message"`
	Steps        []netd.ThreatStep `json:"steps,omitempty"`
	DiskFreeMB   int64             `json:"disk_free_mb"`
	MemoryMB     int64             `json:"memory_mb"`
	CPUs         int               `json:"cpus"`
	RulesUpdated int64             `json:"rules_updated"`
	RulesCount   int               `json:"rules_count"`
	Why          string            `json:"why"`
	Failing      bool              `json:"failing"`
	HasPrevious  bool              `json:"has_previous"`
	CPUNSec      uint64            `json:"cpu_nsec"`
	MemoryBytes  uint64            `json:"memory_bytes"`
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
		gon, goff := a.threatGroupChanges()
		api.JSON(w, 200, map[string]any{
			"mode": a.threatMode(), "preset": a.threatPreset(), "presets": ps, "custom": len(gon)+len(goff) > 0, "changes": len(gon) + len(goff),
			"cpu_percent": a.threatInt(threatCPUSetting, 50, 10, 400), "memory_mb": a.threatInt(threatMemSetting, 2048, 256, 16384),
			"paused_reason": a.Config.Setting(threatWhySetting, ""),
			"program":       info, "engine": a.threatEngine(), "eve_path": a.threatEvePath(), "default_eve": defaultEve,
			"ports": a.ndpiIfaces(), "alerts": a.threatGroups(60), "allowed": al, "trusted": tr,
			"blocking":  "Blocking is not switched on. NopeWarden watches and tells you; it never holds up your traffic.",
			"fail_open": "If the program stops, crashes or is removed, your traffic is not touched. It only reads copies of what passes by, and NopeWarden switches it off by itself if it uses too much of the machine.",
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
			warns = append(warns, "This machine has one processor core. Watching could slow it down; NopeWarden limits it and switches it off if it struggles.")
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
			a.setThreatGroupChanges(nil, nil) // choosing a preset starts from that preset's own groups
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
		if err := a.threatRulesUpdate(); err != nil {
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
	// Rule groups. Which are on is the preset plus the person's changes.
	s.Handle("GET /api/threat/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		plan := a.threatPlan()
		base := map[string]bool{}
		for _, g := range threatcfg.PresetOff(plan.Preset) {
			base[g] = true
		}
		eff := map[string]bool{}
		for _, g := range threatcfg.EffectiveOff(plan) {
			eff[g] = true
		}
		type g struct {
			Name     string `json:"name"`
			Text     string `json:"text"`
			On       bool   `json:"on"`
			PresetOn bool   `json:"preset_on"` // what the chosen preset does with it
			Common   bool   `json:"common"`
			Changed  bool   `json:"changed"`
		}
		out := []g{}
		for _, x := range threatcfg.Groups() {
			out = append(out, g{x.Name, x.Text, !eff[x.Name], !base[x.Name], x.Common, eff[x.Name] != base[x.Name]})
		}
		title := plan.Preset
		for _, p := range threatcfg.Presets() {
			if p.Key == plan.Preset {
				title = p.Title
			}
		}
		api.JSON(w, 200, map[string]any{"groups": out, "preset": plan.Preset, "preset_title": title,
			"note": "Which groups are on follows the preset you chose, plus any changes you make here."})
	})
	// The groups the person wants off, as a whole. Only the differences from the preset are kept, so
	// a later improvement to a preset is not lost.
	s.HandleAdmin("PUT /api/threat/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Off []string `json:"off"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		want := map[string]bool{}
		for _, g := range in.Off {
			if !threatcfg.KnownGroup(g) {
				fail(w, errors.New("a group of checks is not known"))
				return
			}
			want[g] = true
		}
		on, off := threatcfg.Changes(a.threatPreset(), want)
		a.setThreatGroupChanges(on, off)
		if a.threatMode() == "watch" {
			if err := a.threatSync(); err != nil {
				fail(w, err)
				return
			}
		}
		a.event(c.User.Username, "info", "Changed the groups of threat checks", fmt.Sprintf("%d groups differ from the %s setting.", len(on)+len(off), a.threatPreset()))
		ok(w)
	})
	// Back to the defaults for everything threat watching: the setting, limits, groups, single
	// checks switched off, allowed checks and devices never checked. The program stays installed.
	s.HandleAdmin("POST /api/threat/reset", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.Config.SetSetting(threatPresetSetting, "balanced")
		a.Config.SetSetting(threatCPUSetting, "50")
		a.Config.SetSetting(threatMemSetting, "2048")
		a.setThreatOffSIDs(nil)
		a.setThreatGroupChanges(nil, nil)
		a.Config.Exec(`DELETE FROM threat_allowed`)
		a.Config.Exec(`DELETE FROM threat_trusted`)
		if a.threatMode() == "watch" {
			if err := a.threatSync(); err != nil {
				fail(w, err)
				return
			}
		}
		a.event(c.User.Username, "info", "Reset the threat settings to the defaults", "")
		ok(w)
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
		gon, goff := a.threatGroupChanges()
		out, _ := json.MarshalIndent(map[string]any{"nopewarden_threat_tuning": 1, "preset": a.threatPreset(), "cpu_percent": a.threatInt(threatCPUSetting, 50, 10, 400),
			"memory_mb": a.threatInt(threatMemSetting, 2048, 256, 16384), "off_sids": a.threatOffSIDs(), "groups_on": gon, "groups_off": goff, "allowed": al, "trusted": tr}, "", "  ")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="nopewarden-threat-tuning.json"`)
		w.Write(out)
	})
	s.HandleAdmin("POST /api/threat/import", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Version   int      `json:"nopewarden_threat_tuning"`
			Preset    string   `json:"preset"`
			CPU       int      `json:"cpu_percent"`
			Mem       int      `json:"memory_mb"`
			Off       []int    `json:"off_sids"`
			GroupsOn  []string `json:"groups_on"`
			GroupsOff []string `json:"groups_off"`
			Trusted   []string `json:"trusted"`
			Allowed   []struct {
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
			fail(w, errors.New("that is not a NopeWarden threat tuning file"))
			return
		}
		plan := threatcfg.Plan{Preset: in.Preset, CPUPercent: in.CPU, MemoryMB: in.Mem, OffSIDs: in.Off, GroupsOn: in.GroupsOn, GroupsOff: in.GroupsOff}
		if err := plan.Check(); err != nil {
			fail(w, err)
			return
		}
		a.Config.SetSetting(threatPresetSetting, plan.Preset)
		a.Config.SetSetting(threatCPUSetting, strconv.Itoa(plan.CPUPercent))
		a.Config.SetSetting(threatMemSetting, strconv.Itoa(plan.MemoryMB))
		a.setThreatOffSIDs(in.Off)
		a.setThreatGroupChanges(plan.GroupsOn, plan.GroupsOff)
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

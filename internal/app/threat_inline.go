package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/threatcfg"
)

// "Block as it happens" (inline). The firewall decides first; what it accepts between the checked
// networks and the internet is held in a queue until the engine says pass or drop. Which checks
// drop is set per group: Off, Alert (log only) or Block. The queue fails open, so a stopped or
// overloaded engine lets traffic through unchecked rather than cutting the internet.

const (
	threatInlineSetting  = "threat.inline"       // "1" when blocking as it happens is on
	threatInlineNets     = "threat.inline_nets"  // checked networks (ports), comma-separated; empty means all
	threatBlockGroupsSet = "threat.block_groups" // groups set to Block; empty means the defaults, "none" means none
	threatAlertOnlySet   = "threat.alert_only"   // single rules kept at alert only
	threatNeverSet       = "threat.never_check"  // addresses and networks that are never checked
)

// inlineBlockDefault are the groups that start on Block: known malware and its command servers,
// infected-device traffic and exploit attempts. Everything else starts on Alert. The IP-blocklist
// lists (ciarmy, dshield and the like) are not rule groups here at all, so they can never block.
var inlineBlockDefault = []string{"malware", "mobile_malware", "exploit", "exploit_kit"}

func (a *App) threatInline() bool {
	return a.threatMode() == "watch" && a.Config.Setting(threatInlineSetting, "") == "1"
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func (a *App) wanIfaces() []string {
	var out []string
	if rows, err := a.Config.Query(`SELECT iface FROM wan_links WHERE iface<>'' ORDER BY iface`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			out = append(out, n)
		}
		rows.Close()
	}
	return out
}

// inlineIfaces are the checked networks: the person's choice among the inside ports, or all of them.
func (a *App) inlineIfaces() []string {
	all := a.ndpiIfaces()
	chosen := splitList(a.Config.Setting(threatInlineNets, ""))
	if len(chosen) == 0 {
		return all
	}
	have := map[string]bool{}
	for _, i := range all {
		have[i] = true
	}
	var out []string
	for _, c := range chosen {
		if have[c] {
			out = append(out, c)
		}
	}
	return out
}

func (a *App) inlineAlertOnly() []int {
	var out []int
	for _, f := range splitList(a.Config.Setting(threatAlertOnlySet, "")) {
		if n, err := strconv.Atoi(f); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func (a *App) inlineNever() []string { return splitList(a.Config.Setting(threatNeverSet, "")) }

// inlineBlockList is the groups set to Block before groups that are off are taken out.
func (a *App) inlineBlockList() []string {
	v := strings.TrimSpace(a.Config.Setting(threatBlockGroupsSet, ""))
	switch v {
	case "":
		return append([]string{}, inlineBlockDefault...)
	case "none":
		return nil
	}
	var out []string
	for _, g := range splitList(v) {
		if threatcfg.KnownGroup(g) {
			out = append(out, g)
		}
	}
	return out
}

// inlineBlockGroups are the groups that drop: set to Block and not switched off.
func (a *App) inlineBlockGroups(p threatcfg.Plan) []string {
	off := map[string]bool{}
	for _, g := range threatcfg.EffectiveOff(p) {
		off[g] = true
	}
	var out []string
	for _, g := range a.inlineBlockList() {
		if !off[g] {
			out = append(out, g)
		}
	}
	return out
}

func (a *App) setInlineBlockList(l []string) {
	sort.Strings(l)
	v := strings.Join(l, ",")
	if v == "" {
		v = "none"
	}
	a.Config.SetSetting(threatBlockGroupsSet, v)
}

// ---- Keeping up / Falling behind ----

type inlineHealth struct {
	mu        sync.Mutex
	last      netdThreat
	lastAt    time.Time
	behindAt  time.Time // the last time packets were let through unchecked because the engine was behind
	behindN   int64
	startedAt time.Time
}

// inlineTick is called once a minute with the engine's latest numbers.
func (a *App) inlineTick(info netdThreat, now time.Time) {
	h := &a.inlineH
	h.mu.Lock()
	defer h.mu.Unlock()
	if !a.threatInline() {
		h.last, h.lastAt, h.behindAt, h.behindN, h.startedAt = netdThreat{}, time.Time{}, time.Time{}, 0, time.Time{}
		return
	}
	if h.startedAt.IsZero() {
		h.startedAt = now
	}
	if !h.lastAt.IsZero() && info.Queue.Listening && h.last.Queue.Listening {
		d := (info.Queue.QueueDropped + info.Queue.UserDropped) - (h.last.Queue.QueueDropped + h.last.Queue.UserDropped)
		if d > 0 {
			h.behindAt, h.behindN = now, d
		}
	}
	h.last, h.lastAt = info, now
}

// inlineState says in one word how blocking is doing: keeping_up, falling_behind, stopped or starting.
func (a *App) inlineState(now time.Time) (state, text string) {
	h := &a.inlineH
	h.mu.Lock()
	defer h.mu.Unlock()
	info := h.last
	if h.lastAt.IsZero() {
		return "starting", "Starting up. This takes about a minute."
	}
	if !info.Running || !info.Queue.Listening {
		return "stopped", "Not checking. Traffic is passing through unchecked, and your internet keeps working. NopeWarden is restarting it."
	}
	if !h.behindAt.IsZero() && now.Sub(h.behindAt) < 10*time.Minute {
		return "falling_behind", "Falling behind. At busy moments some traffic passed through unchecked. Turn some check groups off, or use a machine with a faster processor."
	}
	return "keeping_up", "Keeping up. Every packet is being checked."
}

// inlineSize says whether this machine looks big enough. It is a warning, not a refusal.
func (a *App) inlineSize(info netdThreat) (warns []string) {
	if info.CPUs > 0 && info.CPUs < 2 {
		warns = append(warns, "This machine has one processor core. Checking every packet as it passes needs at least two; with one, traffic may slow down. NopeWarden never cuts the internet if the checks cannot keep up.")
	}
	if info.MemoryMB > 0 && info.MemoryMB < 3500 {
		warns = append(warns, fmt.Sprintf("This machine has %d MB of memory. Blocking as it happens works best with 4 GB or more.", info.MemoryMB))
	}
	if info.Virt != "" {
		warns = append(warns, "This is a virtual machine ("+info.Virt+"). Checking packets is slow on virtual network cards; expect lower speeds than on real hardware.")
	}
	return warns
}

var errInlineOff = errors.New("switch on Block as it happens first")

func (a *App) inlineSync() error {
	if a.threatInline() {
		return a.threatSync()
	}
	return nil
}

func (a *App) registerInlineRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/threat/inline", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		plan := a.threatPlan()
		info := a.threatInfo()
		off := map[string]bool{}
		for _, g := range threatcfg.EffectiveOff(threatcfg.Plan{Preset: a.threatPreset(), GroupsOn: plan.GroupsOn, GroupsOff: plan.GroupsOff}) {
			off[g] = true
		}
		block := map[string]bool{}
		for _, g := range a.inlineBlockList() {
			block[g] = true
		}
		type cat struct {
			Name    string `json:"name"`
			Text    string `json:"text"`
			Level   string `json:"level"` // off, alert, block
			Default string `json:"default"`
			Common  bool   `json:"common"`
		}
		defs := map[string]bool{}
		for _, g := range inlineBlockDefault {
			defs[g] = true
		}
		cats := []cat{}
		for _, g := range threatcfg.Groups() {
			lv, df := "alert", "alert"
			if defs[g.Name] {
				df = "block"
			}
			if off[g.Name] {
				lv = "off"
			} else if block[g.Name] {
				lv = "block"
			}
			cats = append(cats, cat{g.Name, g.Text, lv, df, g.Common})
		}
		chosen := map[string]bool{}
		for _, i := range a.inlineIfaces() {
			chosen[i] = true
		}
		type netw struct {
			Iface string `json:"iface"`
			Name  string `json:"name"`
			On    bool   `json:"on"`
		}
		nets := []netw{}
		for _, i := range a.ndpiIfaces() {
			name := i
			var z string
			if a.Config.QueryRow(`SELECT COALESCE(z.name,'') FROM interfaces i LEFT JOIN zones z ON z.id=i.zone_id WHERE i.name=?`, i).Scan(&z) == nil && z != "" {
				name = z + " (" + i + ")"
			}
			nets = append(nets, netw{i, name, chosen[i]})
		}
		type rule struct {
			SID int    `json:"sid"`
			Msg string `json:"msg"`
		}
		ao := []rule{}
		for _, sid := range a.inlineAlertOnly() {
			ao = append(ao, rule{sid, a.threatRuleMsg(sid)})
		}
		state, text := a.inlineState(time.Now())
		never := a.inlineNever()
		if never == nil {
			never = []string{}
		}
		api.JSON(w, 200, map[string]any{
			"on": a.threatInline(), "networks": nets, "wan": a.wanIfaces(), "categories": cats, "alert_only": ao, "never": never,
			"state": state, "state_text": text, "size_warnings": a.inlineSize(info), "installed": info.Installed,
			"test_url": "http://example.com" + threatcfg.TestPath,
			"queue":    info.Queue,
			"notes": []string{
				"The firewall decides first. Only traffic it lets through between the checked networks and the internet is checked, in both directions.",
				"Traffic between your own networks, and to this firewall itself (the console, SSH), is not checked.",
				"If the checks stop, or cannot keep up, traffic passes through unchecked. Your internet is never cut by the checks.",
				"Most traffic is encrypted, so this finds known bad servers and attack patterns, not everything.",
			},
		})
	})
	s.HandleAdmin("PUT /api/threat/inline/networks", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Ifaces []string `json:"ifaces"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		have := map[string]bool{}
		for _, i := range a.ndpiIfaces() {
			have[i] = true
		}
		for _, i := range in.Ifaces {
			if !have[i] {
				fail(w, errors.New("that network was not found"))
				return
			}
		}
		if len(in.Ifaces) == 0 {
			fail(w, errors.New("choose at least one network to check"))
			return
		}
		a.Config.SetSetting(threatInlineNets, strings.Join(in.Ifaces, ","))
		if err := a.inlineSync(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Changed which networks are checked as traffic passes", strings.Join(in.Ifaces, ", "))
		ok(w)
	})
	s.HandleAdmin("PUT /api/threat/inline/category", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Group string `json:"group"`
			Level string `json:"level"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if !threatcfg.KnownGroup(in.Group) || (in.Level != "off" && in.Level != "alert" && in.Level != "block") {
			fail(w, errors.New("choose Off, Alert or Block"))
			return
		}
		// Off is the existing on/off of the group; Alert and Block only matter while it is on.
		on, off := a.threatGroupChanges()
		wantOff := map[string]bool{}
		for _, g := range threatcfg.EffectiveOff(threatcfg.Plan{Preset: a.threatPreset(), GroupsOn: on, GroupsOff: off}) {
			wantOff[g] = true
		}
		if in.Level == "off" {
			wantOff[in.Group] = true
		} else {
			delete(wantOff, in.Group)
		}
		on, off = threatcfg.Changes(a.threatPreset(), wantOff)
		a.setThreatGroupChanges(on, off)
		l := a.inlineBlockList()
		keep := l[:0]
		for _, g := range l {
			if g != in.Group {
				keep = append(keep, g)
			}
		}
		if in.Level == "block" {
			keep = append(keep, in.Group)
		}
		a.setInlineBlockList(keep)
		if a.threatMode() == "watch" {
			if err := a.threatSync(); err != nil {
				fail(w, err)
				return
			}
		}
		a.event(c.User.Username, "info", "Changed what a group of threat checks does", in.Group+": "+in.Level)
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/inline/stop-blocking", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			SID int `json:"sid"`
		}
		if !api.Decode(w, r, &in) || in.SID <= 0 {
			fail(w, errors.New("that rule was not found"))
			return
		}
		l := a.inlineAlertOnly()
		for _, x := range l {
			if x == in.SID {
				ok(w)
				return
			}
		}
		l = append(l, in.SID)
		a.setIntList(threatAlertOnlySet, l)
		if err := a.inlineSync(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Stopped blocking one threat check", fmt.Sprintf("Rule %d now only alerts.", in.SID))
		ok(w)
	})
	s.HandleAdmin("DELETE /api/threat/inline/stop-blocking/{sid}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		sid, _ := strconv.Atoi(r.PathValue("sid"))
		var keep []int
		for _, x := range a.inlineAlertOnly() {
			if x != sid {
				keep = append(keep, x)
			}
		}
		a.setIntList(threatAlertOnlySet, keep)
		if err := a.inlineSync(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Went back to the group's setting for one threat check", fmt.Sprintf("Rule %d.", sid))
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/inline/never", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Addr string `json:"addr"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		addr, err := cleanNet(in.Addr)
		if err != nil {
			fail(w, err)
			return
		}
		l := a.inlineNever()
		for _, x := range l {
			if x == addr {
				ok(w)
				return
			}
		}
		l = append(l, addr)
		sort.Strings(l)
		a.Config.SetSetting(threatNeverSet, strings.Join(l, ","))
		if err := a.inlineSync(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Stopped checking one device or address", addr)
		ok(w)
	})
	s.HandleAdmin("DELETE /api/threat/inline/never", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		addr := r.URL.Query().Get("addr")
		var keep []string
		for _, x := range a.inlineNever() {
			if x != addr {
				keep = append(keep, x)
			}
		}
		a.Config.SetSetting(threatNeverSet, strings.Join(keep, ","))
		if err := a.inlineSync(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Started checking an address again", addr)
		ok(w)
	})
}

func (a *App) setIntList(key string, l []int) {
	sort.Ints(l)
	var parts []string
	last := 0
	for _, n := range l {
		if n != last && n > 0 {
			parts = append(parts, strconv.Itoa(n))
			last = n
		}
	}
	a.Config.SetSetting(key, strings.Join(parts, ","))
}

// cleanNet checks an address or network the person typed, and says it the way it is stored.
func cleanNet(s string) (string, error) {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked().String(), nil
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.String(), nil
	}
	return "", errors.New("that is not an address or network, like 192.168.1.50 or 192.168.1.0/24")
}

// threatRuleMsg is the plain name of a rule, from the rules in use.
func (a *App) threatRuleMsg(sid int) string {
	rs, _ := a.threatSearchRules(strconv.Itoa(sid), 5)
	for _, r := range rs {
		if r.SID == sid {
			return r.Msg
		}
	}
	if sid == threatcfg.TestSID {
		return "NopeWarden inline test (harmless)"
	}
	return fmt.Sprintf("Rule %d", sid)
}

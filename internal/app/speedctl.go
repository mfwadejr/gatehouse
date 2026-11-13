package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/appid"
	dnssrv "nopewarden/internal/dns"
	"nopewarden/internal/fw"
)

// Speed control: Give priority (High, Normal, Low) and Limit speed (download and upload) for
// networks, profiles and devices, optionally only for some apps, categories or websites. The
// firewall does the work (fw/speed.go); this file keeps the entries, resolves who and what they
// mean into address lists, and says in words what each one does.

type speedRule struct {
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	Kind        string        `json:"kind"` // limit or priority
	Prio        string        `json:"prio"` // high, normal or low
	DownMbps    float64       `json:"down_mbps"`
	UpMbps      float64       `json:"up_mbps"`
	Each        bool          `json:"each"` // a limit for each device instead of all together
	Schedule    string        `json:"schedule"`
	Enabled     bool          `json:"enabled"`
	PausedUntil int64         `json:"paused_until"`
	Items       []blockItem   `json:"items"`
	Targets     []blockTarget `json:"targets"`
	// Filled in for the console.
	Title  string `json:"title,omitempty"`
	Where  string `json:"where,omitempty"`
	What   string `json:"what,omitempty"`
	When   string `json:"when,omitempty"`
	Does   string `json:"does,omitempty"` // a sentence saying what it does
	Status string `json:"status,omitempty"`
	Note   string `json:"note,omitempty"`
}

const maxSpeedRules = 60

func (a *App) loadSpeedRules() ([]speedRule, error) {
	rows, err := a.Config.Query(`SELECT id,name,kind,prio,down_kbit,up_kbit,each,schedule,enabled,paused_until,items,targets FROM speed_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []speedRule{}
	for rows.Next() {
		var r speedRule
		var down, up, each, en int
		var it, tg string
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Prio, &down, &up, &each, &r.Schedule, &en, &r.PausedUntil, &it, &tg); err != nil {
			return nil, err
		}
		r.DownMbps, r.UpMbps, r.Each, r.Enabled = float64(down)/1000, float64(up)/1000, each == 1, en == 1
		json.Unmarshal([]byte(it), &r.Items)
		json.Unmarshal([]byte(tg), &r.Targets)
		if r.Items == nil {
			r.Items = []blockItem{}
		}
		if r.Targets == nil {
			r.Targets = []blockTarget{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var reMACAddr = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)

func (a *App) validSpeedRule(r *speedRule) error {
	switch r.Kind {
	case "limit":
		if r.DownMbps < 0 || r.UpMbps < 0 || r.DownMbps > 100000 || r.UpMbps > 100000 {
			return errors.New("the speeds must be between 0 and 100,000 Mb/s")
		}
		if r.DownMbps == 0 && r.UpMbps == 0 {
			return errors.New("enter a download speed, an upload speed, or both")
		}
		if (r.DownMbps > 0 && r.DownMbps < 0.1) || (r.UpMbps > 0 && r.UpMbps < 0.1) {
			return errors.New("the smallest limit is 0.1 Mb/s")
		}
		r.Prio = "normal"
	case "priority":
		if r.Prio != "high" && r.Prio != "normal" && r.Prio != "low" {
			return errors.New("choose High, Normal or Low")
		}
		r.DownMbps, r.UpMbps, r.Each = 0, 0, false
	default:
		return errors.New("choose Limit speed or Give priority")
	}
	if len(r.Items) > maxBlockItems || len(r.Targets) > maxBlockTargets {
		return errors.New("that is more than one entry can hold")
	}
	cats := map[string]bool{}
	for _, c := range appid.Categories() {
		cats[c] = true
	}
	apps := map[string]string{}
	for _, n := range appid.Names() {
		apps[strings.ToLower(n)] = n
	}
	for i := range r.Items {
		it := &r.Items[i]
		switch it.Kind {
		case "app":
			n, ok := apps[strings.ToLower(strings.TrimSpace(it.Value))]
			if !ok {
				return errors.New("NopeWarden does not know the app " + it.Value + " yet")
			}
			it.Value = n
		case "topic":
			c, ok := strings.CutPrefix(it.Value, "cat:")
			if !ok || !cats[c] {
				return errors.New("choose one of the app categories, such as Video or Games")
			}
		case "name":
			d := dnssrv.NormalizeSite(it.Value)
			if d == "" {
				return errors.New(it.Value + " is not a website name")
			}
			it.Value = d
		default:
			return errors.New("choose apps, categories or websites")
		}
		it.Label = ""
	}
	for i := range r.Targets {
		t := &r.Targets[i]
		t.Label = ""
		switch t.Scope {
		case "everyone":
			t.Target = ""
		case "network":
			var n int
			a.Config.QueryRow(`SELECT COUNT(*) FROM zones WHERE lower(name)=lower(?) AND kind<>'wan'`, t.Target).Scan(&n)
			if n == 0 {
				return errors.New(t.Target + " is not one of your networks")
			}
		case "profile":
			var n int
			a.Config.QueryRow(`SELECT COUNT(*) FROM groups WHERE id=?`, t.Target).Scan(&n)
			if n == 0 {
				return errors.New("that profile does not exist")
			}
		case "device":
			t.Target = strings.ToLower(strings.TrimSpace(t.Target))
			if !reMACAddr.MatchString(t.Target) {
				return errors.New("a device is chosen by its hardware address")
			}
		default:
			return errors.New("choose where it applies")
		}
	}
	if len(r.Targets) == 0 {
		return errors.New("choose where it applies: everyone, or some networks, profiles and devices")
	}
	if _, err := fw.ParseSchedule(r.Schedule); err != nil {
		return errors.New("the hours are not valid")
	}
	return nil
}

func (a *App) saveSpeedRule(r speedRule) (int64, error) {
	if err := a.validSpeedRule(&r); err != nil {
		return 0, err
	}
	it, _ := json.Marshal(r.Items)
	tg, _ := json.Marshal(r.Targets)
	down, up := int(r.DownMbps*1000+0.5), int(r.UpMbps*1000+0.5)
	b2 := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	if r.ID > 0 {
		res, err := a.Config.Exec(`UPDATE speed_rules SET name=?,kind=?,prio=?,down_kbit=?,up_kbit=?,each=?,schedule=?,enabled=?,paused_until=?,items=?,targets=? WHERE id=?`,
			strings.TrimSpace(r.Name), r.Kind, r.Prio, down, up, b2(r.Each), r.Schedule, b2(r.Enabled), r.PausedUntil, string(it), string(tg), r.ID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return 0, sql.ErrNoRows
		}
		return r.ID, nil
	}
	var n int
	a.Config.QueryRow(`SELECT COUNT(*) FROM speed_rules`).Scan(&n)
	if n >= maxSpeedRules {
		return 0, errors.New("that is as many speed entries as NopeWarden keeps")
	}
	res, err := a.Config.Exec(`INSERT INTO speed_rules(name,kind,prio,down_kbit,up_kbit,each,schedule,enabled,paused_until,items,targets,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(r.Name), r.Kind, r.Prio, down, up, b2(r.Each), r.Schedule, b2(r.Enabled), r.PausedUntil, string(it), string(tg), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func mbps(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10) + " Mb/s"
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " Mb/s"
}

// speedWords fills in what the console shows for one entry.
func (a *App) speedWords(r *speedRule, queueOn bool) {
	var what []string
	for _, it := range r.Items {
		what = append(what, it.Value)
	}
	for i, w := range what {
		what[i] = strings.TrimPrefix(w, "cat:")
	}
	switch {
	case len(what) == 0:
		r.What = "everything"
	case len(what) <= 3:
		r.What = joinWords(what)
	default:
		r.What = nounCount(len(what), "thing")
	}
	var where []string
	for _, t := range r.Targets {
		switch t.Scope {
		case "everyone":
			where = append(where, "Everyone")
		case "network":
			where = append(where, t.Target)
		case "profile":
			var n string
			a.Config.QueryRow(`SELECT name FROM groups WHERE id=?`, t.Target).Scan(&n)
			where = append(where, orWord(n, "Profile "+t.Target))
		case "device":
			where = append(where, orWord(a.fwdDeviceName(t.Target, ""), t.Target))
		}
	}
	switch {
	case len(where) == 0:
		r.Where = "Nowhere"
	case len(where) <= 3:
		r.Where = joinWords(where)
	default:
		r.Where = nounCount(len(where), "place")
	}
	sc, _ := fw.ParseSchedule(r.Schedule)
	r.When = sc.Describe()
	if r.Kind == "limit" {
		var sp []string
		if r.DownMbps > 0 {
			sp = append(sp, mbps(r.DownMbps)+" down")
		}
		if r.UpMbps > 0 {
			sp = append(sp, mbps(r.UpMbps)+" up")
		}
		each := "all together"
		if r.Each {
			each = "each device"
		}
		r.Title = orWord(r.Name, "Limit "+r.Where)
		r.Does = "Holds " + r.What + " for " + r.Where + " to " + strings.Join(sp, " and ") + ", " + each + "."
	} else {
		lvl := map[string]string{"high": "high priority", "normal": "normal priority", "low": "low priority (background)"}[r.Prio]
		r.Title = orWord(r.Name, "Priority for "+r.Where)
		r.Does = "Gives " + r.What + " for " + r.Where + " " + lvl + " when the line is full."
		if !queueOn {
			r.Note = "Priority works through the smart queue. Turn on Keep delay low for the connection on the Speed tab."
		}
	}
	probe := scopeRule{Enabled: r.Enabled, PausedUntil: r.PausedUntil, Schedule: r.Schedule}
	switch {
	case !r.Enabled:
		r.Status = "off"
	case r.PausedUntil > time.Now().Unix():
		r.Status = "paused"
	case !a.inForce(probe, time.Now()):
		r.Status = "outside hours"
	case r.Kind == "priority" && !queueOn:
		r.Status = "waiting"
	case r.Kind == "limit":
		r.Status = "limiting"
	default:
		r.Status = "prioritising"
	}
}

func (a *App) speedQueueOn() bool {
	cfg := a.sqmConfig()
	for _, on := range cfg.Links {
		if on {
			return true
		}
	}
	return false
}

func (a *App) speedViews() []speedRule {
	rules, _ := a.loadSpeedRules()
	q := a.speedQueueOn()
	for i := range rules {
		a.speedWords(&rules[i], q)
	}
	return rules
}

// speedDomains are the website endings an entry's apps, categories and websites stand for.
func speedDomains(items []blockItem) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ds ...string) {
		for _, d := range ds {
			if d = strings.ToLower(d); d != "" && !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	for _, it := range items {
		switch it.Kind {
		case "app":
			add(appid.Domains(it.Value)...)
		case "topic":
			if c, ok := strings.CutPrefix(it.Value, "cat:"); ok {
				add(appid.CategoryDomains(c)...)
			}
		case "name":
			add(it.Value)
		}
	}
	return out
}

// speedFills works out what the address sets of the speed entries hold now: the devices and
// profiles an entry covers (from the lease table) and the addresses its apps and websites have
// lately answered with (from the DNS answers).
func (a *App) speedFills(m *fw.Model, leases, reserved map[string][]string, now time.Time) map[string]fw.SetFill {
	out := map[string]fw.SetFill{}
	if len(m.Speed) == 0 {
		return out
	}
	rules, _ := a.loadSpeedRules()
	by := map[int64]speedRule{}
	for _, r := range rules {
		by[r.ID] = r
	}
	for _, sr := range m.Speed {
		r, ok := by[sr.ID]
		if !ok {
			continue
		}
		if sr.WhoSet {
			var ips []string
			for _, t := range r.Targets {
				switch t.Scope {
				case "device":
					ips = append(ips, deviceAddrs([]string{strings.ToLower(t.Target)}, false, leases, reserved)...)
				case "profile":
					gid, _ := strconv.ParseInt(t.Target, 10, 64)
					for mac, list := range leases {
						for _, ip := range list {
							if a.Devices != nil && a.Devices.Client(ip).GroupID == gid {
								ips = append(ips, ip)
							}
						}
						_ = mac
					}
				}
			}
			out[fw.SpeedWhoSet(sr.ID)] = fw.SetFill{Replace: true, Elems: onlyFamily(uniqStrings(ips), 4)}
		}
		if sr.WhatSet {
			var elems []string
			for _, d := range speedDomains(r.Items) {
				for ip, exp := range a.dyn.dom[d] {
					left := int(exp.Sub(now).Seconds())
					if left < 1 {
						continue
					}
					if addr, err := netipParseAddr(ip); err == nil && addr.Is4() {
						elems = append(elems, ip+" timeout "+istr(left)+"s")
					}
				}
			}
			out[fw.SpeedWhatSet(sr.ID)] = fw.SetFill{Replace: true, Elems: elems}
		}
	}
	return out
}

func (a *App) registerSpeedCtlRoutes() {
	s := a.API
	s.Handle("GET /api/speed", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		apps := []map[string]string{}
		for _, n := range appid.Names() {
			if ap, ok := appid.Lookup(firstDomain(n)); ok {
				apps = append(apps, map[string]string{"name": n, "category": ap.Category})
			} else {
				apps = append(apps, map[string]string{"name": n})
			}
		}
		disc := ""
		a.sqm.mu.Lock()
		disc = a.sqm.disc
		a.sqm.mu.Unlock()
		api.JSON(w, 200, map[string]any{
			"on": a.setting("speed.on", "1") == "1", "rules": a.speedViews(),
			"queue":      map[string]any{"on": a.speedQueueOn(), "disc": disc},
			"apps":       apps,
			"categories": appid.Categories(),
		})
	})
	s.HandleAdmin("PUT /api/speed/rule", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in speedRule
		if !api.Decode(w, r, &in) {
			return
		}
		id, err := a.saveSpeedRule(in)
		if err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		a.fwSync()
		a.sqmSync(false)
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/speed/rule/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM speed_rules WHERE id=?`, id)
		a.markDirty()
		a.fwSync()
		a.sqmSync(false)
		ok(w)
	})
	s.HandleAdmin("POST /api/speed/rule/{id}/pause", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		var in struct {
			Minutes int `json:"minutes"`
		}
		if !good || !api.Decode(w, r, &in) {
			return
		}
		until, err := pausePick(in.Minutes)
		if err != nil {
			fail(w, err)
			return
		}
		a.Config.Exec(`UPDATE speed_rules SET paused_until=? WHERE id=?`, until, id)
		a.markDirty()
		a.fwSync()
		a.sqmSync(false)
		ok(w)
	})
	s.HandleAdmin("POST /api/speed/rule/{id}/enable", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !good || !api.Decode(w, r, &in) {
			return
		}
		a.Config.Exec(`UPDATE speed_rules SET enabled=?, paused_until=0 WHERE id=?`, b2i(in.Enabled), id)
		a.markDirty()
		a.fwSync()
		a.sqmSync(false)
		ok(w)
	})
	s.HandleAdmin("PUT /api/speed/switch", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			On bool `json:"on"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		a.Config.SetSetting("speed.on", map[bool]string{true: "1", false: "0"}[in.On])
		a.markDirty()
		a.fwSync()
		a.sqmSync(false)
		a.event(c.User.Username, "info", "Speed control was turned "+map[bool]string{true: "on", false: "off"}[in.On], "")
		ok(w)
	})
}

func firstDomain(app string) string {
	if d := appid.Domains(app); len(d) > 0 {
		return d[0]
	}
	return ""
}

var _ = fmt.Sprintf

func (a *App) speedRuleByID(id int64) (speedRule, bool) {
	rules, _ := a.loadSpeedRules()
	for _, r := range rules {
		if r.ID == id {
			return r, true
		}
	}
	return speedRule{}, false
}

// speedWantsPriority says whether any switched-on entry gives priority, so the queue should tell
// traffic classes apart.
func (a *App) speedWantsPriority() bool {
	if a.setting("speed.on", "1") != "1" {
		return false
	}
	rules, _ := a.loadSpeedRules()
	for _, r := range rules {
		if r.Enabled && r.Kind == "priority" {
			return true
		}
	}
	return false
}

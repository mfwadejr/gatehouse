package app

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/appid"
	dnssrv "gatehouse/internal/dns"
	"gatehouse/internal/fw"
)

// Blocks by scope: block an app, a topic (a category list) or a name for everyone, one network,
// one profile or one device, always or on a schedule, with an "allow" exception for a device.
//
// Today the "fast" way is in force: Gatehouse refuses the lookup of the app's names. The "thorough"
// way (looking at the traffic itself) is stored and shown but not yet switched on.
//
// This feature fails open. If anything in it goes wrong, lookups are answered as if there were no
// block, and a device is never cut off because of a fault here.

type scopeRule struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`  // app, topic, name
	Value       string `json:"value"` // app name, category key or domain
	Scope       string `json:"scope"` // everyone, network, profile, device
	Target      string `json:"target"`
	Allow       bool   `json:"allow"`
	Method      string `json:"method"`
	Schedule    string `json:"schedule"`
	Enabled     bool   `json:"enabled"`
	PausedUntil int64  `json:"paused_until"`
	// Filled in for the console.
	Text   string `json:"text,omitempty"`
	Status string `json:"status,omitempty"` // blocking, paused, off, outside hours, waiting
	When   string `json:"when,omitempty"`
}

type scopeCache struct {
	mu    sync.Mutex
	rules []scopeRule
	at    time.Time
	lists map[string]int64
	zones sync.Map // ip -> zoneEntry
}

type zoneEntry struct {
	zone string
	at   time.Time
}

func (a *App) scopeLoc() *time.Location {
	if name := strings.TrimSpace(a.Config.Setting("system.timezone", "")); name != "" {
		if l, err := time.LoadLocation(name); err == nil {
			return l
		}
	}
	return time.UTC
}

func (a *App) loadScopeRules() ([]scopeRule, error) {
	rows, err := a.Config.Query(`SELECT id,kind,value,scope,target,allow,method,schedule,enabled,paused_until FROM scope_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []scopeRule
	for rows.Next() {
		var r scopeRule
		var al, en int
		if err := rows.Scan(&r.ID, &r.Kind, &r.Value, &r.Scope, &r.Target, &al, &r.Method, &r.Schedule, &en, &r.PausedUntil); err != nil {
			return nil, err
		}
		r.Allow, r.Enabled = al == 1, en == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// cachedScopeRules keeps the rules for a few seconds so a lookup never waits on the database.
func (a *App) cachedScopeRules() []scopeRule {
	c := &a.scope
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rules == nil || time.Since(c.at) > 5*time.Second {
		if r, err := a.loadScopeRules(); err == nil {
			c.rules, c.at = r, time.Now()
			if c.rules == nil {
				c.rules = []scopeRule{}
			}
		}
	}
	return c.rules
}

func (a *App) dropScopeCache() {
	a.scope.mu.Lock()
	a.scope.at = time.Time{}
	a.scope.rules = nil
	a.scope.mu.Unlock()
}

func (a *App) clientZone(ip string) string {
	if v, ok := a.scope.zones.Load(ip); ok {
		if e := v.(zoneEntry); time.Since(e.at) < time.Minute {
			return e.zone
		}
	}
	z := a.deviceZone(ip)
	a.scope.zones.Store(ip, zoneEntry{z, time.Now()})
	return z
}

// inForce says whether a rule counts right now: on, not paused, and inside its hours.
func (a *App) inForce(r scopeRule, now time.Time) bool {
	if !r.Enabled || (r.PausedUntil > now.Unix()) {
		return false
	}
	sc, err := fw.ParseSchedule(r.Schedule)
	if err != nil {
		return false
	}
	return sc.Active(a.scopeLoc(), now)
}

func (r scopeRule) covers(ci dnssrv.ClientInfo, zone func() string) bool {
	switch r.Scope {
	case "everyone":
		return true
	case "device":
		return ci.MAC != "" && strings.EqualFold(ci.MAC, r.Target)
	case "profile":
		return strconv.FormatInt(ci.GroupID, 10) == r.Target
	case "network":
		return zone() != "" && strings.EqualFold(zone(), r.Target)
	}
	return false
}

func (r scopeRule) names(name string, a *App) (string, bool) {
	switch r.Kind {
	case "name":
		if name == r.Value || strings.HasSuffix(name, "."+r.Value) {
			return r.Value, true
		}
	case "app":
		if ap, ok := appid.Lookup(name); ok && strings.EqualFold(ap.Name, r.Value) {
			return ap.Name, true
		}
	case "topic":
		if id := a.topicList(r.Value); id != 0 {
			if rule, ok := a.DNS.ListHit(name, id); ok {
				return rule, true
			}
		}
	}
	return "", false
}

func (a *App) topicList(key string) int64 {
	for _, c := range categoryCatalog {
		if c.Key == key {
			var id int64
			a.Config.QueryRow(`SELECT id FROM blocklists WHERE url=?`, c.URL).Scan(&id)
			return id
		}
	}
	return 0
}

// scopedBlock is the DNS hook. It returns the reason to refuse the lookup, or "".
func (a *App) scopedBlock(client string, ci dnssrv.ClientInfo, name string) string {
	rules := a.cachedScopeRules()
	if len(rules) == 0 {
		return ""
	}
	now := time.Now()
	var zone string
	var zoneDone bool
	zf := func() string {
		if !zoneDone {
			zone, zoneDone = a.clientZone(client), true
		}
		return zone
	}
	for _, r := range rules {
		if r.Allow || r.Method == "thorough" || !r.covers(ci, zf) || !a.inForce(r, now) {
			continue
		}
		what, hit := r.names(name, a)
		if !hit {
			continue
		}
		// An allow rule for the same thing, covering this device, is the exception.
		for _, x := range rules {
			if x.Allow && x.Kind == r.Kind && strings.EqualFold(x.Value, r.Value) && x.covers(ci, zf) && a.inForce(x, now) {
				return ""
			}
		}
		switch r.Kind {
		case "app":
			return "blocked app: " + what
		case "topic":
			return "blocked topic: " + r.Value
		}
		return "blocked name: " + what
	}
	return ""
}

func scopeWords(r scopeRule, a *App) string {
	what := r.Value
	switch r.Kind {
	case "topic":
		for _, c := range categoryCatalog {
			if c.Key == r.Value {
				what = c.Name
			}
		}
	}
	who := "everyone"
	switch r.Scope {
	case "network":
		who = "the " + r.Target + " network"
	case "profile":
		var n string
		a.Config.QueryRow(`SELECT name FROM groups WHERE id=?`, r.Target).Scan(&n)
		who = "the " + n + " profile"
	case "device":
		who = a.fwdDeviceName(r.Target, "")
	}
	if r.Allow {
		return "Allow " + what + " for " + who
	}
	return "Block " + what + " for " + who
}

func (a *App) scopeViews() []scopeRule {
	rules, _ := a.loadScopeRules()
	now := time.Now()
	out := make([]scopeRule, 0, len(rules))
	for _, r := range rules {
		r.Text = scopeWords(r, a)
		sc, _ := fw.ParseSchedule(r.Schedule)
		r.When = sc.Describe()
		switch {
		case !r.Enabled:
			r.Status = "off"
		case r.PausedUntil > now.Unix():
			r.Status = "paused"
		case r.Method == "thorough":
			r.Status = "waiting"
		case !a.inForce(r, now):
			r.Status = "outside hours"
		case r.Allow:
			r.Status = "allowing"
		default:
			r.Status = "blocking"
		}
		out = append(out, r)
	}
	return out
}

type blockIn struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Scope    string `json:"scope"`
	Target   string `json:"target"`
	Allow    bool   `json:"allow"`
	Method   string `json:"method"`
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
}

func (a *App) saveScopeRule(in blockIn) error {
	in.Value = strings.TrimSpace(in.Value)
	in.Target = strings.TrimSpace(in.Target)
	switch in.Kind {
	case "app":
		found := false
		for _, n := range appid.Names() {
			if strings.EqualFold(n, in.Value) {
				in.Value, found = n, true
			}
		}
		if !found {
			return errors.New("Gatehouse does not know that app yet")
		}
	case "topic":
		if a.topicKey(in.Value) == "" {
			return errors.New("choose one of the topics")
		}
	case "name":
		d := dnssrv.NormalizeDomain(in.Value)
		if d == "" {
			return errors.New("that does not look like a website name")
		}
		in.Value = d
	default:
		return errors.New("choose an app, a topic or a name")
	}
	switch in.Scope {
	case "everyone":
		in.Target = ""
	case "network", "profile", "device":
		if in.Target == "" {
			return errors.New("choose who this is for")
		}
		if in.Scope == "device" {
			in.Target = strings.ToLower(in.Target)
		}
	default:
		return errors.New("choose who this is for")
	}
	if in.Method == "" {
		in.Method = "fast"
	}
	if in.Method != "fast" {
		return errors.New("only the fast way (names) is available so far; thorough checking is not switched on yet")
	}
	sc, err := fw.ParseSchedule(in.Schedule)
	if err == nil {
		err = sc.Check(a.scopeLoc())
	}
	if err != nil {
		return err
	}
	if in.Kind == "topic" && !in.Allow {
		for _, c := range categoryCatalog {
			if c.Key == in.Value {
				if _, e := a.categoryListID(c); e != nil {
					return e
				}
			}
		}
	}
	en := 0
	if in.Enabled {
		en = 1
	}
	al := 0
	if in.Allow {
		al = 1
	}
	if in.ID == 0 {
		_, err = a.Config.Exec(`INSERT INTO scope_rules(kind,value,scope,target,allow,method,schedule,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			in.Kind, in.Value, in.Scope, in.Target, al, in.Method, in.Schedule, en, time.Now().Unix())
	} else {
		_, err = a.Config.Exec(`UPDATE scope_rules SET kind=?,value=?,scope=?,target=?,allow=?,method=?,schedule=?,enabled=? WHERE id=?`,
			in.Kind, in.Value, in.Scope, in.Target, al, in.Method, in.Schedule, en, in.ID)
	}
	a.dropScopeCache()
	return err
}

func (a *App) topicKey(k string) string {
	for _, c := range categoryCatalog {
		if c.Key == k {
			return k
		}
	}
	return ""
}

func (a *App) registerScopeRoutes() {
	s := a.API
	a.DNS.Scoped = a.scopedBlock
	s.Handle("GET /api/blocks", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		type appOut struct {
			Name     string `json:"name"`
			Category string `json:"category"`
		}
		var apps []appOut
		for _, n := range appid.Names() {
			ap, _ := appid.Lookup(appid.Domains(n)[0])
			apps = append(apps, appOut{n, ap.Category})
		}
		sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
		type topicOut struct {
			Key     string `json:"key"`
			Name    string `json:"name"`
			Descr   string `json:"descr"`
			ListID  int64  `json:"list_id"`
			Entries int    `json:"entries"`
			Status  string `json:"status"`
		}
		var topics []topicOut
		for _, ct := range categoryCatalog {
			t := topicOut{Key: ct.Key, Name: ct.Name, Descr: ct.Descr, Status: "not downloaded yet"}
			a.Config.QueryRow(`SELECT id,entries,status FROM blocklists WHERE url=?`, ct.URL).Scan(&t.ListID, &t.Entries, &t.Status)
			topics = append(topics, t)
		}
		api.JSON(w, 200, map[string]any{
			"rules": a.scopeViews(), "apps": apps, "topics": topics,
			"inspection": map[string]any{"available": false, "note": "Thorough checking is not switched on yet. Blocking by name works now."},
			"fail_open":  "If anything goes wrong, nothing is blocked. A fault never cuts a device off.",
		})
	})
	s.HandleAdmin("PUT /api/blocks", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in blockIn
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.saveScopeRule(in); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("POST /api/blocks/{id}/pause", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		var in struct {
			Minutes int `json:"minutes"`
		}
		if !good || !api.Decode(w, r, &in) {
			return
		}
		until := int64(0)
		if in.Minutes > 0 {
			until = time.Now().Add(time.Duration(in.Minutes) * time.Minute).Unix()
		}
		a.Config.Exec(`UPDATE scope_rules SET paused_until=? WHERE id=?`, until, id)
		a.dropScopeCache()
		ok(w)
	})
	s.HandleAdmin("POST /api/blocks/{id}/enable", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !good || !api.Decode(w, r, &in) {
			return
		}
		n := 0
		if in.Enabled {
			n = 1
		}
		a.Config.Exec(`UPDATE scope_rules SET enabled=?, paused_until=0 WHERE id=?`, n, id)
		a.dropScopeCache()
		ok(w)
	})
	s.HandleAdmin("DELETE /api/blocks/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM scope_rules WHERE id=?`, id)
		a.dropScopeCache()
		ok(w)
	})
	// View the names a list blocks, and let one through.
	s.Handle("GET /api/dns/lists/{id}/names", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		rows, total, err := a.Lists.Names(id, r.URL.Query().Get("q"), 100)
		if err != nil {
			fail(w, err)
			return
		}
		if rows == nil {
			rows = []dnssrv.NameRow{}
		}
		var upd int64
		var name string
		a.Config.QueryRow(`SELECT name,last_ok FROM blocklists WHERE id=?`, id).Scan(&name, &upd)
		api.JSON(w, 200, map[string]any{"names": rows, "total": total, "list": name, "updated": upd})
	})
}

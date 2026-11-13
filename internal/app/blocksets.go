package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/appid"
	dnssrv "nopewarden/internal/dns"
	"nopewarden/internal/fw"
)

// A block is a named list of things (apps, categories, websites) that applies to a list of places
// (everyone, networks, profiles, devices), with one How, one When and one on/off switch.
//
// The engine is unchanged. The rows in scope_rules are made from the blocks (every thing in every
// place) and are what the DNS hook reads, so a block behaves exactly like the single blocks before it.

type blockItem struct {
	Kind  string `json:"kind"` // app, topic, name
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

type blockTarget struct {
	Scope  string `json:"scope"` // everyone, network, profile, device
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

type blockSet struct {
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	Allow       bool          `json:"allow"`
	Method      string        `json:"method"`
	Schedule    string        `json:"schedule"`
	Enabled     bool          `json:"enabled"`
	PausedUntil int64         `json:"paused_until"`
	Items       []blockItem   `json:"items"`
	Targets     []blockTarget `json:"targets"`
	// Filled in for the console.
	Title  string `json:"title,omitempty"` // the name, or one made from what it holds
	Where  string `json:"where,omitempty"` // where it applies, in words
	What   string `json:"what,omitempty"`  // what it holds, in words
	When   string `json:"when,omitempty"`
	Status string `json:"status,omitempty"` // blocking, allowing, paused, off, outside hours, waiting, nowhere
	Recent int    `json:"recent"`           // lookups refused in the last day
}

const (
	maxBlockItems   = 200
	maxBlockTargets = 100
	maxBlockRows    = 5000
)

func (a *App) loadBlockSets() ([]blockSet, error) {
	rows, err := a.Config.Query(`SELECT id,name,allow,method,schedule,enabled,paused_until,items,targets FROM block_sets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []blockSet{}
	for rows.Next() {
		var b blockSet
		var al, en int
		var it, tg string
		if err := rows.Scan(&b.ID, &b.Name, &al, &b.Method, &b.Schedule, &en, &b.PausedUntil, &it, &tg); err != nil {
			return nil, err
		}
		b.Allow, b.Enabled = al == 1, en == 1
		json.Unmarshal([]byte(it), &b.Items)
		json.Unmarshal([]byte(tg), &b.Targets)
		if b.Items == nil {
			b.Items = []blockItem{}
		}
		if b.Targets == nil {
			b.Targets = []blockTarget{}
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// migrateBlockSets turns any single block that is not part of a named block yet into a block of one
// (with the same number). It runs when the page loads and after a restore from an older backup.
func (a *App) migrateBlockSets() {
	rows, err := a.Config.Query(`SELECT id,kind,value,scope,target,allow,method,schedule,enabled,paused_until FROM scope_rules WHERE set_id=0`)
	if err != nil {
		return
	}
	type old struct {
		id                                 int64
		kind, value, scope, target, method string
		schedule                           string
		allow, enabled                     int
		paused                             int64
	}
	var all []old
	for rows.Next() {
		var o old
		if rows.Scan(&o.id, &o.kind, &o.value, &o.scope, &o.target, &o.allow, &o.method, &o.schedule, &o.enabled, &o.paused) == nil {
			all = append(all, o)
		}
	}
	rows.Close()
	for _, o := range all {
		it, _ := json.Marshal([]blockItem{{Kind: o.kind, Value: o.value}})
		tg, _ := json.Marshal([]blockTarget{{Scope: o.scope, Target: o.target}})
		a.Config.Exec(`INSERT OR IGNORE INTO block_sets(id,name,allow,method,schedule,enabled,paused_until,items,targets,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			o.id, "", o.allow, o.method, o.schedule, o.enabled, o.paused, string(it), string(tg), time.Now().Unix())
		a.Config.Exec(`UPDATE scope_rules SET set_id=? WHERE id=?`, o.id, o.id)
	}
	if len(all) > 0 {
		a.dropScopeCache()
	}
}

// checkBlockItem checks one thing to block or allow and returns it in its proper form.
func (a *App) checkBlockItem(kind, value string, allow bool) (string, error) {
	value = strings.TrimSpace(value)
	switch kind {
	case "app":
		for _, n := range appid.Names() {
			if strings.EqualFold(n, value) {
				return n, nil
			}
		}
		return "", errors.New("NopeWarden does not know the app " + value + " yet")
	case "topic":
		if a.topicKey(value) == "" {
			return "", errors.New("choose one of the categories")
		}
		if !allow {
			for _, c := range categoryCatalog {
				if c.Key == value {
					if _, e := a.categoryListID(c); e != nil {
						return "", e
					}
				}
			}
		}
		return value, nil
	case "name":
		d := dnssrv.NormalizeSite(value)
		if d == "" {
			return "", errors.New(value + " does not look like a website name")
		}
		return d, nil
	}
	return "", errors.New("choose an app, a category or a website")
}

func checkBlockTarget(scope, target string) (string, error) {
	target = strings.TrimSpace(target)
	switch scope {
	case "everyone":
		return "", nil
	case "network", "profile", "device":
		if target == "" {
			return "", errors.New("choose where this applies")
		}
		if scope == "device" {
			target = strings.ToLower(target)
		}
		return target, nil
	}
	return "", errors.New("choose where this applies")
}

// saveBlockSet checks and saves a block, then rebuilds the rows the DNS hook reads.
func (a *App) saveBlockSet(in blockSet) (int64, error) {
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) > 60 {
		return 0, errors.New("keep the name to 60 characters or fewer")
	}
	if len(in.Items) == 0 {
		return 0, errors.New("choose at least one app, category or website")
	}
	if len(in.Targets) == 0 {
		return 0, errors.New("choose where this applies, or choose everyone")
	}
	if len(in.Items) > maxBlockItems || len(in.Targets) > maxBlockTargets || len(in.Items)*len(in.Targets) > maxBlockRows {
		return 0, errors.New("that is more than one block can hold; split it into two")
	}
	seen := map[string]bool{}
	var items []blockItem
	for _, it := range in.Items {
		v, err := a.checkBlockItem(it.Kind, it.Value, in.Allow)
		if err != nil {
			return 0, err
		}
		if k := it.Kind + "|" + strings.ToLower(v); !seen[k] {
			seen[k] = true
			items = append(items, blockItem{Kind: it.Kind, Value: v})
		}
	}
	seen = map[string]bool{}
	var targets []blockTarget
	for _, t := range in.Targets {
		v, err := checkBlockTarget(t.Scope, t.Target)
		if err != nil {
			return 0, err
		}
		if k := t.Scope + "|" + v; !seen[k] {
			seen[k] = true
			targets = append(targets, blockTarget{Scope: t.Scope, Target: v})
		}
	}
	for _, t := range targets { // everyone already covers every place
		if t.Scope == "everyone" {
			targets = []blockTarget{t}
			break
		}
	}
	if in.Method == "" {
		in.Method = "fast"
	}
	if in.Method != "fast" && in.Method != "thorough" && in.Method != "both" {
		return 0, errors.New("choose by name, thorough, or both")
	}
	sc, err := fw.ParseSchedule(in.Schedule)
	if err == nil {
		err = sc.Check(a.scopeLoc())
	}
	if err != nil {
		return 0, err
	}
	it, _ := json.Marshal(items)
	tg, _ := json.Marshal(targets)
	en, al := b2i(in.Enabled), b2i(in.Allow)
	tx, err := a.Config.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id := in.ID
	if id == 0 {
		res, e := tx.Exec(`INSERT INTO block_sets(name,allow,method,schedule,enabled,items,targets,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			in.Name, al, in.Method, in.Schedule, en, string(it), string(tg), time.Now().Unix())
		if e != nil {
			return 0, e
		}
		id, _ = res.LastInsertId()
	} else {
		res, e := tx.Exec(`UPDATE block_sets SET name=?,allow=?,method=?,schedule=?,enabled=?,items=?,targets=? WHERE id=?`,
			in.Name, al, in.Method, in.Schedule, en, string(it), string(tg), id)
		if e != nil {
			return 0, e
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return 0, errors.New("that block was not found")
		}
	}
	if err := regenerateRows(tx, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	a.dropScopeCache()
	return id, nil
}

type txer interface {
	Exec(q string, args ...any) (sql.Result, error)
	QueryRow(q string, args ...any) *sql.Row
}

// regenerateRows rebuilds a block's rows: every thing in every place.
func regenerateRows(tx txer, id int64) error {
	var al, en int
	var method, schedule, it, tg string
	var paused int64
	if err := tx.QueryRow(`SELECT allow,method,schedule,enabled,paused_until,items,targets FROM block_sets WHERE id=?`, id).Scan(&al, &method, &schedule, &en, &paused, &it, &tg); err != nil {
		return err
	}
	var items []blockItem
	var targets []blockTarget
	json.Unmarshal([]byte(it), &items)
	json.Unmarshal([]byte(tg), &targets)
	if _, err := tx.Exec(`DELETE FROM scope_rules WHERE set_id=?`, id); err != nil {
		return err
	}
	for _, i := range items {
		for _, t := range targets {
			if _, err := tx.Exec(`INSERT INTO scope_rules(kind,value,scope,target,allow,method,schedule,enabled,paused_until,created_at,set_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				i.Kind, i.Value, t.Scope, t.Target, al, method, schedule, en, paused, time.Now().Unix(), id); err != nil {
				return err
			}
		}
	}
	return nil
}

// blockWhatWhere says in words what a block holds and where it applies.
func (a *App) blockWords(b *blockSet) {
	var apps, topics, names []string
	for i := range b.Items {
		it := &b.Items[i]
		it.Label = it.Value
		if it.Kind == "topic" {
			for _, c := range categoryCatalog {
				if c.Key == it.Value {
					it.Label = c.Name
				}
			}
			for _, t := range appTopics {
				if t.Key == it.Value {
					it.Label = t.Name
				}
			}
			topics = append(topics, it.Label)
		} else if it.Kind == "app" {
			apps = append(apps, it.Label)
		} else {
			names = append(names, it.Label)
		}
	}
	all := append(append(append([]string{}, apps...), topics...), names...)
	b.What = nounCount(len(all), "thing")
	switch {
	case len(all) == 0:
		b.What = "nothing"
	case len(all) <= 3:
		b.What = joinWords(all)
	}
	var where []string
	for i := range b.Targets {
		t := &b.Targets[i]
		switch t.Scope {
		case "everyone":
			t.Label = "Everyone"
		case "network":
			t.Label = t.Target
		case "profile":
			a.Config.QueryRow(`SELECT name FROM groups WHERE id=?`, t.Target).Scan(&t.Label)
			if t.Label == "" {
				t.Label = "Profile " + t.Target
			}
		case "device":
			t.Label = a.fwdDeviceName(t.Target, "")
			if t.Label == "" {
				t.Label = t.Target
			}
		}
		where = append(where, t.Label)
	}
	switch {
	case len(where) == 0:
		b.Where = "Nowhere"
	case len(where) <= 3:
		b.Where = joinWords(where)
	default:
		b.Where = nounCount(len(where), "place")
	}
	b.Title = b.Name
	if b.Title == "" {
		verb := "Block "
		if b.Allow {
			verb = "Allow "
		}
		switch {
		case len(all) == 0:
			b.Title = verb + "nothing"
		case len(all) <= 2:
			b.Title = verb + joinWords(all)
		default:
			b.Title = verb + all[0] + " and " + strconv.Itoa(len(all)-1) + " more"
		}
	}
}

func nounCount(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func joinWords(w []string) string {
	switch len(w) {
	case 0:
		return ""
	case 1:
		return w[0]
	}
	return strings.Join(w[:len(w)-1], ", ") + " and " + w[len(w)-1]
}

// blockViews are the blocks as the console shows them.
func (a *App) blockViews() []blockSet {
	sets, _ := a.loadBlockSets()
	now := time.Now()
	recent := a.blockedRecently()
	for i := range sets {
		b := &sets[i]
		a.blockWords(b)
		sc, _ := fw.ParseSchedule(b.Schedule)
		b.When = sc.Describe()
		probe := scopeRule{Enabled: b.Enabled, PausedUntil: b.PausedUntil, Schedule: b.Schedule}
		switch {
		case !b.Enabled:
			b.Status = "off"
		case b.PausedUntil > now.Unix():
			b.Status = "paused"
		case len(b.Targets) == 0 || len(b.Items) == 0:
			b.Status = "nowhere"
		case b.Method == "thorough":
			b.Status = "waiting"
		case !a.inForce(probe, now):
			b.Status = "outside hours"
		case b.Allow:
			b.Status = "allowing"
		default:
			b.Status = "blocking"
		}
		b.Recent = recent[b.ID]
	}
	return sets
}

// pruneBlockTargets takes a network, profile or device that no longer exists out of the blocks that
// used it. A block left with no place stays in the list, marked as applying nowhere.
func (a *App) pruneBlockTargets() {
	sets, err := a.loadBlockSets()
	if err != nil {
		return
	}
	exists := func(t blockTarget) bool {
		var n int
		switch t.Scope {
		case "network":
			a.Config.QueryRow(`SELECT count(*) FROM zones WHERE lower(name)=lower(?)`, t.Target).Scan(&n)
		case "profile":
			a.Config.QueryRow(`SELECT count(*) FROM groups WHERE id=?`, t.Target).Scan(&n)
		case "device":
			a.Config.QueryRow(`SELECT count(*) FROM devices WHERE lower(mac)=?`, strings.ToLower(t.Target)).Scan(&n)
		default:
			return true
		}
		return n > 0
	}
	changed := false
	for _, b := range sets {
		keep := []blockTarget{}
		for _, t := range b.Targets {
			if exists(t) {
				keep = append(keep, t)
			}
		}
		if len(keep) == len(b.Targets) {
			continue
		}
		tg, _ := json.Marshal(keep)
		if tx, err := a.Config.Begin(); err == nil {
			_, e := tx.Exec(`UPDATE block_sets SET targets=? WHERE id=?`, string(tg), b.ID)
			if e == nil {
				e = regenerateRows(tx, b.ID)
			}
			if e == nil && tx.Commit() == nil {
				changed = true
			} else {
				tx.Rollback()
			}
		}
	}
	if changed {
		a.dropScopeCache()
	}
}

// blocksUsing names the blocks that use a network, a profile or a device, for the warning shown
// before it is removed.
func (a *App) blocksUsing(scope, target string) []string {
	var out []string
	sets, _ := a.loadBlockSets()
	for _, b := range sets {
		for _, t := range b.Targets {
			if t.Scope == scope && strings.EqualFold(t.Target, target) {
				a.blockWords(&b)
				out = append(out, b.Title)
				break
			}
		}
	}
	return out
}

// blockedRecently counts, per block, the lookups it refused in the last day. The DNS log keeps the
// reason, which names the thing blocked; it is matched to the blocks that hold that thing.
func (a *App) blockedRecently() map[int64]int {
	out := map[int64]int{}
	counts := map[string]int{}
	rows, err := a.Logs.Query(`SELECT rule,COUNT(*) FROM dns_queries WHERE ts>=? AND result='blocked' AND rule LIKE 'blocked %' GROUP BY rule`, time.Now().Add(-24*time.Hour).Unix())
	if err != nil {
		return out
	}
	for rows.Next() {
		var k string
		var n int
		if rows.Scan(&k, &n) == nil {
			counts[k] = n
		}
	}
	rows.Close()
	if len(counts) == 0 {
		return out
	}
	sets, _ := a.loadBlockSets()
	for _, b := range sets {
		if b.Allow {
			continue
		}
		for _, it := range b.Items {
			var key string
			switch it.Kind {
			case "app":
				key = "blocked app: " + it.Value
			case "topic":
				key = "blocked topic: " + it.Value
			default:
				key = "blocked name: " + it.Value
			}
			out[b.ID] += counts[key]
		}
	}
	return out
}

func (a *App) registerBlockSetRoutes() {
	s := a.API
	s.HandleAdmin("PUT /api/blocks", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		// The older body (one thing, one place) is still accepted, as a block of one.
		var in struct {
			blockSet
			Kind   string `json:"kind"`
			Value  string `json:"value"`
			Scope  string `json:"scope"`
			Target string `json:"target"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if len(in.Items) == 0 && in.Kind != "" {
			in.Items = []blockItem{{Kind: in.Kind, Value: in.Value}}
			in.Targets = []blockTarget{{Scope: in.Scope, Target: in.Target}}
		}
		id, err := a.saveBlockSet(in.blockSet)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"ok": true, "id": id})
	})
	s.HandleAdmin("POST /api/blocks/{id}/pause", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
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
		a.Config.Exec(`UPDATE block_sets SET paused_until=? WHERE id=?`, until, id)
		a.Config.Exec(`UPDATE scope_rules SET paused_until=? WHERE set_id=?`, until, id)
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
		a.Config.Exec(`UPDATE block_sets SET enabled=?, paused_until=0 WHERE id=?`, b2i(in.Enabled), id)
		a.Config.Exec(`UPDATE scope_rules SET enabled=?, paused_until=0 WHERE set_id=?`, b2i(in.Enabled), id)
		a.dropScopeCache()
		ok(w)
	})
	s.HandleAdmin("DELETE /api/blocks/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM scope_rules WHERE set_id=?`, id)
		a.Config.Exec(`DELETE FROM block_sets WHERE id=?`, id)
		a.dropScopeCache()
		ok(w)
	})
}

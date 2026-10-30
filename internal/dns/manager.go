package dnssrv

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/store"
)

// ListInfo is a blocklist as the console shows it.
type ListInfo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Format    string `json:"format"`
	Enabled   bool   `json:"enabled"`
	Interval  int    `json:"interval_hours"`
	LastFetch int64  `json:"last_fetch"`
	LastOK    int64  `json:"last_ok"`
	Entries   int    `json:"entries"` // names the list blocks
	Allowed   int    `json:"allowed"` // exceptions ("do not block this") the list carries
	Skipped   int    `json:"skipped"` // lines in a syntax Gatehouse does not use
	Status    string `json:"status"`  // new, downloading, ok, error, stale
	Error     string `json:"error,omitempty"`
	Plain     string `json:"plain,omitempty"` // the error in plain words
	RetryAt   int64  `json:"retry_at,omitempty"`
	Local     bool   `json:"local"`
}

const maxListBytes = 200 << 20

// Manager owns blocklists, custom rules and group assignments, and builds the
// immutable engine the resolver reads.
type Manager struct {
	db       *store.DB
	dir      string
	eng      *engineHolder
	http     *http.Client
	now      func() time.Time
	mu       sync.Mutex // serialises reloads and refreshes
	mu2      sync.Mutex // guards fetching and unique
	OnChange func()
	// Online, when set, says whether the internet answers; a list that failed is tried again at
	// once when this goes from false to true, instead of waiting out its backoff.
	Online   func() bool
	fetching map[int64]bool // lists being downloaded right now
	unique   int            // distinct names blocked across all enabled lists
	// AllowFetch lets tests permit loopback URLs; production blocks private addresses.
	AllowPrivate bool
}

func NewManager(db *store.DB, dir string, eng *engineHolder) *Manager {
	os.MkdirAll(dir, 0o750)
	return &Manager{db: db, dir: dir, eng: eng, now: time.Now, fetching: map[int64]bool{},
		http: &http.Client{Timeout: 90 * time.Second}}
}

func (m *Manager) file(id int64) string { return filepath.Join(m.dir, fmt.Sprintf("%d.list", id)) }

// SeedDefaults adds the default group and two well-known lists on first run.
func (m *Manager) SeedDefaults() error {
	var n int
	m.db.QueryRow(`SELECT count(*) FROM groups`).Scan(&n)
	if n > 0 {
		return nil
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, g := range []struct {
		name string
		filt int
		desc string
	}{{"Default", 1, "Devices without a group"}, {"Trusted", 0, "No DNS filtering"}, {"Kids", 1, "Filtered, add stricter lists here"}} {
		if _, err := tx.Exec(`INSERT INTO groups(name,filtering,description) VALUES(?,?,?)`, g.name, g.filt, g.desc); err != nil {
			return err
		}
	}
	for _, l := range []struct{ name, url, format string }{
		{"AdGuard DNS filter", "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt", "abp"},
		{"StevenBlack unified hosts", "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts", "hosts"},
	} {
		res, err := tx.Exec(`INSERT INTO blocklists(name,url,format) VALUES(?,?,?)`, l.name, l.url, l.format)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO group_blocklists(group_id,blocklist_id) SELECT id,? FROM groups WHERE name IN ('Default','Kids')`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Lists returns every blocklist.
func (m *Manager) Lists() ([]ListInfo, error) {
	rows, err := m.db.Query(`SELECT id,name,url,format,enabled,interval_hours,last_fetch,last_ok,entries,allowed,skipped,status,error,retry_at,local FROM blocklists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ListInfo
	for rows.Next() {
		var l ListInfo
		var en, loc int
		if err := rows.Scan(&l.ID, &l.Name, &l.URL, &l.Format, &en, &l.Interval, &l.LastFetch, &l.LastOK, &l.Entries, &l.Allowed, &l.Skipped, &l.Status, &l.Error, &l.RetryAt, &loc); err != nil {
			return nil, err
		}
		l.Enabled, l.Local = en == 1, loc == 1
		if m.isFetching(l.ID) {
			l.Status = "downloading"
		}
		if l.Status == "error" {
			l.Plain = PlainListError(l.Error)
		}
		if l.Status == "ok" && l.Interval > 0 && l.LastOK > 0 && m.now().Unix()-l.LastOK > int64(l.Interval)*3600*3 {
			l.Status = "stale"
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (m *Manager) isFetching(id int64) bool {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	return m.fetching[id]
}

func (m *Manager) setFetching(id int64, on bool) {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	if on {
		m.fetching[id] = true
	} else {
		delete(m.fetching, id)
	}
}

// UniqueBlocked is how many different names the enabled lists block, counting a name once
// even when several lists carry it.
func (m *Manager) UniqueBlocked() int {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	return m.unique
}

// PlainListError turns the technical text of a failed download into a sentence a person can use.
func PlainListError(e string) string {
	l := strings.ToLower(e)
	switch {
	case e == "":
		return ""
	case strings.Contains(l, "no usable entries"):
		return "The file did not look like a blocklist."
	case strings.Contains(l, "larger than"):
		return "The list is too large to use."
	case strings.Contains(l, "server answered"):
		return "The list's website answered with an error."
	case strings.Contains(l, "lookup") || strings.Contains(l, "dial tcp") || strings.Contains(l, "no such host") || strings.Contains(l, "timeout") ||
		strings.Contains(l, "connection refused") || strings.Contains(l, "unreachable") || strings.Contains(l, "deadline exceeded") || strings.Contains(l, "eof"):
		return "Couldn't reach the list's website."
	}
	return "Couldn't download this list."
}

// retryDelay is how long to wait after the n-th failed download in a row: a minute, 2, 5, 15, 30,
// then an hour, until it works.
func retryDelay(n int) time.Duration {
	steps := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}
	if n < 0 {
		n = 0
	}
	if n >= len(steps) {
		n = len(steps) - 1
	}
	return steps[n]
}

// Nudge lets lists that failed try again at once, for example when the internet comes back.
func (m *Manager) Nudge() {
	m.db.Exec(`UPDATE blocklists SET retry_at=0 WHERE status='error'`)
}

func validListURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("list address must be an http or https URL")
	}
	if !allowPrivate {
		h := u.Hostname()
		if h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") {
			return fmt.Errorf("list address must not point at this network")
		}
	}
	return nil
}

// Add creates a URL-based list and fetches it.
func (m *Manager) Add(name, rawURL, format string, intervalHours int) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("a name is required")
	}
	if err := validListURL(rawURL, m.AllowPrivate); err != nil {
		return 0, err
	}
	if format == "" {
		format = FormatAuto
	}
	if intervalHours <= 0 {
		intervalHours = 24
	}
	res, err := m.db.Exec(`INSERT INTO blocklists(name,url,format,interval_hours) VALUES(?,?,?,?)`, name, rawURL, format, intervalHours)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, fmt.Errorf("a list with that name already exists")
		}
		return 0, err
	}
	id, _ := res.LastInsertId()
	m.db.Exec(`INSERT INTO group_blocklists(group_id,blocklist_id) SELECT id,? FROM groups WHERE name='Default'`, id)
	return id, nil
}

// AddLocal creates a list from pasted or uploaded text.
func (m *Manager) AddLocal(name, text, format string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("a name is required")
	}
	res, err := m.db.Exec(`INSERT INTO blocklists(name,url,format,local,interval_hours) VALUES(?,?,?,1,0)`, name, "", format)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, fmt.Errorf("a list with that name already exists")
		}
		return 0, err
	}
	id, _ := res.LastInsertId()
	if err := m.store(id, strings.NewReader(text), format); err != nil {
		m.db.Exec(`DELETE FROM blocklists WHERE id=?`, id)
		return 0, err
	}
	m.db.Exec(`INSERT INTO group_blocklists(group_id,blocklist_id) SELECT id,? FROM groups WHERE name='Default'`, id)
	return id, m.Reload()
}

// Delete removes a list and its cached file.
func (m *Manager) Delete(id int64) error {
	if _, err := m.db.Exec(`DELETE FROM blocklists WHERE id=?`, id); err != nil {
		return err
	}
	os.Remove(m.file(id))
	return m.Reload()
}

// store parses r, and only if it yields rules replaces the cached file and the row.
func (m *Manager) store(id int64, r io.Reader, format string) error {
	tmp, err := os.CreateTemp(m.dir, "fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, io.LimitReader(r, maxListBytes+1)); err != nil {
		tmp.Close()
		return err
	}
	if st, _ := tmp.Stat(); st != nil && st.Size() > maxListBytes {
		tmp.Close()
		return fmt.Errorf("list is larger than %d MB", maxListBytes>>20)
	}
	tmp.Seek(0, io.SeekStart)
	count, allowed := 0, 0
	stats, err := Parse(tmp, format, func(r Rule) {
		if r.Allow {
			allowed++
		} else {
			count++
		}
	})
	tmp.Close()
	if err != nil {
		return err
	}
	if count+allowed == 0 {
		return fmt.Errorf("no usable entries found (%d lines skipped)", stats.Skipped)
	}
	if err := os.Rename(tmp.Name(), m.file(id)); err != nil {
		return err
	}
	now := m.now().Unix()
	_, err = m.db.Exec(`UPDATE blocklists SET entries=?, allowed=?, skipped=?, format=CASE WHEN format='auto' THEN ? ELSE format END, last_fetch=?, last_ok=?, status='ok', error='', failures=0, retry_at=0 WHERE id=?`,
		count, allowed, stats.Skipped, stats.Format, now, now, id)
	return err
}

// Refresh downloads one list. On failure the previous copy stays in use.
func (m *Manager) Refresh(ctx context.Context, id int64) error {
	var name, raw, format string
	var local int
	if err := m.db.QueryRow(`SELECT name,url,format,local FROM blocklists WHERE id=?`, id).Scan(&name, &raw, &format, &local); err != nil {
		return err
	}
	if local == 1 || raw == "" {
		return nil
	}
	m.setFetching(id, true)
	defer m.setFetching(id, false)
	// A failed try is not a finished one: the list is tried again soon (1, 2, 5, 15 minutes, then
	// hourly), not after a whole refresh interval.
	fail := func(err error) error {
		var n int
		m.db.QueryRow(`SELECT failures FROM blocklists WHERE id=?`, id).Scan(&n)
		m.db.Exec(`UPDATE blocklists SET last_fetch=?, status='error', error=?, failures=?, retry_at=? WHERE id=?`,
			m.now().Unix(), err.Error(), n+1, m.now().Add(retryDelay(n)).Unix(), id)
		return err
	}
	if err := validListURL(raw, m.AllowPrivate); err != nil {
		return fail(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("User-Agent", "Gatehouse/1.0")
	resp, err := m.http.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("server answered %s", resp.Status))
	}
	if err := m.store(id, resp.Body, format); err != nil {
		return fail(err)
	}
	return m.Reload()
}

// Due returns lists whose refresh interval has passed.
func (m *Manager) due() []int64 {
	rows, err := m.db.Query(`SELECT id FROM blocklists WHERE enabled=1 AND local=0 AND url<>'' AND ?>=retry_at AND
		(last_ok=0 OR ?-last_ok >= interval_hours*3600)`, m.now().Unix(), m.now().Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

// Run refreshes due lists periodically until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	wasOnline := false
	for {
		if m.Online != nil {
			on := m.Online()
			if on && !wasOnline {
				m.Nudge()
			}
			wasOnline = on
		}
		for _, id := range m.due() {
			rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			m.Refresh(rctx, id)
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Reload rebuilds the engine from the database and cached list files.
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	type lst struct {
		id                        int64
		name                      string
		format                    string
		entries, allowed, skipped int // as saved; corrected below from the file itself
	}
	rows, err := m.db.Query(`SELECT id,name,format,entries,allowed,skipped FROM blocklists WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return err
	}
	var lists []lst
	for rows.Next() {
		var l lst
		rows.Scan(&l.id, &l.name, &l.format, &l.entries, &l.allowed, &l.skipped)
		lists = append(lists, l)
	}
	rows.Close()
	if len(lists) > MaxLists {
		lists = lists[:MaxLists]
	}
	b := newIndexBuilder()
	bit := map[int64]int{}
	for i, l := range lists {
		bit[l.id] = i
		b.setName(i, l.name)
		f, err := os.Open(m.file(l.id))
		if err != nil {
			continue // not fetched yet
		}
		i := i
		l := l
		blocked, allowed := 0, 0
		stats, _ := Parse(f, l.format, func(r Rule) {
			b.add(i, l.name, r)
			if r.Allow {
				allowed++
			} else {
				blocked++
			}
		})
		f.Close()
		// Lists saved by an older version only knew one number; fill in the three from the file.
		if blocked != l.entries || allowed != l.allowed || stats.Skipped != l.skipped {
			m.db.Exec(`UPDATE blocklists SET entries=?, allowed=?, skipped=? WHERE id=?`, blocked, allowed, stats.Skipped, l.id)
		}
	}
	custom := newCustom()
	crow, err := m.db.Query(`SELECT domain,action FROM custom_rules`)
	if err == nil {
		for crow.Next() {
			var d, a string
			crow.Scan(&d, &a)
			if a == "allow" {
				custom.allow[d] = ""
			} else {
				custom.block[d] = ""
			}
		}
		crow.Close()
	}
	masks := map[int64]uint64{}
	nofilter := map[int64]bool{}
	grows, err := m.db.Query(`SELECT id,filtering FROM groups`)
	if err == nil {
		for grows.Next() {
			var id int64
			var f int
			grows.Scan(&id, &f)
			masks[id] = 0
			if f == 0 {
				nofilter[id] = true
			}
		}
		grows.Close()
	}
	gb, err := m.db.Query(`SELECT group_id,blocklist_id FROM group_blocklists`)
	if err == nil {
		for gb.Next() {
			var g, l int64
			gb.Scan(&g, &l)
			if i, ok := bit[l]; ok {
				masks[g] |= 1 << uint(i)
			}
		}
		gb.Close()
	}
	m.mu2.Lock()
	m.unique = len(b.ix.block)
	m.mu2.Unlock()
	m.eng.p.Store(&engine{ix: b.ix, custom: custom, masks: masks, nofilter: nofilter})
	if m.OnChange != nil {
		m.OnChange()
	}
	return nil
}

// Explanation answers "why was this blocked?".
type Explanation struct {
	Domain  string   `json:"domain"`
	Blocked bool     `json:"blocked"`
	Reason  string   `json:"reason"` // custom-allow, custom-block, list, none, unfiltered
	Rule    string   `json:"rule,omitempty"`
	Lists   []string `json:"lists"` // every enabled list that contains the domain, in or out of the group
}

// Explain checks a domain for a group and reports which lists contain it.
func (m *Manager) Explain(domain string, group int64) Explanation {
	d := NormalizeDomain(domain)
	if d == "" {
		d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	}
	e := m.eng.get()
	ex := Explanation{Domain: d, Reason: "none"}
	for i := 0; i < MaxLists; i++ {
		if _, _, ok := e.ix.Lookup(d, 1<<uint(i)); ok {
			ex.Lists = append(ex.Lists, e.ix.ListName(i))
		}
	}
	sort.Strings(ex.Lists)
	if e.nofilter[group] {
		ex.Reason = "unfiltered"
		return ex
	}
	switch a, rule := e.custom.Check(d); a {
	case "allow":
		ex.Reason, ex.Rule = "custom-allow", rule
		return ex
	case "block":
		ex.Blocked, ex.Reason, ex.Rule = true, "custom-block", rule
		return ex
	}
	if i, rule, ok := e.ix.Lookup(d, e.masks[group]); ok {
		ex.Blocked, ex.Reason, ex.Rule = true, "list", rule
		ex.Lists = []string{e.ix.ListName(i)}
		for _, n := range ex.Lists {
			_ = n
		}
		// Keep every containing list visible too.
		ex.Lists = uniq(append(ex.Lists, m.allContaining(e, d)...))
	}
	return ex
}

func (m *Manager) allContaining(e *engine, d string) []string {
	var out []string
	for i := 0; i < MaxLists; i++ {
		if _, _, ok := e.ix.Lookup(d, 1<<uint(i)); ok {
			out = append(out, e.ix.ListName(i))
		}
	}
	return out
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// CustomRule is an administrator allow/block entry.
type CustomRule struct {
	ID     int64  `json:"id"`
	Domain string `json:"domain"`
	Action string `json:"action"`
	Note   string `json:"note"`
}

// AddCustom stores an allow or block rule.
func (m *Manager) AddCustom(domain, action, note string) error {
	d := NormalizeDomain(domain)
	if d == "" {
		return fmt.Errorf("that does not look like a domain name")
	}
	if action != "allow" && action != "block" {
		return fmt.Errorf("action must be allow or block")
	}
	if _, err := m.db.Exec(`INSERT OR REPLACE INTO custom_rules(domain,action,note,created_at) VALUES(?,?,?,?)`, d, action, note, m.now().Unix()); err != nil {
		return err
	}
	return m.Reload()
}

// DeleteCustom removes a rule.
func (m *Manager) DeleteCustom(id int64) error {
	if _, err := m.db.Exec(`DELETE FROM custom_rules WHERE id=?`, id); err != nil {
		return err
	}
	return m.Reload()
}

// Customs lists the administrator's rules.
func (m *Manager) Customs() ([]CustomRule, error) {
	rows, err := m.db.Query(`SELECT id,domain,action,note FROM custom_rules ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomRule
	for rows.Next() {
		var c CustomRule
		rows.Scan(&c.ID, &c.Domain, &c.Action, &c.Note)
		out = append(out, c)
	}
	return out, rows.Err()
}

var _ = sql.ErrNoRows

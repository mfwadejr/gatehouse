package dnssrv

import (
	"database/sql"
	"strings"
	"sync/atomic"
	"time"

	"gatehouse/internal/store"
)

// LogEntry is one query as it is stored.
type LogEntry struct {
	TS         int64  `json:"ts"`
	Client     string `json:"client"`
	ClientName string `json:"client_name"`
	QName      string `json:"qname"`
	QType      string `json:"qtype"`
	Result     string `json:"result"`
	RCode      string `json:"rcode"`
	Upstream   string `json:"upstream"`
	LatencyUS  int64  `json:"latency_us"`
	Rule       string `json:"rule"`
}

// QueryLog batches inserts into logs.db so the resolver never waits on the disk.
type QueryLog struct {
	db        *store.DB
	ch        chan LogEntry
	dropped   atomic.Uint64
	Anonymize atomic.Bool
	live      func(LogEntry) // optional hook for the live log widget
}

func NewQueryLog(db *store.DB, live func(LogEntry)) *QueryLog {
	return &QueryLog{db: db, ch: make(chan LogEntry, 16384), live: live}
}

// Add queues an entry; it never blocks.
func (q *QueryLog) Add(e LogEntry) {
	if q.Anonymize.Load() {
		e.Client, e.ClientName = "anonymous", ""
	}
	select {
	case q.ch <- e:
	default:
		q.dropped.Add(1)
	}
	if q.live != nil {
		q.live(e)
	}
}

// Dropped returns how many entries were discarded because the writer fell behind.
func (q *QueryLog) Dropped() uint64 { return q.dropped.Load() }

// Run writes queued entries in batches until stop is closed, then drains.
func (q *QueryLog) Run(stop <-chan struct{}) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	batch := make([]LogEntry, 0, 512)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		q.write(batch)
		batch = batch[:0]
	}
	for {
		select {
		case e := <-q.ch:
			batch = append(batch, e)
			if len(batch) >= 512 {
				flush()
			}
		case <-t.C:
			flush()
		case <-stop:
			for {
				select {
				case e := <-q.ch:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (q *QueryLog) write(batch []LogEntry) {
	tx, err := q.db.Begin()
	if err != nil {
		return
	}
	st, err := tx.Prepare(`INSERT INTO dns_queries(ts,client,client_name,qname,qtype,result,rcode,upstream,latency_us,rule) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return
	}
	for _, e := range batch {
		st.Exec(e.TS, e.Client, e.ClientName, e.QName, e.QType, e.Result, e.RCode, e.Upstream, e.LatencyUS, e.Rule)
	}
	st.Close()
	tx.Commit()
}

// Prune deletes entries older than the retention period.
func (q *QueryLog) Prune(days int) {
	if days <= 0 {
		return
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	q.db.Exec(`DELETE FROM dns_queries WHERE ts<?`, cut)
}

// Clear removes every entry.
func (q *QueryLog) Clear() { q.db.Exec(`DELETE FROM dns_queries`) }

// Filter selects log rows.
type Filter struct {
	Text   string // matches client, client name or domain
	Result string
	Since  int64
	Before int64 // for paging: rows with id lower than this
	// BeforeTS limits to rows at or before a time, for paging a merged log.
	BeforeTS int64
	// NotResult leaves out one result, such as "blocked".
	NotResult string
	Limit     int
}

type Row struct {
	ID int64 `json:"id"`
	LogEntry
}

// Search returns recent entries, newest first.
func (q *QueryLog) Search(f Filter) ([]Row, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var where []string
	var args []any
	if t := strings.TrimSpace(f.Text); t != "" {
		where = append(where, `(client LIKE ? OR client_name LIKE ? OR qname LIKE ?)`)
		like := "%" + strings.ReplaceAll(strings.ReplaceAll(t, "%", ""), "_", "") + "%"
		args = append(args, like, like, like)
	}
	if f.Result != "" {
		where = append(where, `result=?`)
		args = append(args, f.Result)
	}
	if f.Since > 0 {
		where = append(where, `ts>=?`)
		args = append(args, f.Since)
	}
	if f.Before > 0 {
		where = append(where, `id<?`)
		args = append(args, f.Before)
	}
	if f.BeforeTS > 0 {
		where = append(where, `ts<=?`)
		args = append(args, f.BeforeTS)
	}
	if f.NotResult != "" {
		where = append(where, `result<>?`)
		args = append(args, f.NotResult)
	}
	sqlStr := `SELECT id,ts,client,client_name,qname,qtype,result,rcode,upstream,latency_us,rule FROM dns_queries`
	if len(where) > 0 {
		sqlStr += ` WHERE ` + strings.Join(where, " AND ")
	}
	sqlStr += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := q.db.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.TS, &r.Client, &r.ClientName, &r.QName, &r.QType, &r.Result, &r.RCode, &r.Upstream, &r.LatencyUS, &r.Rule); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Top is one entry of a ranking.
type Top struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	N     int64  `json:"n"`
}

// TopClients ranks clients by query count since the cutoff.
func (q *QueryLog) TopClients(since int64, n int) ([]Top, error) {
	return q.top(`SELECT client, MAX(client_name), COUNT(*) c FROM dns_queries WHERE ts>=? GROUP BY client ORDER BY c DESC LIMIT ?`, since, n)
}

// TopBlocked ranks blocked domains.
func (q *QueryLog) TopBlocked(since int64, n int) ([]Top, error) {
	return q.top(`SELECT qname, '', COUNT(*) c FROM dns_queries WHERE ts>=? AND result='blocked' GROUP BY qname ORDER BY c DESC LIMIT ?`, since, n)
}

// TopDomains ranks all queried domains.
func (q *QueryLog) TopDomains(since int64, n int) ([]Top, error) {
	return q.top(`SELECT qname, '', COUNT(*) c FROM dns_queries WHERE ts>=? GROUP BY qname ORDER BY c DESC LIMIT ?`, since, n)
}

func (q *QueryLog) top(query string, since int64, n int) ([]Top, error) {
	rows, err := q.db.Query(query, since, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Top
	for rows.Next() {
		var t Top
		var label sql.NullString
		if err := rows.Scan(&t.Key, &label, &t.N); err != nil {
			return nil, err
		}
		t.Label = label.String
		out = append(out, t)
	}
	return out, rows.Err()
}

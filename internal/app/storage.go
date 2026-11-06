package app

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"nopewarden/internal/api"
)

// Storage: what is kept, how much room it takes, and the rule that decides when it goes. Every table
// that grows has a rule here, so nothing is kept forever by accident.

const (
	defaultLongDays = 365 // alerts and outages
	defaultRevKeep  = 200 // firewall change copies kept for going back
)

type storTable struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Policy string `json:"policy"` // plain words: when it is removed
	db     string
	table  string
	ts     string // time column; empty when the table has none
}

func (a *App) storTables() []storTable {
	days := a.logDays()
	long := a.longDays()
	return []storTable{
		{"connections", "Connections", fmt.Sprintf("%d days, and trimmed first when the size limit is reached", days), "logs", "fw_events", "ts"},
		{"lookups", "DNS lookups", fmt.Sprintf("%d days, and trimmed first when the size limit is reached", days), "logs", "dns_queries", "ts"},
		{"events", "What happened (activity)", fmt.Sprintf("%d days", days), "logs", "events", "ts"},
		{"traffic", "Data used by each device", fmt.Sprintf("%d days", days), "logs", "traffic", "ts"},
		{"countries", "Data by country", fmt.Sprintf("%d days", days), "logs", "country_traffic", "ts"},
		{"history", "Overview charts", "Kept in three steps: fine for 2 hours, medium for 25 hours, coarse for 31 days", "logs", "metrics", "ts"},
		{"speed", "Speed tests", "365 days", "logs", "speedtests", "ts"},
		{"threats", "Threat alerts", fmt.Sprintf("%d days", long), "logs", "threat_alerts", "ts"},
		{"outages", "Internet outages", fmt.Sprintf("%d days", long), "logs", "outages", "start"},
		{"revisions", "Firewall change copies", fmt.Sprintf("The newest %d, plus the one in use and any waiting to be kept", a.revKeep()), "config", "fw_revisions", "created_at"},
	}
}

func (a *App) logDays() int { return int(a.settingFloat("logs.retention_days", 30)) }
func (a *App) longDays() int {
	return clampInt(int(a.settingFloat("logs.long_days", defaultLongDays)), 7, 3650)
}
func (a *App) revKeep() int {
	return clampInt(int(a.settingFloat("fw.revisions_keep", defaultRevKeep)), 20, 5000)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// pruneLong removes the older rows of the tables that follow their own, longer rule.
func (a *App) pruneLong() {
	cut := time.Now().Add(-time.Duration(a.longDays()) * 24 * time.Hour).Unix()
	a.Logs.Exec(`DELETE FROM threat_alerts WHERE ts<?`, cut)
	a.Logs.Exec(`DELETE FROM outages WHERE start<? AND end>0`, cut)
	keep := a.revKeep()
	// Keep the newest few, the copy in use, and anything waiting for a decision.
	a.Config.Exec(`DELETE FROM fw_revisions WHERE status NOT IN ('pending','confirmed') AND version NOT IN (SELECT version FROM fw_revisions ORDER BY version DESC LIMIT ?)`, keep)
}

// sizeEstimate is the rough space a table takes: its rows times the average width of a sample.
// SQLite does not report this per table without an extra module, so it is an estimate, and the
// console says so.
func (a *App) sizeEstimate(t storTable, rows int64) int64 {
	if rows == 0 {
		return 0
	}
	db := a.Logs
	if t.db == "config" {
		db = a.Config
	}
	cols, err := db.Query(`SELECT name FROM pragma_table_info(?)`, t.table)
	if err != nil {
		return 0
	}
	var parts []string
	for cols.Next() {
		var n string
		cols.Scan(&n)
		parts = append(parts, `COALESCE(LENGTH(CAST("`+n+`" AS BLOB)),0)`)
	}
	cols.Close()
	if len(parts) == 0 {
		return 0
	}
	var avg float64
	db.QueryRow(`SELECT COALESCE(AVG(w),0) FROM (SELECT ` + strings.Join(parts, "+") + ` AS w FROM ` + t.table + ` ORDER BY rowid DESC LIMIT 2000)`).Scan(&avg)
	return int64((avg + 12) * float64(rows))
}

type storRow struct {
	storTable
	Rows   int64 `json:"rows"`
	Bytes  int64 `json:"bytes"`
	Oldest int64 `json:"oldest"`
}

func (a *App) storageReport() map[string]any {
	var out []storRow
	for _, t := range a.storTables() {
		db := a.Logs
		if t.db == "config" {
			db = a.Config
		}
		r := storRow{storTable: t}
		db.QueryRow(`SELECT COUNT(*) FROM ` + t.table).Scan(&r.Rows)
		if t.ts != "" && r.Rows > 0 {
			db.QueryRow(`SELECT COALESCE(MIN(` + t.ts + `),0) FROM ` + t.table).Scan(&r.Oldest)
		}
		r.Bytes = a.sizeEstimate(t, r.Rows)
		out = append(out, r)
	}
	usage := a.logUsage()
	var st syscall.Statfs_t
	var free, total int64
	if syscall.Statfs(a.Config.Path, &st) == nil {
		free, total = int64(st.Bavail)*int64(st.Bsize), int64(st.Blocks)*int64(st.Bsize)
	}
	var cfgPages, cfgSize int64
	a.Config.QueryRow(`PRAGMA page_count`).Scan(&cfgPages)
	a.Config.QueryRow(`PRAGMA page_size`).Scan(&cfgSize)
	return map[string]any{
		"tables": out, "logs_bytes": usage["used_bytes"], "config_bytes": cfgPages * cfgSize,
		"disk_free": free, "disk_total": total,
		"days": a.logDays(), "long_days": a.longDays(), "max_mb": int(a.settingFloat("logs.max_mb", 200)), "revisions_keep": a.revKeep(),
		"last_prune": usage["last_prune"],
	}
}

func (a *App) registerStorageRoutes() {
	a.API.Handle("GET /api/logs/storage", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.storageReport())
	})
	a.API.HandleAdmin("GET /api/logs/admin", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		q := r.URL.Query()
		hours, _ := strconv.Atoi(q.Get("hours"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		rows, total := a.adminActivity(q.Get("q"), hours, limit, before)
		api.JSON(w, 200, map[string]any{"rows": rows, "total": total, "days": a.logDays()})
	})
	a.API.HandleAdmin("PUT /api/logs/storage", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			LongDays      int `json:"long_days"`
			RevisionsKeep int `json:"revisions_keep"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.LongDays < 7 || in.LongDays > 3650 {
			fail(w, fmt.Errorf("keep alerts and outages between 7 and 3650 days"))
			return
		}
		if in.RevisionsKeep < 20 || in.RevisionsKeep > 5000 {
			fail(w, fmt.Errorf("keep between 20 and 5000 firewall change copies"))
			return
		}
		a.Config.SetSetting("logs.long_days", strconv.Itoa(in.LongDays))
		a.Config.SetSetting("fw.revisions_keep", strconv.Itoa(in.RevisionsKeep))
		a.pruneLong()
		api.JSON(w, 200, a.storageReport())
	})
}

// auditEvent records who signed in or out and from where. It is a plain log entry in the "admin"
// area, shown on Logs, Admin activity. It does not raise an alert by itself.
func (a *App) auditEvent(actor, sev, title, detail string) {
	if a.Logs == nil {
		return
	}
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, time.Now().Unix(), "admin", sev, actor, title, detail)
	a.Bus.Publish("events", map[string]int64{"at": time.Now().Unix()})
}

type adminRow struct {
	ID     int64  `json:"id"`
	TS     int64  `json:"ts"`
	Who    string `json:"who"`
	What   string `json:"what"`
	Detail string `json:"detail"`
	Sev    string `json:"severity"`
}

// adminActivity lists what people did to NopeWarden: sign-ins and failed attempts, and every change
// an administrator made that was recorded. The search words must all appear somewhere in the row.
func (a *App) adminActivity(q string, hours, limit int, before int64) ([]adminRow, int) {
	names := []any{}
	if rows, err := a.Config.Query(`SELECT username FROM users`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			names = append(names, n)
		}
		rows.Close()
	}
	where := []string{`(area='admin'`}
	args := []any{}
	if len(names) > 0 {
		where[0] += ` OR actor IN (` + strings.TrimSuffix(strings.Repeat("?,", len(names)), ",") + `)`
		args = append(args, names...)
	}
	where[0] += `)`
	if hours > 0 {
		where = append(where, `ts>=?`)
		args = append(args, time.Now().Add(-time.Duration(hours)*time.Hour).Unix())
	}
	if before > 0 {
		where = append(where, `id<?`)
		args = append(args, before)
	}
	for _, w := range strings.Fields(q) {
		l := like(w)
		where = append(where, `(actor LIKE ? OR title LIKE ? OR detail LIKE ? OR severity LIKE ?)`)
		args = append(args, l, l, l, l)
	}
	cond := strings.Join(where, " AND ")
	var total int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE `+cond, args...).Scan(&total)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := []adminRow{}
	rows, err := a.Logs.Query(`SELECT id,ts,actor,title,detail,severity FROM events WHERE `+cond+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err == nil {
		for rows.Next() {
			var r adminRow
			rows.Scan(&r.ID, &r.TS, &r.Who, &r.What, &r.Detail, &r.Sev)
			out = append(out, r)
		}
		rows.Close()
	}
	return out, total
}

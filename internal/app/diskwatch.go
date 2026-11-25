package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/netd"
)

// Closer drive watching. Every 15 minutes NopeWarden reads each drive, notes the numbers worth
// watching (sectors moved to spare, sectors waiting, cable errors, media errors, wear), and says so
// when one rises. It also tells you when a drive that was there has gone, when a drive starts
// reporting a problem, and when the system log shows a drive failing to read or write. Drives are
// also asked to run their own self-tests on a schedule.

const diskWatchEvery = 15 * time.Minute

// The counters that should never go up. Wear is shown but is expected to rise.
var diskWatched = []struct{ key, one, many string }{
	{"realloc", "sector moved to spare", "sectors moved to spare"},
	{"pending", "sector waiting to be moved", "sectors waiting to be moved"},
	{"uncorrectable", "sector that could not be read", "sectors that could not be read"},
	{"crc", "cable or connection error", "cable or connection errors"},
	{"media", "media error", "media errors"},
}

// diskTests says when each kind of self-test runs. Day is 0 for Sunday.
type diskTests struct {
	Short diskTestWhen `json:"short"`
	Long  diskTestWhen `json:"long"`
}
type diskTestWhen struct {
	On    bool   `json:"on"`
	Day   int    `json:"day"`
	At    string `json:"at"`    // 03:00
	Every string `json:"every"` // week or month (first such day of the month)
}

func defaultDiskTests() diskTests {
	return diskTests{
		Short: diskTestWhen{On: true, Day: 0, At: "03:00", Every: "week"},
		Long:  diskTestWhen{On: true, Day: 0, At: "03:30", Every: "month"},
	}
}

func (a *App) diskTests() diskTests {
	c := defaultDiskTests()
	if raw := a.setting("disk.tests", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	for _, w := range []*diskTestWhen{&c.Short, &c.Long} {
		if w.Day < 0 || w.Day > 6 {
			w.Day = 0
		}
		if _, ok := clockMinutes(w.At); !ok {
			w.At = "03:00"
		}
		if w.Every != "week" && w.Every != "month" {
			w.Every = "week"
		}
	}
	return c
}

// testDue says which self-test (if any) should start now. ran holds the date each kind last started.
// Long wins when both are due, so a drive is not asked for two tests at once.
func testDue(c diskTests, now time.Time, ran map[string]string) string {
	today := now.Format("2006-01-02")
	due := func(w diskTestWhen, kind string) bool {
		if !w.On || int(now.Weekday()) != w.Day || ran[kind] == today {
			return false
		}
		if w.Every == "month" && now.Day() > 7 {
			return false
		}
		at, _ := clockMinutes(w.At)
		return now.Hour()*60+now.Minute() >= at
	}
	if due(c.Long, "long") {
		return "long"
	}
	if due(c.Short, "short") {
		return "short"
	}
	return ""
}

// diskState is what the watcher remembers between readings.
type diskState struct {
	Seen   map[string]string `json:"seen"`   // drive name -> model
	Gone   map[string]int64  `json:"gone"`   // drive name -> when it was first missed
	Level  map[string]string `json:"level"`  // drive name -> level at the last reading
	Raised map[string]int64  `json:"raised"` // what was last told, by key, so one thing is told once
	Ran    map[string]string `json:"ran"`    // self-test kind -> date it last started
	Miss   map[string]int    `json:"miss"`   // drive name -> readings in a row without it
}

func (a *App) diskState() diskState {
	var s diskState
	if raw := a.setting("disk.state", ""); raw != "" {
		json.Unmarshal([]byte(raw), &s)
	}
	for _, m := range []*map[string]string{&s.Seen, &s.Ran} {
		if *m == nil {
			*m = map[string]string{}
		}
	}
	if s.Gone == nil {
		s.Gone = map[string]int64{}
	}
	if s.Level == nil {
		s.Level = map[string]string{}
	}
	if s.Raised == nil {
		s.Raised = map[string]int64{}
	}
	if s.Miss == nil {
		s.Miss = map[string]int{}
	}
	return s
}

func (a *App) saveDiskState(s diskState) {
	b, _ := json.Marshal(s)
	a.Config.SetSetting("disk.state", string(b))
}

// diskLast is the newest stored value of a counter, and when it was stored.
func (a *App) diskLast(drive, key string) (val, ts int64, ok bool) {
	rows, err := a.Logs.Query(`SELECT value,ts FROM disk_counters WHERE drive=? AND key=? ORDER BY ts DESC LIMIT 1`, drive, key)
	if err != nil {
		return 0, 0, false
	}
	defer rows.Close()
	if rows.Next() {
		rows.Scan(&val, &ts)
		return val, ts, true
	}
	return 0, 0, false
}

// diskBase is the value a counter had a week ago, or the oldest value held when less than a week of
// history exists. since is how long before now that value was noted.
func (a *App) diskBase(drive, key string, now time.Time) (val int64, since time.Duration, ok bool) {
	week := now.Add(-7 * 24 * time.Hour).Unix()
	q := func(sql string, args ...any) bool {
		rows, err := a.Logs.Query(sql, args...)
		if err != nil {
			return false
		}
		defer rows.Close()
		if rows.Next() {
			var ts int64
			rows.Scan(&val, &ts)
			since = now.Sub(time.Unix(ts, 0))
			return true
		}
		return false
	}
	if q(`SELECT value,ts FROM disk_counters WHERE drive=? AND key=? AND ts<=? ORDER BY ts DESC LIMIT 1`, drive, key, week) {
		return val, since, true
	}
	if q(`SELECT value,ts FROM disk_counters WHERE drive=? AND key=? ORDER BY ts ASC LIMIT 1`, drive, key) {
		return val, since, true
	}
	return 0, 0, false
}

// trendLine is the plain sentence for a counter that rose: "3 more sectors moved to spare than a week ago".
func trendLine(key string, rise int64, since time.Duration) string {
	for _, w := range diskWatched {
		if w.key == key {
			noun := w.many
			if rise == 1 {
				noun = w.one
			}
			return fmt.Sprintf("%d more %s than %s", rise, noun, agoWord(since))
		}
	}
	return ""
}

func agoWord(d time.Duration) string {
	switch {
	case d >= 6*24*time.Hour:
		return "a week ago"
	case d >= 36*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24+0.5))
	case d >= 20*time.Hour:
		return "a day ago"
	case d < time.Hour:
		return "a few minutes ago"
	default:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
}

// annotateDisks adds the trend lines and raises a drive to amber when a counter rose over the last
// week, when the system log shows it failing, or when it has gone. It never lowers a level.
func (a *App) annotateDisks(rep netd.DiskReport, now time.Time) netd.DiskReport {
	if a.Logs == nil {
		return rep
	}
	st := a.diskState()
	have := map[string]bool{}
	rep.Drives = append([]netd.Drive(nil), rep.Drives...) // the reading may be shared; work on a copy
	for i := range rep.Drives {
		d := &rep.Drives[i]
		d.Trend = nil
		have[d.Name] = true
		for _, w := range diskWatched {
			cur, ok := d.Counters[w.key]
			if !ok {
				continue
			}
			if base, since, ok := a.diskBase(d.Name, w.key, now); ok && cur > base {
				d.Trend = append(d.Trend, trendLine(w.key, cur-base, since))
			}
		}
		raise := func(level, why string) {
			rank := map[string]int{"": 0, "info": 1, "warn": 2, "bad": 3}
			if rank[level] > rank[d.Level] {
				d.Word, d.Level, d.Reason = "Warning", level, why
			}
		}
		if len(d.Trend) > 0 {
			raise("warn", "It has got worse: "+d.Trend[0]+".")
		}
		if len(d.LogErrors) > 0 {
			raise("warn", "The system log shows this drive failing to read or write in the last day.")
		}
	}
	// A drive that was there and is not any more.
	names := make([]string, 0, len(st.Gone))
	for n := range st.Gone {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if have[n] {
			continue
		}
		model := st.Seen[n]
		if model == "" {
			model = "/dev/" + n
		}
		rep.Drives = append(rep.Drives, netd.Drive{
			Name: n, Model: model, Kind: "Disk", Word: "Warning", Level: "bad", Wear: -1, Gone: true,
			Reason: "This drive was here before and NopeWarden can no longer find it. Check the cable and the power to it.",
		})
	}
	return rep
}

// diskWatch is one reading: store the counters, tell what changed, start any self-test that is due.
func (a *App) diskWatch(now time.Time) {
	if a.Net == nil || a.Logs == nil {
		return
	}
	rep := a.diskReport(0)
	if len(rep.Drives) == 0 {
		return
	}
	st := a.diskState()
	told := func(key string, every time.Duration) bool { // true when this has not been told lately
		if t, ok := st.Raised[key]; ok && now.Sub(time.Unix(t, 0)) < every {
			return false
		}
		st.Raised[key] = now.Unix()
		return true
	}
	here := map[string]bool{}
	var real []netd.Drive
	for _, d := range rep.Drives {
		if d.Gone {
			continue
		}
		here[d.Name] = true
		if d.Kind != "Virtual disk" {
			st.Seen[d.Name] = d.Model
			real = append(real, d)
		}
		delete(st.Gone, d.Name)
		delete(st.Miss, d.Name)
		if d.Kind == "Virtual disk" || d.Word == "Not reported" {
			continue
		}
		name := d.Model + " (" + d.Name + ")"
		for _, w := range diskWatched {
			cur, ok := d.Counters[w.key]
			if !ok {
				continue
			}
			last, lts, had := a.diskLast(d.Name, w.key)
			if had && cur > last {
				msg := fmt.Sprintf("%d more %s since the last reading", cur-last, pluralWord(cur-last, w.one, w.many))
				a.event("drives", "warning", "A drive's error count went up: "+name, msg)
				a.alert("drive", "Drive problem: "+d.Model, fmt.Sprintf("%s: %s. It now has %d in total.", name, msg, cur))
			}
			if !had || cur != last || now.Unix()-lts >= 6*3600 {
				a.Logs.Exec(`INSERT INTO disk_counters(ts,drive,key,value) VALUES(?,?,?,?)`, now.Unix(), d.Name, w.key, cur)
			}
		}
		if w, ok := d.Counters["wear"]; ok {
			if _, lts, had := a.diskLast(d.Name, "wear"); !had || now.Unix()-lts >= 6*3600 {
				a.Logs.Exec(`INSERT INTO disk_counters(ts,drive,key,value) VALUES(?,?,?,?)`, now.Unix(), d.Name, "wear", w)
			}
		}
		if len(d.LogErrors) > 0 && told("log:"+d.Name, 6*time.Hour) {
			a.event("drives", "warning", "The system log shows disk errors: "+name, strings.Join(d.LogErrors, "\n"))
			a.alert("drive", "Disk errors in the system log: "+d.Model, name+" failed to read or write in the last day. Back up now and check the cable.")
		}
		// A drive that has started to report a problem.
		bad := d.Level == "warn" || d.Level == "bad"
		was := st.Level[d.Name]
		if bad && was != "warn" && was != "bad" && d.Word == "Warning" && told("level:"+d.Name, 24*time.Hour) {
			a.event("drives", "warning", "A drive reports a problem: "+name, d.Reason)
			a.alert("drive", "Drive problem: "+d.Model, name+": "+d.Reason)
		}
		st.Level[d.Name] = d.Level
	}
	// A drive seen before that has not shown up on two readings in a row.
	for n := range st.Seen {
		if here[n] {
			continue
		}
		st.Miss[n]++
		if st.Miss[n] >= 2 {
			if _, already := st.Gone[n]; !already {
				st.Gone[n] = now.Unix()
				a.event("drives", "error", "A drive has gone: "+st.Seen[n], "NopeWarden could see /dev/"+n+" before and cannot now. Check its cable and power.")
				a.alert("drive", "A drive has disappeared", fmt.Sprintf("%s (/dev/%s) was there before and cannot be found now. Check its cable and power.", st.Seen[n], n))
			}
		}
	}
	// Self-tests.
	if kind := testDue(a.diskTests(), now, st.Ran); kind != "" && rep.Tool {
		st.Ran[kind] = now.Format("2006-01-02")
		for _, d := range real {
			if d.Word == "Not reported" || (d.Test != nil && d.Test.Running) {
				continue
			}
			if err := a.Net.DiskSelfTest(d.Name, kind); err != nil {
				a.event("drives", "warning", "A drive self-test did not start: "+d.Model, err.Error())
				continue
			}
			a.event("drives", "info", "Started a "+kind+" self-test on "+d.Model, "The drive runs it by itself and keeps working. The result shows on Drive health.")
		}
	}
	a.saveDiskState(st)
	a.dropCached("disk-health")
}

func pluralWord(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (a *App) diskLoop(ctx context.Context) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.diskWatch(time.Now())
			t.Reset(diskWatchEvery)
		}
	}
}

// lastBackupCopy says whether a backup has been downloaded to somewhere other than this machine.
func (a *App) lastBackupDownload() int64 {
	bkHistMu.Lock()
	defer bkHistMu.Unlock()
	var at int64
	for _, e := range a.readBackupHistoryLocked() {
		if e.Action == "downloaded" && e.At > at {
			at = e.At
		}
	}
	return at
}

// diskView is what the Drive health card shows: the drives, whether alerts for them are on, whether a
// backup is kept somewhere other than this machine, and when self-tests run.
type diskView struct {
	netd.DiskReport
	Alerts   diskAlerts `json:"alerts"`
	Backup   diskBackup `json:"backup"`
	Schedule diskTests  `json:"schedule"`
	Power    string     `json:"power"` // one line about the UPS, when one is watched
}
type diskAlerts struct {
	On      bool `json:"on"`      // the "drive problem" alert is switched on
	Channel bool `json:"channel"` // email or phone alerts are set up
}
type diskBackup struct {
	Off  bool   `json:"off"` // a copy exists somewhere other than this machine
	Note string `json:"note"`
}

func (a *App) diskViewNow(maxAge time.Duration) diskView {
	c := a.alertConfig()
	v := diskView{DiskReport: a.diskReport(maxAge), Schedule: a.diskTests()}
	v.Alerts = diskAlerts{On: c.On["drive"], Channel: c.Email.Enabled || c.Ntfy.Enabled}
	v.Power = a.powerOneLine()
	at := a.lastBackupDownload()
	switch {
	case at == 0:
		v.Backup.Note = "Backups are kept on this machine's own drive, and none has been downloaded to another place. If the drive fails, the backups go with it. Download one from Settings, Backup & recovery and keep it elsewhere."
	case time.Since(time.Unix(at, 0)) > 30*24*time.Hour:
		v.Backup.Note = fmt.Sprintf("Backups are kept on this machine's own drive. The last copy taken elsewhere was %d days ago. Download a fresh one from Settings, Backup & recovery.", int(time.Since(time.Unix(at, 0)).Hours()/24))
	default:
		v.Backup.Off = true
		v.Backup.Note = fmt.Sprintf("A backup was downloaded %s, so a copy exists away from this drive.", agoDays(at))
	}
	return v
}

func agoDays(at int64) string {
	d := int(time.Since(time.Unix(at, 0)).Hours() / 24)
	switch d {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", d)
}

func (a *App) registerDiskWatchRoutes() {
	s := a.API
	s.Handle("GET /api/disk/schedule", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.diskTests())
	})
	s.HandleAdmin("PUT /api/disk/schedule", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in diskTests
		if !api.Decode(w, r, &in) {
			return
		}
		for _, x := range []diskTestWhen{in.Short, in.Long} {
			if x.Day < 0 || x.Day > 6 {
				api.Error(w, 400, "pick a day of the week")
				return
			}
			if _, ok := clockMinutes(x.At); !ok {
				api.Error(w, 400, "the time should look like 03:00")
				return
			}
		}
		b, _ := json.Marshal(in)
		a.Config.SetSetting("disk.tests", string(b))
		a.auditEvent(c.User.Username, "info", "Changed the drive self-test schedule", "")
		api.JSON(w, 200, a.diskTests())
	})
	s.HandleAdmin("POST /api/disk/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct{ Drive, Kind string }
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.Net.DiskSelfTest(in.Drive, in.Kind); err != nil {
			api.Error(w, 400, err.Error())
			return
		}
		a.event(c.User.Username, "info", "Started a "+in.Kind+" self-test on /dev/"+in.Drive, "The drive runs it by itself and keeps working.")
		a.dropCached("disk-health")
		api.JSON(w, 200, map[string]bool{"ok": true})
	})
}

// driveSummaryLines is what the Daily summary says about drives: "healthy, no change" or what is wrong.
func (a *App) driveSummaryLines() []string {
	var out []string
	for _, d := range a.diskReport(30 * time.Minute).Drives {
		switch {
		case d.Word == "Not reported":
			continue
		case d.Gone:
			out = append(out, fmt.Sprintf("The drive %s cannot be found any more.", d.Model))
		case d.Level == "" || d.Word == "Healthy":
			out = append(out, fmt.Sprintf("The drive %s is healthy, no change.", d.Model))
		case len(d.Trend) > 0:
			out = append(out, fmt.Sprintf("The drive %s has %s.", d.Model, strings.ToLower(d.Trend[0][:1])+d.Trend[0][1:]))
		default:
			out = append(out, fmt.Sprintf("The drive %s: %s", d.Model, d.Reason))
		}
	}
	return out
}

package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/sched"
)

// One schedule control for every recurring job. A job's schedule is kept as text in the settings
// ("every:12", "daily:03:30", "weekly:1:03:30" or "off"); sched decides when it is due. Each job's
// own loop asks here instead of keeping a clock of its own.

type schedJob struct {
	ID, Name, Help, Default string
	AllowOff                bool
	Last                    func(a *App) time.Time
	Run                     func(a *App, ctx context.Context) error
}

var schedJobs = []schedJob{
	{ID: "updates", Name: "Look for a new version", Default: "every:12", AllowOff: false,
		Help: "How often NopeWarden asks whether a new version or new system packages are ready. Installing stays under Updates, with its own start hour.",
		Last: func(a *App) time.Time { a.upd.mu.Lock(); defer a.upd.mu.Unlock(); return a.upd.checkedAt },
		Run:  func(a *App, ctx context.Context) error { a.checkUpdate(ctx); return nil }},
	{ID: "lists", Name: "Block lists", Default: "daily:04:10", AllowOff: false,
		Help: "When the lists of names to block are downloaded again.",
		Last: func(a *App) time.Time {
			var t int64
			a.Config.QueryRow(`SELECT COALESCE(MAX(last_ok),0) FROM blocklists WHERE local=0`).Scan(&t)
			if t == 0 {
				return time.Time{}
			}
			return time.Unix(t, 0)
		},
		Run: func(a *App, ctx context.Context) error {
			ids, err := a.Lists.Lists()
			if err != nil {
				return err
			}
			var first error
			for _, l := range ids {
				if !l.Enabled {
					continue
				}
				if e := a.Lists.Refresh(ctx, l.ID); e != nil && first == nil {
					first = e
				}
			}
			return first
		}},
	{ID: "geo", Name: "Country address data", Default: "weekly:0:03:40", AllowOff: false,
		Help: "When the list of which addresses belong to which country is downloaded again. It feeds the map, the logs and Countries rules.",
		Last: func(a *App) time.Time {
			age, ok := a.geoAge()
			if !ok {
				return time.Time{}
			}
			return time.Now().Add(-age)
		},
		Run: func(a *App, ctx context.Context) error { a.geoNow(); return nil }},
	{ID: "threat", Name: "Threat rules", Default: "daily:03:20", AllowOff: true,
		Help: "When the rules that recognise attacks are fetched again. Only used while threat watching is on.",
		Last: func(a *App) time.Time { return a.schedLastSetting("threat") },
		Run: func(a *App, ctx context.Context) error {
			if a.Net == nil {
				return errNoHelper
			}
			return a.threatRulesUpdate()
		}},
}

func schedFind(id string) (schedJob, bool) {
	for _, j := range schedJobs {
		if j.ID == id {
			return j, true
		}
	}
	return schedJob{}, false
}

func (a *App) schedLastSetting(id string) time.Time {
	n, _ := strconv.ParseInt(a.Config.Setting("sched."+id+".last", "0"), 10, 64)
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func (a *App) schedMarkDone(id string) {
	a.Config.SetSetting("sched."+id+".last", strconv.FormatInt(time.Now().Unix(), 10))
}

// schedSpec is the job's schedule: the owner's choice, or the default when none or an unusable one is stored.
func (a *App) schedSpec(id string) sched.Spec {
	j, _ := schedFind(id)
	if s, err := sched.Parse(a.Config.Setting("sched."+id, j.Default)); err == nil {
		return s
	}
	s, _ := sched.Parse(j.Default)
	return s
}

// schedDue says whether the job should run now.
func (a *App) schedDue(id string) bool {
	j, ok := schedFind(id)
	if !ok {
		return false
	}
	return a.schedSpec(id).Due(j.Last(a), time.Now())
}

// threatRulesUpdate fetches the rules and remembers when it worked.
func (a *App) threatRulesUpdate() error {
	if err := a.Net.ThreatUpdateRules(); err != nil {
		return err
	}
	a.schedMarkDone("threat")
	a.threatRulesUpdatedNote(time.Now())
	return nil
}

type schedView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Help     string `json:"help"`
	Spec     string `json:"spec"`
	Plain    string `json:"plain"`
	Last     int64  `json:"last"` // unix seconds, 0 = never
	Next     int64  `json:"next"` // unix seconds, 0 = not scheduled
	AllowOff bool   `json:"allow_off"`
	Default  string `json:"default"`
}

func (a *App) schedViewOf(j schedJob) schedView {
	s := a.schedSpec(j.ID)
	last := j.Last(a)
	v := schedView{ID: j.ID, Name: j.Name, Help: j.Help, Spec: s.String(), Plain: s.Plain(), AllowOff: j.AllowOff, Default: j.Default}
	if !last.IsZero() {
		v.Last = last.Unix()
	}
	if n := s.Next(last, time.Now()); !n.IsZero() {
		v.Next = n.Unix()
	}
	return v
}

func (a *App) registerScheduleRoutes() {
	s := a.API
	s.Handle("GET /api/schedules", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		out := []schedView{}
		for _, j := range schedJobs {
			out = append(out, a.schedViewOf(j))
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("PUT /api/schedules/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		j, ok := schedFind(r.PathValue("id"))
		if !ok {
			api.Error(w, 404, "There is no such schedule.")
			return
		}
		var in struct {
			Spec string `json:"spec"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		sp, err := sched.Parse(in.Spec)
		if err != nil {
			fail(w, err)
			return
		}
		if sp.Kind == "off" && !j.AllowOff {
			fail(w, errors.New("This one can't be turned off. Choose how often instead."))
			return
		}
		a.Config.SetSetting("sched."+j.ID, sp.String())
		if j.ID == "updates" && sp.Kind == "every" {
			a.Config.SetSetting(updateEverySetting, strconv.Itoa(sp.Hours))
		}
		api.JSON(w, 200, a.schedViewOf(j))
	})
	s.HandleAdmin("POST /api/schedules/{id}/run", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		j, ok := schedFind(r.PathValue("id"))
		if !ok {
			api.Error(w, 404, "There is no such schedule.")
			return
		}
		if err := j.Run(a, r.Context()); err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, a.schedViewOf(j))
	})
}

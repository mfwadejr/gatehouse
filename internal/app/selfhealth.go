package app

import (
	"fmt"
	"net/http"
	"nopewarden/internal/store"
	"os"
	"time"

	"nopewarden/internal/api"
)

// The Diagnostics verdict and self-health summary: one sentence first, problems first, details below.

type healthIssue struct {
	Level string `json:"level"` // warn or bad
	Text  string `json:"text"`
	To    string `json:"to,omitempty"` // where to look
}

type healthFact struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Level string `json:"level,omitempty"`
}

func agoSpan(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < 2*time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

// clockSynced says whether the clock has been set from the internet: yes, no, or "" when this
// machine does not use the built-in time service and so cannot tell.
func clockSynced() string {
	if _, err := os.Stat("/run/systemd/timesync/synchronized"); err == nil {
		return "yes"
	}
	if _, err := os.Stat("/run/systemd/timesync"); err == nil {
		return "no"
	}
	return ""
}

func (a *App) selfHealth() map[string]any {
	var issues []healthIssue
	var facts []healthFact
	now := time.Now()
	h := a.health.Health("/")

	// Services.
	failing := 0
	if a.Net != nil {
		for _, s := range a.Net.DiagServices() {
			if s.State == "failing" {
				failing++
				issues = append(issues, healthIssue{"bad", s.Title + " keeps stopping. " + s.Reason, "diagnostics/services"})
			}
		}
	}

	if msg := a.setting("fw.last_error", ""); msg != "" {
		issues = append(issues, healthIssue{"bad", "The last firewall change did not apply: " + msg, "firewall"})
	}

	for _, db := range []*store.DB{a.Config, a.Logs} {
		if n, last, at := db.WriteFailures(time.Hour); n > 0 {
			issues = append(issues, healthIssue{"bad", fmt.Sprintf("%d saves failed in the last hour, the latest %s ago: %s. If the disk is full or failing, settings and logs are not being kept.", n, agoSpan(now.Sub(at)), last), "settings/alerts"})
			break
		}
	}

	// Restarts: a process much younger than the machine was restarted on its own, or by an update.
	proc := now.Sub(a.startedAt)
	machine := time.Duration(h.Uptime) * time.Second
	facts = append(facts, healthFact{Label: "NopeWarden has been running for", Value: agoSpan(proc)})
	facts = append(facts, healthFact{Label: "The machine has been on for", Value: agoSpan(machine)})
	if h.Uptime > 0 && machine-proc > 2*time.Minute && proc < 24*time.Hour {
		issues = append(issues, healthIssue{"warn", "NopeWarden restarted " + agoSpan(proc) + " ago while the machine kept running. An update does this; if you did not update, look at the system log.", "diagnostics/log"})
	}

	// Firewall.
	var applied int64
	a.Config.QueryRow(`SELECT COALESCE(MAX(created_at),0) FROM fw_revisions WHERE status='confirmed'`).Scan(&applied)
	if applied > 0 {
		facts = append(facts, healthFact{Label: "Firewall last applied", Value: agoSpan(now.Sub(time.Unix(applied, 0))) + " ago"})
	} else {
		facts = append(facts, healthFact{Label: "Firewall last applied", Value: "never from this console"})
	}

	// Space.
	if h.DiskSize > 0 {
		lvl := ""
		if h.Disk >= 90 {
			lvl = "bad"
			issues = append(issues, healthIssue{"bad", fmt.Sprintf("The disk is %.0f %% full. Lower how long logs are kept, or clean up now.", h.Disk), "settings/alerts"})
		} else if h.Disk >= 80 {
			lvl = "warn"
			issues = append(issues, healthIssue{"warn", fmt.Sprintf("The disk is %.0f %% full.", h.Disk), "settings/alerts"})
		}
		facts = append(facts, healthFact{Label: "Disk", Value: fmt.Sprintf("%.0f %% used, %.1f GB free", h.Disk, float64(h.DiskFree)/1e9), Level: lvl})
	}
	usage := a.logUsage()
	if used, ok := usage["used_bytes"].(int64); ok {
		capMB := int64(a.settingFloat("logs.max_mb", 200))
		lvl := ""
		if capMB > 0 && used > capMB<<20*9/10 {
			lvl = "warn"
			issues = append(issues, healthIssue{"warn", "The logs are near their size limit, so the oldest entries are being removed early.", "settings/alerts"})
		}
		facts = append(facts, healthFact{Label: "Logs", Value: fmt.Sprintf("%.0f MB of a %d MB limit", float64(used)/1e6, capMB), Level: lvl})
	}

	// Clock.
	switch clockSynced() {
	case "yes":
		facts = append(facts, healthFact{Label: "Clock", Value: "set from the internet"})
	case "no":
		facts = append(facts, healthFact{Label: "Clock", Value: "not set from the internet yet", Level: "warn"})
		issues = append(issues, healthIssue{"warn", "The clock has not been set from the internet. Log times and certificates can be wrong until it is.", ""})
	default:
		facts = append(facts, healthFact{Label: "Clock", Value: "not checked on this machine"})
	}

	// Recent errors.
	var errs int
	a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE severity IN ('error','warning','warn') AND area<>'admin' AND ts>=?`, now.Add(-time.Hour).Unix()).Scan(&errs)
	facts = append(facts, healthFact{Label: "Warnings and errors in the last hour", Value: fmt.Sprint(errs)})
	if errs >= 5 {
		issues = append(issues, healthIssue{"warn", fmt.Sprintf("%d warnings or errors in the last hour.", errs), "logs/activity"})
	}

	// The console's own live connections.
	st := a.API.StreamStats()
	by, _ := st["by_reason"].(map[string]int64)
	dropped := by["a write failed or timed out"]
	facts = append(facts, healthFact{Label: "Console live connections", Value: fmt.Sprintf("%v open, %v dropped since start", st["open"], dropped)})
	if dropped >= 5 {
		issues = append(issues, healthIssue{"warn", fmt.Sprintf("The console's live connection dropped %d times. See Connections below for when and from where.", dropped), "diagnostics/services"})
	}

	level, text := "good", "Everything is working."
	if len(issues) > 0 {
		level = "warn"
		text = fmt.Sprintf("%d thing%s need%s a look.", len(issues), map[bool]string{true: "", false: "s"}[len(issues) == 1], map[bool]string{true: "s", false: ""}[len(issues) == 1])
		for _, i := range issues {
			if i.Level == "bad" {
				level = "bad"
			}
		}
	}
	// Problems first.
	ordered := []healthIssue{}
	for _, lv := range []string{"bad", "warn"} {
		for _, i := range issues {
			if i.Level == lv {
				ordered = append(ordered, i)
			}
		}
	}
	return map[string]any{"level": level, "text": text, "issues": ordered, "facts": facts}
}

func (a *App) registerSelfHealthRoutes() {
	a.API.HandleAdmin("GET /api/diag/health", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.selfHealth())
	})
	a.API.HandleAdmin("GET /api/diag/connections", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.API.StreamStats())
	})
}

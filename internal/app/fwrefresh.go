package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"

	"nopewarden/internal/fw"
)

// An update can change how the firewall rules are written without changing what they allow. The
// console used to count that as "changes waiting" after every such update, with nothing the person
// had done. Every applied firewall revision now records a fingerprint of the settings it came from
// (the model column), and the version of the program that wrote it (setting fw.rules_version). When
// the settings are still the applied ones and only the program has moved on, the difference is a
// refresh, not a change: Review says so, and the program applies the refreshed rules itself.

// revisionPrint is a short fingerprint of the firewall settings a ruleset was made from. Time moves
// on by itself, so it is left out; the rules that depend on the clock are made from it separately.
func revisionPrint(m *fw.Model) string {
	if m == nil {
		return ""
	}
	c := *m
	c.Now = time.Time{}
	raw, err := json.Marshal(&c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:12])
}

// stampRules records which version of the program wrote the live ruleset.
func (a *App) stampRules() { a.Config.SetSetting("fw.rules_version", a.Cfg.Version) }

// lastRevision is the live revision's ruleset text and the fingerprint recorded with it.
func (a *App) lastRevision() (ruleset, print string, ok bool) {
	if a.Config.QueryRow(`SELECT ruleset,model FROM fw_revisions WHERE status='confirmed' ORDER BY version DESC LIMIT 1`).Scan(&ruleset, &print) != nil {
		return "", "", false
	}
	return ruleset, print, true
}

// rulesRefreshOnly says the live rules differ from today's only because of how this program writes
// them: the settings are exactly the applied ones.
func (a *App) rulesRefreshOnly() bool {
	_, print, ok := a.lastRevision()
	if !ok || print == "" {
		return false
	}
	m, err := fw.Load(a.Config)
	return err == nil && revisionPrint(m) == print
}

// protectedTags lists the protected rules a ruleset carries, so a refresh can never drop one.
func protectedTags(script string) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(script, "\n") {
		if i := strings.Index(l, `comment "protected:`); i >= 0 {
			out[l[i:]] = true
		}
	}
	return out
}

// refreshSettle is how long the firewall gets to answer before the refreshed rules are kept.
var refreshSettle = 3 * time.Second

// refreshRulesIfNeeded applies the refreshed rules after an update when nothing but the program
// changed. It checks the new rules the way any apply does, keeps the old ones if anything protected
// would go missing, starts the usual safety timer, and keeps the rules only when the firewall
// answers afterwards. Otherwise the timer puts the old rules back and the change stays waiting.
func (a *App) refreshRulesIfNeeded() {
	if a.FW == nil {
		return
	}
	if _, _, pending := a.FW.Pending(); pending {
		return
	}
	if a.setting("fw.rules_version", "") == a.Cfg.Version {
		return // the live rules were written by this very version
	}
	old, print, ok := a.lastRevision()
	if !ok || print == "" {
		return // an older apply left no fingerprint: one manual commit stamps it
	}
	m, script, err := a.currentRuleset()
	if err != nil || script == old || revisionPrint(m) != print || a.geoMissing(m) {
		return
	}
	for tag := range protectedTags(old) {
		if !protectedTags(script)[tag] {
			log.Printf("firewall: the refreshed rules would drop a protected rule, so they were left waiting")
			return
		}
	}
	a.restorePoint()
	tok, _, err := a.FW.Apply(a.fullScript(m, script), confirmWindow, "NopeWarden")
	if err != nil {
		log.Printf("firewall: refresh after the update was not applied: %v", err)
		return
	}
	// Keep it only if the firewall answers with the new rules loaded; otherwise the timer undoes it.
	time.Sleep(refreshSettle)
	if c, cerr := a.FW.Counters(); cerr != nil || len(c) == 0 {
		log.Printf("firewall: refreshed rules not kept: the firewall did not answer with them loaded (%v)", cerr)
		return
	}
	if err := a.FW.Confirm(tok); err != nil {
		log.Printf("firewall: refreshed rules not kept: %v", err)
		return
	}
	a.Config.Exec(`UPDATE fw_revisions SET status='superseded' WHERE status='confirmed'`)
	a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status,model) VALUES(?,?,?,?,'confirmed',?)`,
		time.Now().Unix(), "NopeWarden", "Updated by NopeWarden: the rules are written a little differently. What is allowed has not changed.", script, print)
	a.stampRules()
	a.event("NopeWarden", "info", "Updated by NopeWarden", "NopeWarden was updated and now writes the firewall rules a little differently. What is allowed has not changed.")
	a.fwSync()
	a.publishFW()
	log.Printf("firewall: rules refreshed after the update to %s; what they allow is unchanged", a.Cfg.Version)
}

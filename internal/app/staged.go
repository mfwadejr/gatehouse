package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/update"
)

// Staged changes. A change saved "for later" waits, in plain words, on the Review page until the
// person commits everything at once or discards it. Gatehouse keeps:
//   - a journal of what was staged, in words, written when each change is saved; and
//   - one copy of the settings from just before the first staged change, so Discard puts everything
//     back exactly as it was running.
// Both are cleared the moment nothing is waiting any more (after a commit, or a discard).

const (
	stagedJournalKey = "staged.journal"
	stagedBaseKey    = "staged.base"
	stagedBaseKind   = "staged-base"
	stepUpMemory     = 15 * time.Minute
)

type stagedEntry struct {
	ID    int64  `json:"id"`
	TS    int64  `json:"ts"`
	Actor string `json:"actor"`
	Text  string `json:"text"`
}

var stagedMu sync.Mutex

// stagedPath says whether a request can stage a change to the firewall or the network.
func stagedPath(path string) bool {
	for _, end := range []string{"/apply", "/confirm", "/rollback", "/preview", "/discard"} {
		if strings.HasSuffix(path, end) {
			return false
		}
	}
	for _, p := range []string{"/api/fw/", "/api/net/", "/api/routes", "/api/vpn/", "/api/dhcp/", "/api/dns/", "/api/wan"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func (a *App) stagedJournal() []stagedEntry {
	var out []stagedEntry
	json.Unmarshal([]byte(a.Config.Setting(stagedJournalKey, "")), &out)
	if out == nil {
		out = []stagedEntry{}
	}
	return out
}

func (a *App) anyDirty() (fw, net bool) {
	fw, _ = a.fwStatus()["dirty"].(bool)
	net, _ = a.netStatus()["dirty"].(bool)
	return
}

// stagedBefore keeps the "as it was" copy, once, before the first change that could be staged.
func (a *App) stagedBefore(method, path string) {
	if !stagedPath(path) {
		return
	}
	stagedMu.Lock()
	defer stagedMu.Unlock()
	if a.Config.Setting(stagedBaseKey, "") != "" || len(a.stagedJournal()) > 0 {
		return
	}
	if f, n := a.anyDirty(); f || n {
		return // something was already waiting before staging was tracked; nothing clean to go back to
	}
	if name, err := a.saveBackup(stagedBaseKind); err == nil {
		a.Config.SetSetting(stagedBaseKey, filepath.Base(name))
	}
}

func (a *App) clearStaged() {
	if name := a.Config.Setting(stagedBaseKey, ""); name != "" {
		if p, err := a.backupPath(name); err == nil {
			os.Remove(p)
		}
	}
	a.Config.SetSetting(stagedBaseKey, "")
	a.Config.SetSetting(stagedJournalKey, "")
	a.Config.SetSetting("staged.upto", "0")
}

// stagedAfter writes what was just staged into the journal, in words; when nothing is waiting any
// more it clears the journal and the copy.
func (a *App) stagedAfter(actor, method, path string, body []byte) {
	area := ""
	switch {
	case strings.HasPrefix(path, "/api/fw/"):
		area = "fw"
	case strings.HasPrefix(path, "/api/net/"):
		area = "net"
	}
	apply, confirm := strings.HasSuffix(path, "/apply"), strings.HasSuffix(path, "/confirm")
	if !stagedPath(path) && !apply && !confirm && !strings.HasSuffix(path, "/rollback") {
		return
	}
	stagedMu.Lock()
	defer stagedMu.Unlock()
	f, n := a.anyDirty()
	j := a.stagedJournal()
	if !f && !n {
		if len(j) > 0 || a.Config.Setting(stagedBaseKey, "") != "" {
			a.clearStaged()
		}
		return
	}
	if apply && area != "" && len(j) > 0 {
		// Remember how far the list had got, and whether the other area still waits, so the entries
		// that went live can be dropped when this change is kept.
		other := n
		if area == "net" {
			other = f
		}
		a.Config.SetSetting("staged.upto", strconv.FormatInt(j[len(j)-1].ID, 10))
		a.Config.SetSetting("staged.other", strconv.FormatBool(other))
		return
	}
	if confirm {
		if upto, _ := strconv.ParseInt(a.Config.Setting("staged.upto", "0"), 10, 64); upto > 0 && a.Config.Setting("staged.other", "true") == "false" {
			keep := []stagedEntry{}
			for _, e := range j {
				if e.ID > upto {
					keep = append(keep, e)
				}
			}
			b, _ := json.Marshal(keep)
			a.Config.SetSetting(stagedJournalKey, string(b))
			// What went live is no longer "as it was", so there is no copy to go back to.
			if name := a.Config.Setting(stagedBaseKey, ""); name != "" {
				if p, err := a.backupPath(name); err == nil {
					os.Remove(p)
				}
				a.Config.SetSetting(stagedBaseKey, "")
			}
			a.Config.SetSetting("staged.upto", "0")
		}
		return
	}
	if !stagedPath(path) {
		return
	}
	text := stagedWords(method, path, body)
	if len(j) > 0 && j[len(j)-1].Text == text {
		return
	}
	var id int64 = 1
	if len(j) > 0 {
		id = j[len(j)-1].ID + 1
	}
	j = append(j, stagedEntry{ID: id, TS: time.Now().Unix(), Actor: actor, Text: text})
	if len(j) > 200 {
		j = j[len(j)-200:]
	}
	b, _ := json.Marshal(j)
	a.Config.SetSetting(stagedJournalKey, string(b))
}

// stagedWords describes a staged change in plain words, for the Review page.
func stagedWords(method, path string, body []byte) string {
	if w, ok := describePolicyChange(method, path, body); ok {
		return w[1]
	}
	verb := "Changed"
	if method == http.MethodDelete {
		verb = "Removed"
	}
	switch {
	case strings.HasPrefix(path, "/api/fw/zones"):
		return verb + " a network type or its ports."
	case strings.HasPrefix(path, "/api/fw/settings"):
		return verb + " firewall settings."
	case strings.HasPrefix(path, "/api/fw/"):
		return verb + " the firewall."
	case strings.HasPrefix(path, "/api/net/wans"), strings.HasPrefix(path, "/api/wan"):
		return verb + " an internet connection."
	case strings.HasPrefix(path, "/api/net/"):
		return verb + " network settings."
	case strings.HasPrefix(path, "/api/vpn/"):
		return verb + " VPN settings."
	case strings.HasPrefix(path, "/api/dhcp/"):
		return verb + " DHCP settings."
	case strings.HasPrefix(path, "/api/dns/"):
		return verb + " DNS settings."
	case strings.HasPrefix(path, "/api/routes"):
		return verb + " routes."
	}
	return verb + " settings."
}

func (a *App) stagedView() map[string]any {
	f, n := a.anyDirty()
	return map[string]any{
		"items": a.stagedJournal(), "fw": f, "net": n,
		"can_discard": a.Config.Setting(stagedBaseKey, "") != "",
	}
}

func (a *App) registerStagedRoutes() {
	s := a.API
	s.Handle("GET /api/staged", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.stagedView())
	})
	// Discard puts the settings back exactly as they were running, dropping everything staged.
	s.HandleAdmin("POST /api/staged/discard", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		stagedMu.Lock()
		defer stagedMu.Unlock()
		name := a.Config.Setting(stagedBaseKey, "")
		if name == "" {
			fail(w, errors.New("there is no earlier copy to go back to; change the items by hand"))
			return
		}
		if a.applyPending() {
			api.Error(w, http.StatusConflict, "a change is being tried out right now; wait for it to finish first")
			return
		}
		if st := update.ReadState(a.updateState()); st != "idle" {
			api.Error(w, http.StatusConflict, "an update is running ("+st+"); wait for it to finish")
			return
		}
		p, err := a.backupPath(name)
		if err != nil {
			fail(w, errors.New("the earlier copy is gone; change the items by hand"))
			return
		}
		if _, err := a.restoreBackup(p, "", c.User.Username); err != nil {
			fail(w, err)
			return
		}
		a.clearStaged()
		a.markDirty()
		a.fwSync()
		a.publishNet()
		a.auditEvent(c.User.Username, "info", "Staged changes discarded", "Discarded every change that was waiting to be applied.")
		ok(w)
	})
}

// everydayStepUp asks for the authenticator code when applying, then remembers it for fifteen
// minutes so a run of changes does not ask each time.
var stepUpSeen sync.Map // user id -> time of the last good code

func (a *App) everydayStepUp(uid int64, code string) error {
	if t, ok := stepUpSeen.Load(uid); ok && time.Since(t.(time.Time)) < stepUpMemory {
		return nil
	}
	if err := a.Auth.VerifyStepUp(uid, code); err != nil {
		return err
	}
	stepUpSeen.Store(uid, time.Now())
	return nil
}

// applyPending is true while a firewall or network change is being tried out (the 60-second window).
func (a *App) applyPending() bool {
	if a.FW != nil {
		if _, _, p := a.FW.Pending(); p {
			return true
		}
	}
	if a.Net != nil {
		if _, _, p := a.Net.NetPending(); p {
			return true
		}
	}
	return false
}

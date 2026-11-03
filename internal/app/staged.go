package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
//   - a copy of the settings from just before each staged change, so Discard puts everything back
//     exactly as it was running, or goes back to just before one line (dropping that line and the
//     ones saved after it, because later changes can depend on earlier ones).
// All of it is cleared the moment nothing is waiting any more (after a commit, or a discard).

const (
	stagedJournalKey = "staged.journal"
	stagedBaseKey    = "staged.base"
	stagedBaseKind   = "staged-base"
	stagedTipKey     = "staged.tip"
	stepUpMemory     = 15 * time.Minute
)

type stagedEntry struct {
	ID    int64  `json:"id"`
	TS    int64  `json:"ts"`
	Actor string `json:"actor"`
	Text  string `json:"text"`
	// Before names the copy of the settings taken just before this change was saved. Discarding this
	// line puts that copy back, which also drops every change saved after it.
	Before string `json:"before,omitempty"`
}

var stagedMu sync.Mutex

var reStagedSnap = regexp.MustCompile(`^snap-[0-9]+\.[a-z.]+$`)

func (a *App) stagedSnapPath(name string) string {
	if !reStagedSnap.MatchString(name) {
		return ""
	}
	return filepath.Join(a.backupDir(), "staged", name)
}

// takeStagedSnap saves a copy of the settings as they are right now and returns its name ("" if it
// could not be saved).
func (a *App) takeStagedSnap() string {
	name := "snap-" + strconv.FormatInt(time.Now().UnixNano(), 10) + bkExt
	if err := a.writeBackupFile(a.stagedSnapPath(name), stagedBaseKind, false); err != nil {
		return ""
	}
	return name
}

func (a *App) removeStagedSnap(name string) {
	if p := a.stagedSnapPath(name); p != "" {
		os.Remove(p)
	}
}

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
	if name := a.takeStagedSnap(); name != "" {
		a.Config.SetSetting(stagedBaseKey, name)
		a.Config.SetSetting(stagedTipKey, name)
	}
}

func (a *App) clearStaged() {
	for _, e := range a.stagedJournal() {
		a.removeStagedSnap(e.Before)
	}
	a.removeStagedSnap(a.Config.Setting(stagedBaseKey, ""))
	a.removeStagedSnap(a.Config.Setting(stagedTipKey, ""))
	a.Config.SetSetting(stagedBaseKey, "")
	a.Config.SetSetting(stagedTipKey, "")
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
				} else {
					a.removeStagedSnap(e.Before)
				}
			}
			b, _ := json.Marshal(keep)
			a.Config.SetSetting(stagedJournalKey, string(b))
			// What went live is no longer "as it was", so there is no single copy to go back to;
			// lines saved after it keep their own copies.
			a.removeStagedSnap(a.Config.Setting(stagedBaseKey, ""))
			a.Config.SetSetting(stagedBaseKey, "")
			a.Config.SetSetting("staged.upto", "0")
		}
		return
	}
	if !stagedPath(path) {
		return
	}
	text := stagedWords(method, path, body)
	tip := a.Config.Setting(stagedTipKey, "")
	if len(j) > 0 && j[len(j)-1].Text == text {
		// The same line again: whatever it changed belongs to that line, so the next line starts from now.
		if nt := a.takeStagedSnap(); nt != "" {
			a.removeStagedSnap(tip)
			a.Config.SetSetting(stagedTipKey, nt)
		}
		return
	}
	var id int64 = 1
	if len(j) > 0 {
		id = j[len(j)-1].ID + 1
	}
	j = append(j, stagedEntry{ID: id, TS: time.Now().Unix(), Actor: actor, Text: text, Before: tip})
	a.Config.SetSetting(stagedTipKey, a.takeStagedSnap())
	if len(j) > 60 { // each line holds a copy of the settings, so the list is kept short
		for _, e := range j[:len(j)-60] {
			a.removeStagedSnap(e.Before)
		}
		j = j[len(j)-60:]
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
	items := []map[string]any{}
	for _, e := range a.stagedJournal() {
		can := false
		if p := a.stagedSnapPath(e.Before); p != "" {
			if _, err := os.Stat(p); err == nil {
				can = true
			}
		}
		items = append(items, map[string]any{"id": e.ID, "ts": e.TS, "actor": e.Actor, "text": e.Text, "can_discard": can})
	}
	return map[string]any{
		"items": items, "fw": f, "net": n,
		"can_discard": a.Config.Setting(stagedBaseKey, "") != "",
	}
}

func (a *App) registerStagedRoutes() {
	s := a.API
	s.Handle("GET /api/staged", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.stagedView())
	})
	// Discard puts the settings back as they were running. With no "from" it drops everything that is
	// waiting; with "from" it goes back to just before that line, dropping it and the lines after it.
	s.HandleAdmin("POST /api/staged/discard", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			From int64 `json:"from"`
		}
		if r.ContentLength != 0 {
			if !api.Decode(w, r, &in) {
				return
			}
		}
		stagedMu.Lock()
		defer stagedMu.Unlock()
		if a.applyPending() {
			api.Error(w, http.StatusConflict, "a change is being tried out right now; wait for it to finish first")
			return
		}
		if st := update.ReadState(a.updateState()); st != "idle" {
			api.Error(w, http.StatusConflict, "an update is running ("+st+"); wait for it to finish")
			return
		}
		j := a.stagedJournal()
		snap, idx := a.Config.Setting(stagedBaseKey, ""), -1
		if in.From > 0 {
			for i, e := range j {
				if e.ID == in.From {
					idx = i
				}
			}
			if idx < 0 {
				fail(w, errors.New("that change is no longer waiting"))
				return
			}
			snap = j[idx].Before
		}
		p := a.stagedSnapPath(snap)
		if p == "" {
			fail(w, errors.New("there is no earlier copy to go back to; change the items by hand"))
			return
		}
		if _, err := os.Stat(p); err != nil {
			fail(w, errors.New("the earlier copy is gone; change the items by hand"))
			return
		}
		// Restoring puts the settings from that moment back, including these bookkeeping values, so read
		// them first and write them again afterwards.
		oldBase, oldTip := a.Config.Setting(stagedBaseKey, ""), a.Config.Setting(stagedTipKey, "")
		if _, err := a.restoreBackup(p, "", c.User.Username); err != nil {
			fail(w, err)
			return
		}
		what, detail := "Staged changes discarded", "Discarded every change that was waiting to be applied."
		if idx >= 0 {
			what, detail = "Staged change discarded", "Discarded a waiting change and the ones saved after it: "+j[idx].Text
			// Everything from that line on is gone. Its copy becomes the new starting point.
			for _, e := range j[idx+1:] {
				a.removeStagedSnap(e.Before)
			}
			a.removeStagedSnap(oldTip)
			a.Config.SetSetting(stagedBaseKey, oldBase)
			a.Config.SetSetting(stagedTipKey, snap)
			b, _ := json.Marshal(j[:idx])
			a.Config.SetSetting(stagedJournalKey, string(b))
		} else {
			for _, e := range j {
				a.removeStagedSnap(e.Before)
			}
			a.removeStagedSnap(oldBase)
			a.removeStagedSnap(oldTip)
			a.clearStaged()
		}
		a.markDirty()
		a.fwSync()
		a.publishNet()
		if idx >= 0 {
			if f, n := a.anyDirty(); !f && !n {
				a.clearStaged()
			}
		}
		a.auditEvent(c.User.Username, "info", what, detail)
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

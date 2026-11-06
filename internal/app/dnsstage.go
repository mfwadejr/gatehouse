package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"

	"nopewarden/internal/api"
)

// DNS changes can wait on the Review page like firewall and network changes. The console marks the
// requests it sends with a header; while such a change waits, the stored settings are changed but the
// running DNS keeps working from what it had. Applying (or the console doing it for you a moment after
// a single change) rebuilds DNS from the stored settings; discarding puts the old settings back.
//
// The "waiting" state lives only in memory: if NopeWarden restarts, DNS starts from the stored
// settings, so a waiting change simply goes live then.

const stageHeader = "X-Stage"

var dnsHold atomic.Bool

func (a *App) dnsHeld() bool { return dnsHold.Load() }

// dnsStagePath is a DNS change that can wait (not the buttons that act at once).
func dnsStagePath(method, path string) bool {
	if method == http.MethodGet || method == http.MethodHead {
		return false
	}
	if path == "/api/blocks" || strings.HasPrefix(path, "/api/blocks/") {
		return true // blocking by name reads its rules through a cache that is frozen while a change waits
	}
	if !strings.HasPrefix(path, "/api/dns/") {
		return false
	}
	for _, end := range []string{"/apply", "/preview", "/test", "/refresh", "/undo", "/cache/flush"} {
		if strings.HasSuffix(path, end) {
			return false
		}
	}
	// encrypted DNS has its own try-and-undo, so it stays immediate
	return !strings.HasPrefix(path, "/api/dns/encrypted")
}

func (a *App) dnsStageBefore(r *http.Request) {
	if r.Header.Get(stageHeader) != "" && dnsStagePath(r.Method, r.URL.Path) {
		if !dnsHold.Load() {
			a.cachedScopeRules() // keep what is in force now, so the change cannot leak before it is applied
		}
		dnsHold.Store(true)
	}
}

const forgetLaterKey = "devices.forget_later"

func (a *App) forgetPendingIDs() []int64 {
	var ids []int64
	json.Unmarshal([]byte(a.Config.Setting(forgetLaterKey, "")), &ids)
	return ids
}

// forgetLater puts "forget this device" on the Review page; the device is forgotten when the waiting
// changes are applied, and stays as it is if they are discarded.
func (a *App) forgetLater(id int64) {
	ids := a.forgetPendingIDs()
	for _, x := range ids {
		if x == id {
			return
		}
	}
	b, _ := json.Marshal(append(ids, id))
	a.Config.SetSetting(forgetLaterKey, string(b))
	dnsHold.Store(true)
}

func (a *App) forgetDevice(id int64) error {
	var mac string
	a.Config.QueryRow(`SELECT lower(mac) FROM devices WHERE id=?`, id).Scan(&mac)
	if err := a.Devices.Forget(id); err != nil {
		return err
	}
	a.Config.Exec(`DELETE FROM device_group_members WHERE lower(mac)=?`, mac)
	a.fwSync()
	return nil
}

// forgetPending forgets every device that was waiting to be forgotten.
func (a *App) forgetPending() {
	ids := a.forgetPendingIDs()
	if len(ids) == 0 {
		return
	}
	a.Config.SetSetting(forgetLaterKey, "")
	for _, id := range ids {
		a.forgetDevice(id)
	}
}

func (a *App) reloadLists() {
	a.Lists.Reload()
}

func (a *App) setLocalRecords() {
	if a.dnsHeld() {
		return
	}
	a.DNS.SetLocalRecords(a.localRecords())
}

func (a *App) reloadUpstreams() {
	if a.dnsHeld() {
		return
	}
	a.loadUpstreams()
}

func (a *App) applyDNSHeld() error {
	if a.dnsHeld() {
		return nil
	}
	return a.applyDNS()
}

// dnsApplyAll makes the running DNS match the stored settings.
func (a *App) dnsApplyAll() error {
	dnsHold.Store(false)
	a.dropScopeCache()
	a.forgetPending()
	var first error
	if err := a.Lists.ReloadNow(); err != nil {
		first = err
	}
	if err := a.applyDNS(); err != nil && first == nil {
		first = err
	}
	a.DNS.SetLocalRecords(a.localRecords())
	return first
}

func (a *App) registerDNSStageRoutes() {
	s := a.API
	a.forgetPending() // a restart makes waiting changes live, as it does for DNS
	s.HandleAdmin("POST /api/dns/apply", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if err := a.dnsApplyAll(); err != nil {
			fail(w, err)
			return
		}
		a.stagedDropDNS()
		api.JSON(w, 200, map[string]any{"status": "ok", "dirty": false})
	})
	s.HandleAdmin("POST /api/dns/preview", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"ok": true})
	})
}

// stagedDropDNS takes the DNS lines out of the waiting list once they are live.
func (a *App) stagedDropDNS() {
	stagedMu.Lock()
	defer stagedMu.Unlock()
	j := a.stagedJournal()
	keep := []stagedEntry{}
	for _, e := range j {
		if e.DNS || isDNSEntry(e.Text) {
			a.removeStagedSnap(e.Before)
			continue
		}
		keep = append(keep, e)
	}
	if len(keep) == len(j) {
		return
	}
	if f, n := a.anyDirty(); !f && !n {
		a.clearStaged()
		return
	}
	b, _ := json.Marshal(keep)
	a.Config.SetSetting(stagedJournalKey, string(b))
}

func isDNSEntry(text string) bool {
	return strings.HasSuffix(text, " DNS settings.") || strings.HasPrefix(text, "Forgot the device")
}

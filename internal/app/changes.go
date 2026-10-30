package app

import (
	"errors"
	"net/http"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/update"
)

// Undo. Before every apply of firewall or network changes, Gatehouse keeps a restore point: a
// complete, checked copy of the settings as they were. The Changes timeline lists them and each has
// a Revert button. Reverting puts the settings back as changes that are not live yet, so you review
// and apply them the usual way, with the usual 60-second way back.

const (
	restorePointKind = "before-apply"
	restorePointKeep = 20
)

// restorePoint saves the settings before a change goes live. A failure never stops the change,
// because the 60-second rollback still protects it; it is recorded so it is not silent.
func (a *App) restorePoint() {
	if _, err := a.saveBackup(restorePointKind); err != nil {
		a.event("system", "warning", "A restore point could not be saved", err.Error())
	}
}

type changeItem struct {
	Name    string `json:"name"`
	Created int64  `json:"created"`
	OK      bool   `json:"ok"` // passed its own checks, so Revert will work
	Summary string `json:"summary"`
}

func (a *App) changeList() []changeItem {
	out := []changeItem{}
	for _, b := range a.listBackups() {
		if kindLabel(b.Name) != "Before a change" {
			continue
		}
		it := changeItem{Name: b.Name, Created: b.Created, OK: b.Report != nil && b.Report.OK,
			Summary: "Settings as they were before the change made at " + time.Unix(b.Created, 0).Format("Mon 2 Jan, 15:04") + "."}
		out = append(out, it)
	}
	return out
}

func (a *App) registerChangeRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/changes", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"points": a.changeList(), "keep": restorePointKeep})
	})
	s.HandleAdmin("POST /api/changes/revert", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Name     string `json:"name"`
			Code     string `json:"code"`
			Password string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		p, err := a.backupPath(in.Name)
		if err != nil || kindLabel(in.Name) != "Before a change" {
			fail(w, errors.New("that restore point was not found"))
			return
		}
		if err := a.serious(c, in.Code, in.Password); err != nil {
			api.Error(w, http.StatusForbidden, err.Error())
			return
		}
		if st := update.ReadState(a.updateState()); st != "idle" {
			api.Error(w, http.StatusConflict, "an update is running ("+st+"); wait for it to finish")
			return
		}
		if _, err := a.restoreBackup(p, "", c.User.Username); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		a.fwSync()
		ok(w)
	})
}

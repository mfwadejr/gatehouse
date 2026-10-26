package app

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
	"gatehouse/internal/store"
	"gatehouse/internal/update"
)

// Reset to default. Two levels: network settings (networks, firewall, DHCP, DNS, VPN and Wi-Fi go
// back to a clean state, accounts and update settings stay) and factory (everything goes and the
// setup pages open again). A copy of the settings is always saved first, and offered for
// download, so a reset can be undone.

// backupDir is where copies of the settings are kept on the machine.
func (a *App) backupDir() string { return filepath.Join(a.Cfg.DataDir, "backups") }

func (a *App) registerResetRoutes() {
	// A fresh copy of the settings as a download, so it can be kept before a reset.
	a.API.HandleAdmin("GET /api/system/backup", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		name, err := a.saveBackup("manual")
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(name)+`"`)
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, name)
	})

	a.API.HandleAdmin("POST /api/system/reset", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Level   string `json:"level"`
			Code    string `json:"code"`
			Confirm string `json:"confirm"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Level != store.ResetNetwork && in.Level != store.ResetFactory {
			fail(w, errors.New("choose which reset you want"))
			return
		}
		if in.Level == store.ResetFactory && in.Confirm != "reset" {
			fail(w, errors.New("type the word reset to confirm a factory reset"))
			return
		}
		// The authenticator code is asked for only when the administrator set one up.
		if a.Config.Setting("fw.stepup", "1") == "1" && c.User.TOTPEnabled {
			if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
				api.Error(w, http.StatusForbidden, "enter a current code from your authenticator")
				return
			}
		}
		if st := update.ReadState(a.updateState()); st != "idle" {
			api.Error(w, http.StatusConflict, "an update is running ("+st+"); wait for it to finish")
			return
		}
		backup, err := a.saveBackup("before-reset")
		if err != nil {
			fail(w, err)
			return
		}
		if err := a.resetNow(in.Level, c.User.Username); err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"status": "reset", "level": in.Level, "backup": backup})
	})
}

// resetNow clears the settings and puts the empty configuration in place.
func (a *App) resetNow(level, actor string) error {
	if err := store.ResetConfig(a.Config, level); err != nil {
		return err
	}
	if level == store.ResetFactory {
		if err := store.ResetLogs(a.Logs); err != nil {
			return err
		}
		os.RemoveAll(filepath.Join(a.Cfg.DataDir, "update"))
	}
	// What a fresh install starts with: DNS servers and the default lists.
	if err := a.seedUpstreams(); err != nil {
		return err
	}
	if err := a.Lists.SeedDefaults(); err != nil {
		return err
	}
	a.Lists.Reload()
	a.DHCP.Reload()
	a.Devices.Rebuild()
	a.applyDNS()
	a.writeSetupState()
	title := "Network settings were reset to the defaults"
	if level == store.ResetFactory {
		title = "The machine was reset to factory settings"
	}
	a.event(actor, "warning", title, "A copy of the settings from before is kept in the backups folder.")
	// The empty network and the safe starting firewall go live a moment after this answer has
	// left, because they can move the very connection the answer travels on.
	go func() {
		time.Sleep(2 * time.Second)
		a.backToStart()
		a.publishNet()
		a.publishFW()
	}()
	return nil
}

// backToStart puts the machine in the state of a fresh install: no networks of its own, every
// wired port asking for an address, and the safe starting firewall with the console reachable.
func (a *App) backToStart() {
	if a.Net == nil || a.FW == nil {
		return
	}
	if err := a.FW.Check(fw.FirstBootBaselineFor(fw.SavedConsolePort())); err != nil {
		log.Printf("reset: starting firewall: %v", err)
	} else if tok, _, err := a.FW.Apply(fw.FirstBootBaselineFor(fw.SavedConsolePort()), confirmWindow, "reset"); err != nil {
		log.Printf("reset: starting firewall: %v", err)
	} else if err := a.FW.Confirm(tok); err != nil {
		log.Printf("reset: starting firewall: %v", err)
	}
	if err := a.Net.NetReset(); err != nil {
		log.Printf("reset: network: %v", err)
		return
	}
	a.Config.SetSetting("net.autoapply", "0")
}

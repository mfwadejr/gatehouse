package app

import (
	"errors"
	"net/http"

	"gatehouse/internal/api"
	"gatehouse/internal/update"
)

// Power: restart or shut down the machine, or restart only Gatehouse's own programs.

var powerActions = map[string]struct{ title, detail string }{
	"reboot":            {"The machine was restarted from the console", "The network is down for about a minute."},
	"poweroff":          {"The machine was shut down from the console", "It stays off until someone switches it on."},
	"restart-gatehouse": {"Gatehouse was restarted from the console", "The console, DNS and DHCP are down for a few seconds."},
}

func (a *App) registerPowerRoutes() {
	a.API.HandleAdmin("POST /api/system/power", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Action string `json:"action"`
			Code   string `json:"code"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		meta, known := powerActions[in.Action]
		if !known {
			fail(w, errors.New("choose restart, shut down or restart Gatehouse"))
			return
		}
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		if a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled {
			if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
				api.Error(w, http.StatusForbidden, "enter a current code from your authenticator")
				return
			}
		}
		if st := update.ReadState(a.updateState()); st != "idle" {
			api.Error(w, http.StatusConflict, "an update is running ("+st+"); wait for it to finish")
			return
		}
		a.event(c.User.Username, "warning", meta.title, meta.detail)
		if err := a.Net.Power(in.Action); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
}

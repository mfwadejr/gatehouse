package app

import (
	"net/http"

	"nopewarden/internal/api"
	"nopewarden/internal/fw"
)

// The "Finish setting up" list on the Overview. It is worked out from what the machine actually
// has, so it is right whatever order things were done in, and it needs no bookkeeping.

const (
	checklistHidden = "setup.checklist" // "hidden" once the person dismissed it
	setupModeKey    = "setup.mode"      // "router" or "behind", as chosen in the wizard
)

type checkItem struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
	Text string `json:"text"`
	Why  string `json:"why"`
	To   string `json:"to"` // the page that does it
}

// checklist lists what is left to set up for this machine.
func (a *App) checklist(username string) []checkItem {
	var items []checkItem
	behind := a.Config.Setting(setupModeKey, "") == "behind"

	if !behind {
		var wans int
		a.Config.QueryRow(`SELECT count(*) FROM wan_links WHERE enabled=1`).Scan(&wans)
		items = append(items, checkItem{ID: "internet", Done: wans > 0, To: "internet",
			Text: "Connect to the internet",
			Why:  "Choose which port your modem or provider plugs into. A Wi-Fi card can do this too, by joining a hotspot."})
	}

	lan := false
	zones, _ := fw.Zones(a.Config)
	for _, z := range zones {
		if z.Kind != fw.KindWan && z.Kind != fw.KindVpn && len(z.Ifaces) > 0 {
			lan = true
		}
	}
	dhcp := behind
	if !dhcp {
		var n int
		a.Config.QueryRow(`SELECT count(*) FROM dhcp_scopes WHERE enabled=1 AND mode<>'off'`).Scan(&n)
		dhcp = n > 0
	}
	items = append(items, checkItem{ID: "network", Done: lan && dhcp, To: "networks",
		Text: "Set up your network and its addresses",
		Why:  "A network is a port with an address range and DHCP, so your devices get addresses."})

	two := a.Auth.MFARequired()
	if !two && username != "" {
		var en int
		a.Config.QueryRow(`SELECT totp_enabled FROM users WHERE username=?`, username).Scan(&en)
		two = en == 1
	}
	items = append(items, checkItem{ID: "twostep", Done: two, To: "settings",
		Text: "Turn on two-factor sign-in",
		Why:  "Asks for a code from an authenticator app each time you sign in."})
	return items
}

func (a *App) registerChecklistRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/setup/checklist", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		items := a.checklist(c.User.Username)
		left := 0
		for _, i := range items {
			if !i.Done {
				left++
			}
		}
		api.JSON(w, 200, map[string]any{
			"items": items, "left": left,
			"hidden":         a.Config.Setting(checklistHidden, "") == "hidden",
			"pending_wizard": a.wizardPending(),
		})
	})
	s.HandleAdmin("PUT /api/setup/checklist", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Hidden bool `json:"hidden"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		v := ""
		if in.Hidden {
			v = "hidden"
		}
		a.Config.SetSetting(checklistHidden, v)
		ok(w)
	})
}

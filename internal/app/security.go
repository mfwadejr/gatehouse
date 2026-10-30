package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// The security check: a short list of the things that most often leave a router exposed, each
// checked against the real settings, each with a button that goes to where it is fixed. The verdict
// at the top is one plain sentence.

type secItem struct {
	ID    string  `json:"id"`
	Level string  `json:"level"` // bad, warn, ok
	Title string  `json:"title"`
	Text  string  `json:"text"`
	Fix   *whyFix `json:"fix,omitempty"`
}

type secReport struct {
	Verdict string    `json:"verdict"` // good, warn, bad
	Summary string    `json:"summary"`
	Items   []secItem `json:"items"`
	At      int64     `json:"at"`
}

func names(zs []string) string {
	switch len(zs) {
	case 0:
		return ""
	case 1:
		return zs[0]
	}
	return strings.Join(zs[:len(zs)-1], ", ") + " and " + zs[len(zs)-1]
}

func (a *App) securityCheck() secReport {
	var items []secItem
	add := func(it secItem) { items = append(items, it) }

	var two int
	a.Config.QueryRow(`SELECT count(*) FROM users WHERE totp_enabled=1`).Scan(&two)
	if two > 0 || a.Auth.MFARequired() {
		add(secItem{ID: "twostep", Level: "ok", Title: "Two-factor sign-in is on", Text: "Signing in needs a code from your authenticator app."})
	} else {
		add(secItem{ID: "twostep", Level: "bad", Title: "Anyone with your password can sign in",
			Text: "Two-factor sign-in asks for a code from an app on your phone as well, so a stolen password is not enough.",
			Fix:  &whyFix{Label: "Turn on two-factor", Kind: "goto", Page: "settings"}})
	}

	m, err := fw.Load(a.Config)
	if err == nil {
		var ssh, sshBad, mgmtBad, upnp, upnpBad []string
		for _, z := range m.Zones {
			if z.Kind == fw.KindWan || z.Kind == fw.KindVpn {
				continue
			}
			untrusted := z.Kind == fw.KindGuest || z.Kind == fw.KindIsolated || z.Kind == fw.KindCustom
			if z.SSH {
				ssh = append(ssh, z.Name)
				if untrusted {
					sshBad = append(sshBad, z.Name)
				}
			}
			if z.Management && untrusted {
				mgmtBad = append(mgmtBad, z.Name)
			}
			if z.UPnP {
				upnp = append(upnp, z.Name)
				if untrusted {
					upnpBad = append(upnpBad, z.Name)
				}
			}
		}
		switch {
		case len(mgmtBad) > 0:
			add(secItem{ID: "console", Level: "bad", Title: "Guests can reach this console",
				Text: names(mgmtBad) + " is allowed to open the Gatehouse console. Anyone on it can try to sign in.",
				Fix:  &whyFix{Label: "Open Networks", Kind: "goto", Page: "networks"}})
		default:
			add(secItem{ID: "console", Level: "ok", Title: "The console is only open to your own networks", Text: "Guest and isolated networks cannot reach it."})
		}
		switch {
		case len(sshBad) > 0:
			add(secItem{ID: "ssh", Level: "bad", Title: "Remote command-line access is open to the wrong network",
				Text: "SSH is allowed from " + names(sshBad) + ".", Fix: &whyFix{Label: "Open Networks", Kind: "goto", Page: "networks"}})
		case len(ssh) > 0:
			add(secItem{ID: "ssh", Level: "warn", Title: "SSH is on for " + names(ssh),
				Text: "Fine if you use it, but turn it off if you do not. It is a way in that needs a key or password to be strong.",
				Fix:  &whyFix{Label: "Open Networks", Kind: "goto", Page: "networks"}})
		default:
			add(secItem{ID: "ssh", Level: "ok", Title: "SSH is off", Text: "There is no command-line way in over the network."})
		}
		switch {
		case len(upnpBad) > 0:
			add(secItem{ID: "upnp", Level: "bad", Title: "Devices on a guest network can open ports to the internet",
				Text: names(upnpBad) + " allows UPnP, so any device there can make your router accept connections from outside.",
				Fix:  &whyFix{Label: "Open Networks", Kind: "goto", Page: "networks"}})
		case len(upnp) > 0:
			add(secItem{ID: "upnp", Level: "warn", Title: "UPnP is on for " + names(upnp),
				Text: "Games and apps there can open ports to the internet by themselves. Handy, but anything on that network can do it. You can limit it to chosen devices.",
				Fix:  &whyFix{Label: "Review UPnP", Kind: "goto", Page: "firewall"}})
		default:
			add(secItem{ID: "upnp", Level: "ok", Title: "UPnP is off", Text: "Nothing can open ports to the internet without you."})
		}
		var open []string
		for _, f := range m.Forwards {
			if f.Enabled && len(f.Src) == 0 && len(f.Countries) == 0 {
				open = append(open, fmt.Sprintf("%s (%s %s)", f.Name, f.Proto, f.ExtPort))
			}
		}
		if len(open) > 0 {
			add(secItem{ID: "forwards", Level: "warn", Title: "Open ports have no limit on who can use them",
				Text: names(open) + " can be reached from anywhere in the world. Limit them to your own country or to the addresses that need them.",
				Fix:  &whyFix{Label: "Open the Forwards list", Kind: "goto", Page: "firewall"}})
		} else {
			add(secItem{ID: "forwards", Level: "ok", Title: "No open ports are wide open", Text: "Every forwarded port is limited, or you have none."})
		}
		if !m.BlockDoT {
			add(secItem{ID: "dot", Level: "warn", Title: "Devices can bypass your DNS filtering",
				Text: "Encrypted DNS to other servers is not blocked, so a device can skip your filters.",
				Fix:  &whyFix{Label: "Open Protection", Kind: "goto", Page: "firewall"}})
		}
	}

	if u := a.updateStatus(); u.Available {
		add(secItem{ID: "updates", Level: "warn", Title: "Updates are waiting",
			Text: "There is a newer Gatehouse or there are system security updates to install.",
			Fix:  &whyFix{Label: "Open updates", Kind: "goto", Page: "settings"}})
	} else {
		add(secItem{ID: "updates", Level: "ok", Title: "Up to date", Text: "No updates are waiting."})
	}

	var newest int64
	for _, b := range a.listBackups() {
		if b.Created > newest {
			newest = b.Created
		}
	}
	if newest == 0 || time.Since(time.Unix(newest, 0)) > 30*24*time.Hour {
		add(secItem{ID: "backup", Level: "warn", Title: "There is no recent backup",
			Text: "A backup lets you get everything back after a failed disk. The last one is more than a month old, or there is none.",
			Fix:  &whyFix{Label: "Open Backups", Kind: "goto", Page: "settings"}})
	} else {
		add(secItem{ID: "backup", Level: "ok", Title: "A recent backup exists", Text: "Made " + time.Since(time.Unix(newest, 0)).Round(time.Hour).String() + " ago."})
	}

	if cfg := a.alertConfig(); !cfg.Email.Enabled && !cfg.Ntfy.Enabled {
		add(secItem{ID: "alerts", Level: "warn", Title: "Nothing will tell you if something goes wrong",
			Text: "Set up an email or a push message so you hear about outages and updates.",
			Fix:  &whyFix{Label: "Set up alerts", Kind: "goto", Page: "settings"}})
	} else {
		add(secItem{ID: "alerts", Level: "ok", Title: "Alerts are set up", Text: "Gatehouse can reach you."})
	}

	bad, warn := 0, 0
	for _, it := range items {
		switch it.Level {
		case "bad":
			bad++
		case "warn":
			warn++
		}
	}
	r := secReport{Items: items, At: time.Now().Unix()}
	switch {
	case bad > 0:
		r.Verdict, r.Summary = "bad", fmt.Sprintf("%d thing%s should be fixed now.", bad, plural(bad))
	case warn > 0:
		r.Verdict, r.Summary = "warn", fmt.Sprintf("Safe enough, with %d thing%s worth tightening.", warn, plural(warn))
	default:
		r.Verdict, r.Summary = "good", "Everything checked looks good."
	}
	return r
}

func (a *App) registerSecurityRoutes() {
	a.API.HandleAdmin("GET /api/security/check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.securityCheck())
	})
}

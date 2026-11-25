package app

import (
	"net/http"
	"strconv"
	"strings"

	"nopewarden/internal/api"
	dnssrv "nopewarden/internal/dns"
)

// Allow this site / Block this site, from a row in Logs. The dialog asks this first, so it can say
// what blocks the site now, whether a block someone wrote on purpose covers this device (which an
// exception from Logs never overrides), and whether the lists involved are about threats.

var twoPartEnds = map[string]bool{"co": true, "com": true, "org": true, "net": true, "gov": true, "edu": true, "ac": true}

// wholeSite is the site a name belongs to: the name with any leading labels such as www or music
// taken off. It is a guess that is right for ordinary names and for the common co.uk style.
func wholeSite(name string) string {
	l := strings.Split(strings.ToLower(strings.Trim(name, ".")), ".")
	n := len(l)
	if n <= 2 {
		return strings.Join(l, ".")
	}
	keep := 2
	if len(l[n-1]) == 2 && twoPartEnds[l[n-2]] {
		keep = 3
	}
	if keep > n {
		keep = n
	}
	return strings.Join(l[n-keep:], ".")
}

type siteHold struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
	Kind string `json:"kind"` // block, custom
}

// threatList says whether a block list is one of the threat lists (malware, phishing, scams).
func (a *App) threatList(name string) bool {
	var url string
	a.Config.QueryRow(`SELECT url FROM blocklists WHERE name=?`, name).Scan(&url)
	for _, c := range categoryCatalog {
		if (c.Key == "malware" || c.Key == "scams") && c.URL == url && url != "" {
			return true
		}
	}
	l := strings.ToLower(name)
	return strings.Contains(l, "malware") || strings.Contains(l, "phish") || strings.Contains(l, "scam") || strings.Contains(l, "threat")
}

func (a *App) registerSiteCheckRoutes() {
	a.API.Handle("GET /api/logs/site", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		name := dnssrv.NormalizeSite(r.URL.Query().Get("name"))
		if name == "" {
			fail(w, errBadID)
			return
		}
		client := strings.TrimSpace(r.URL.Query().Get("client"))
		var ci dnssrv.ClientInfo
		if client != "" && a.Devices != nil {
			ci = a.Devices.Client(client)
		} else {
			ci = dnssrv.ClientInfo{Filtering: true}
		}
		zone := ""
		if client != "" {
			zone = a.clientZone(client)
		}
		ex := a.Lists.Explain(name, ci.GroupID)
		lists := ex.Lists
		if lists == nil {
			lists = []string{}
		}
		threat := false
		for _, l := range lists {
			threat = threat || a.threatList(l)
		}
		var group string
		if ci.GroupID != 0 {
			a.Config.QueryRow(`SELECT name FROM groups WHERE id=?`, ci.GroupID).Scan(&group)
		}
		holds := []siteHold{}
		allowed := []siteHold{}
		rules, _ := a.loadScopeRules()
		sets := map[int64]bool{}
		for _, ru := range rules {
			if ru.Kind != "name" || !(name == ru.Value || strings.HasSuffix(name, "."+ru.Value)) || !ru.Enabled {
				continue
			}
			if ru.Scope != "everyone" && !ru.covers(ci, func() string { return zone }) {
				continue
			}
			var set int64
			a.Config.QueryRow(`SELECT set_id FROM scope_rules WHERE id=?`, ru.ID).Scan(&set)
			if set == 0 {
				set = ru.ID
			}
			if sets[set] {
				continue
			}
			sets[set] = true
			if ru.Allow {
				allowed = append(allowed, siteHold{ID: set, Text: scopeWords(ru, a), Kind: "block"})
			} else {
				holds = append(holds, siteHold{ID: set, Text: scopeWords(ru, a), Kind: "block"})
			}
		}
		if ex.Reason == "custom-block" {
			holds = append(holds, siteHold{Text: "Block " + ex.Rule + " for everyone, on the Your names list", Kind: "custom"})
		}
		api.JSON(w, 200, map[string]any{
			"name": name, "site": wholeSite(name), "lists": lists, "threat": threat,
			"holds": holds, "allowed": allowed,
			"who": map[string]any{"device": ci.Name, "mac": strings.ToLower(ci.MAC), "network": zone, "profile_id": strconv.FormatInt(ci.GroupID, 10), "profile": group},
		})
	})
}

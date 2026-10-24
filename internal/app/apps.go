package app

import (
	"net/http"
	"sort"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/appid"
)

// Application names. Gatehouse names the app behind a connection from the website name the
// device asked for through Gatehouse's DNS. It never looks inside traffic. The owner can turn it
// off, because it shows which apps each person uses.

func (a *App) appsOn() bool { return a.Config.Setting("apps.enabled", "1") == "1" }

type appRow struct {
	App      string `json:"app"`
	Category string `json:"category"`
	Bytes    int64  `json:"bytes"`
	N        int    `json:"n"`
}

// topApps groups the estimated data per site (see topDomains) by application. Sites that are
// not in the catalogue are left out rather than guessed.
func (a *App) topApps(rng string) map[string]any {
	if !a.appsOn() {
		return map[string]any{"enabled": false, "apps": []appRow{}}
	}
	all := a.topDomainsAll(rng)
	by := map[string]*appRow{}
	for _, d := range all {
		ap, ok := appid.Lookup(d.Domain)
		if !ok {
			continue
		}
		r := by[ap.Name]
		if r == nil {
			r = &appRow{App: ap.Name, Category: ap.Category}
			by[ap.Name] = r
		}
		r.Bytes += d.Bytes
		r.N++
	}
	out := []appRow{}
	for _, r := range by {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Bytes > out[j].Bytes || out[i].Bytes == out[j].Bytes && out[i].App < out[j].App
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return map[string]any{"enabled": true, "approximate": true, "apps": out}
}

func (a *App) registerAppRoutes() {
	s := a.API
	s.Handle("GET /api/apps", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"enabled": a.appsOn(), "names": appid.Names()})
	})
	s.HandleAdmin("PUT /api/apps/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		v := "0"
		if in.Enabled {
			v = "1"
		}
		a.Config.SetSetting("apps.enabled", v)
		ok(w)
	})
	s.Handle("GET /api/traffic/apps", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.topApps(r.URL.Query().Get("range")))
	})
}

var _ = strings.TrimSpace

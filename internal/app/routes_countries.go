package app

import (
	"errors"
	"net"
	"strings"

	"encoding/json"
	"net/http"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// homeCountry guesses which country this network is in, from the public address of the internet
// connection and, failing that, from the address the console was opened from.
func (a *App) homeCountry(remote string) string {
	if ws, err := a.listWans(a.Config); err == nil {
		for _, w := range ws {
			if !w.Enabled {
				continue
			}
			for _, ad := range ifaceAddrs(w.Iface) {
				ip, _, err := net.ParseCIDR(ad)
				if err == nil {
					if cc := a.country(ip.String()); cc != "" {
						return cc
					}
				}
			}
		}
	}
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}
	return a.country(host)
}

func (a *App) registerCountryRoutes() {
	s := a.API
	s.Handle("GET /api/fw/countries", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.geoEnsure()
		p := fw.CountryPolicyFrom(a.Config)
		_, have := a.geoAge()
		api.JSON(w, 200, map[string]any{"policy": p, "home": a.homeCountry(r.RemoteAddr), "ready": have, "max": fw.MaxCountries})
	})
	s.HandleAdmin("PUT /api/fw/countries", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			fw.CountryPolicy
			Confirm bool `json:"confirm"` // the person has seen the warning about their own country
		}
		if !api.Decode(w, r, &in) {
			return
		}
		p := in.CountryPolicy.Clean()
		if len(p.Countries) > fw.MaxCountries {
			fail(w, errors.New("Choose up to 40 countries. For more, use Allow only, and list the ones you want."))
			return
		}
		home := a.homeCountry(r.RemoteAddr)
		if p.Active() && home != "" && !in.Confirm {
			inList := false
			for _, cc := range p.Countries {
				if cc == home {
					inList = true
				}
			}
			switch {
			case p.Mode == "allow" && !inList:
				fail(w, errors.New("needs-confirm: Allow only these countries leaves out "+home+", where this network seems to be. Your own country would be blocked."))
				return
			case p.Mode == "block" && inList:
				fail(w, errors.New("needs-confirm: This blocks "+home+", where this network seems to be."))
				return
			}
		}
		old := a.Config.Setting("fw.countries", "")
		b, _ := json.Marshal(p)
		a.Config.SetSetting("fw.countries", string(b))
		// Check the result compiles before it is kept.
		m, err := fw.Load(a.Config)
		if err == nil {
			err = m.Validate()
		}
		if err == nil {
			_, err = fw.Compile(m)
		}
		if err != nil {
			a.Config.SetSetting("fw.countries", old)
			fail(w, err)
			return
		}
		if p.Active() {
			a.geoEnsure()
		}
		a.event(c.User.Username, "info", "Changed the country rules", strings.ToLower(p.Mode)+", "+p.Dir)
		a.markDirty()
		ok(w)
	})
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
	"gatehouse/internal/mdns"
)

// Devices finding each other across networks. The person picks pairs of networks; the reflector
// copies announcements between them and the firewall lets those announcements reach the router.

var discoveryKinds = []struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Types []string `json:"types"`
}{
	{"printers", "Printers and scanners", []string{"_ipp._tcp", "_ipps._tcp", "_printer._tcp", "_pdl-datastream._tcp", "_scanner._tcp", "_uscan._tcp"}},
	{"apple", "AirPlay, Apple TV and speakers", []string{"_airplay._tcp", "_raop._tcp", "_companion-link._tcp", "_airport._tcp", "_homekit._tcp"}},
	{"cast", "Chromecast and Google TV", []string{"_googlecast._tcp", "_googlezone._tcp"}},
	{"home", "Home Assistant and HomeKit devices", []string{"_hap._tcp", "_home-assistant._tcp", "_matter._tcp", "_matterc._udp"}},
	{"music", "Spotify Connect and Sonos", []string{"_spotify-connect._tcp", "_sonos._tcp"}},
}

type mdnsStored struct {
	Pairs []fw.MDNSPair `json:"pairs"`
	Kinds []string      `json:"kinds"` // empty means everything
}

func (a *App) mdnsStored() mdnsStored {
	var s mdnsStored
	if raw := a.setting("fw.mdns_ui", ""); raw != "" {
		json.Unmarshal([]byte(raw), &s)
	}
	if s.Pairs == nil {
		s.Pairs = []fw.MDNSPair{}
	}
	if s.Kinds == nil {
		s.Kinds = []string{}
	}
	return s
}

func servicesFor(kinds []string) []string {
	var out []string
	for _, k := range kinds {
		for _, d := range discoveryKinds {
			if d.ID == k {
				out = append(out, d.Types...)
			}
		}
	}
	return out
}

// mdnsApply makes the running reflector match what is saved.
func (a *App) mdnsApply() {
	m, err := fw.Load(a.Config)
	if err != nil {
		return
	}
	st := a.mdnsStored()
	byName := map[string][]string{}
	for _, z := range m.Zones {
		if z.Kind != fw.KindWan {
			byName[strings.ToLower(z.Name)] = z.Ifaces
		}
	}
	var pairs [][2][]string
	for _, p := range st.Pairs {
		x, y := byName[strings.ToLower(p.A)], byName[strings.ToLower(p.B)]
		if len(x) > 0 && len(y) > 0 {
			pairs = append(pairs, [2][]string{x, y})
		}
	}
	a.mdns.SetConfig(mdns.Config{Links: mdns.Plan(pairs), Services: servicesFor(st.Kinds)})
}

func (a *App) mdnsLoop(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	a.mdnsApply()
	for {
		select {
		case <-ctx.Done():
			a.mdns.Stop()
			return
		case <-t.C:
			a.mdnsApply()
		}
	}
}

func (a *App) registerDiscoveryRoutes() {
	s := a.API
	s.Handle("GET /api/fw/discovery", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		zones, _ := fw.Zones(a.Config)
		names := []string{}
		for _, z := range zones {
			if z.Kind != fw.KindWan {
				names = append(names, z.Name)
			}
		}
		st := a.mdnsStored()
		api.JSON(w, 200, map[string]any{"pairs": st.Pairs, "kinds": st.Kinds, "networks": names, "catalog": discoveryKinds})
	})
	s.HandleAdmin("PUT /api/fw/discovery", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in mdnsStored
		if !api.Decode(w, r, &in) {
			return
		}
		zones, _ := fw.Zones(a.Config)
		valid := map[string]string{}
		for _, z := range zones {
			if z.Kind != fw.KindWan {
				valid[strings.ToLower(z.Name)] = z.Name
			}
		}
		out := mdnsStored{Pairs: []fw.MDNSPair{}, Kinds: []string{}}
		seen := map[string]bool{}
		for _, p := range in.Pairs {
			x, okx := valid[strings.ToLower(p.A)]
			y, oky := valid[strings.ToLower(p.B)]
			if !okx || !oky {
				fail(w, errors.New("choose two of your networks"))
				return
			}
			if x == y {
				fail(w, errors.New("a network does not need to be paired with itself"))
				return
			}
			k := x + "|" + y
			if x > y {
				k = y + "|" + x
			}
			if seen[k] {
				continue
			}
			seen[k] = true
			out.Pairs = append(out.Pairs, fw.MDNSPair{A: x, B: y})
		}
		known := map[string]bool{}
		for _, d := range discoveryKinds {
			known[d.ID] = true
		}
		for _, k := range in.Kinds {
			if !known[k] {
				fail(w, errors.New("unknown kind of device"))
				return
			}
			out.Kinds = append(out.Kinds, k)
		}
		raw, _ := json.Marshal(out)
		a.Config.SetSetting("fw.mdns_ui", string(raw))
		// The firewall reads the pairs from fw.mdns; the services list is the reflector's own.
		pb, _ := json.Marshal(fw.MDNSPolicy{Pairs: out.Pairs})
		a.Config.SetSetting("fw.mdns", string(pb))
		if _, err := fw.Load(a.Config); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		a.mdnsApply()
		ok(w)
	})
}

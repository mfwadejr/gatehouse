package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
)

// geocodeBase is the place-search service used by "Find" in Your place on the map. It is a variable
// so tests can point it at a local server. Only the text the person typed is sent, and only when
// they press Find; nothing is stored there by this machine.
var geocodeBase = "https://nominatim.openstreetmap.org/search"

var geoMu sync.Mutex
var geoLast time.Time

type geoHit struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

func lookupPlace(q string) ([]geoHit, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 || len(q) > 200 {
		return nil, fmt.Errorf("type a zip or postal code, a city, or an address")
	}
	// The service asks for no more than one search a second.
	geoMu.Lock()
	if d := time.Until(geoLast.Add(time.Second)); d > 0 {
		time.Sleep(d)
	}
	geoLast = time.Now()
	geoMu.Unlock()
	req, _ := http.NewRequest("GET", geocodeBase+"?format=jsonv2&limit=5&q="+url.QueryEscape(q), nil)
	req.Header.Set("User-Agent", "NopeWarden-firewall/1.0 (place lookup)")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the place search. Check the internet connection, or type the latitude and longitude instead")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("the place search answered with an error (%d). Try again later, or type the latitude and longitude", resp.StatusCode)
	}
	var raw []struct {
		Display string `json:"display_name"`
		Lat     string `json:"lat"`
		Lon     string `json:"lon"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("the place search gave an answer that could not be read")
	}
	out := []geoHit{}
	for _, r := range raw {
		la, e1 := strconv.ParseFloat(r.Lat, 64)
		lo, e2 := strconv.ParseFloat(r.Lon, 64)
		if e1 != nil || e2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
			continue
		}
		out = append(out, geoHit{Name: r.Display, Lat: la, Lon: lo})
	}
	return out, nil
}

func (a *App) registerGeocode() {
	a.API.HandleAdmin("POST /api/overview/find", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct{ Q string }
		if !api.Decode(w, r, &in) {
			return
		}
		hits, err := lookupPlace(in.Q)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"results": hits})
	})
}

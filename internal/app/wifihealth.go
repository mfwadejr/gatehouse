package app

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
)

// A card that broadcasts can hang while its interface still says "access point". This watches the
// cards that should be broadcasting, asks the helper to recover one that has stopped serving, and
// says so in the event log, so a stall is never silent.

type wifiWatch struct {
	mu     sync.Mutex
	states map[string]netd.WifiState
	bad    map[string]bool // cards last reported as not serving
}

func (a *App) wifiKeepAwake() bool { return a.Config.Setting("wifi.keep_awake", "1") == "1" }

// broadcastingCards lists the cards set up to broadcast and switched on.
func (a *App) broadcastingCards() []string {
	var out []string
	rows, err := a.Config.Query(`SELECT iface FROM wifi_radios WHERE mode='ap' AND enabled=1`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

func (a *App) wifiState(iface string) (netd.WifiState, bool) {
	a.wifiw.mu.Lock()
	defer a.wifiw.mu.Unlock()
	s, ok := a.wifiw.states[iface]
	return s, ok
}

// wifiCheckOnce makes one round of checks and reports changes in the event log.
func (a *App) wifiCheckOnce() {
	cards := a.broadcastingCards()
	if len(cards) == 0 || a.Net == nil {
		return
	}
	res, err := a.Net.WifiHealth(cards, a.wifiKeepAwake())
	if err != nil {
		return
	}
	a.wifiw.mu.Lock()
	if a.wifiw.states == nil {
		a.wifiw.states = map[string]netd.WifiState{}
		a.wifiw.bad = map[string]bool{}
	}
	for k := range a.wifiw.states {
		found := false
		for _, c := range cards {
			found = found || c == k
		}
		if !found {
			delete(a.wifiw.states, k)
			delete(a.wifiw.bad, k)
		}
	}
	var evs [][3]string
	for _, st := range res {
		a.wifiw.states[st.Iface] = st
		switch {
		case st.Action != "" && !st.GaveUp:
			a.wifiw.bad[st.Iface] = true
			evs = append(evs, [3]string{"warning", "Wi-Fi on " + st.Iface + " stopped working; Gatehouse " + st.Action, "Reason: " + st.Reason + "."})
		case st.GaveUp && !a.wifiw.bad[st.Iface+"!"]:
			a.wifiw.bad[st.Iface+"!"] = true
			evs = append(evs, [3]string{"error", "Wi-Fi on " + st.Iface + " keeps failing", "Gatehouse " + st.Action + ". Reason: " + st.Reason + ". Open the Wi-Fi page and use Wi-Fi report."})
		case st.OK && a.wifiw.bad[st.Iface]:
			a.wifiw.bad[st.Iface] = false
			a.wifiw.bad[st.Iface+"!"] = false
			evs = append(evs, [3]string{"info", "Wi-Fi on " + st.Iface + " is working again", ""})
		}
	}
	a.wifiw.mu.Unlock()
	for _, e := range evs {
		a.event("wifi", e[0], e[1], e[2])
	}
	a.publishNet()
}

func (a *App) wifiLoop(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.wifiCheckOnce()
		}
	}
}

func (a *App) registerWifiHealthRoutes() {
	s := a.API
	s.HandleAdmin("PUT /api/net/wifi/keepawake", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			On bool `json:"on"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		a.Config.SetSetting("wifi.keep_awake", map[bool]string{true: "1", false: "0"}[in.On])
		if a.Net != nil {
			for _, c := range a.wifiCards() {
				if err := a.Net.WifiPowerSave(c.Iface, in.On); err != nil {
					fail(w, err)
					return
				}
			}
		}
		a.publishNet()
		ok(w)
	})
	s.HandleAdmin("GET /api/net/wifi/{iface}/report", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errNoHelper)
			return
		}
		out, err := a.Net.WifiReport(r.PathValue("iface"))
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]string{"report": strings.TrimSpace(out)})
	})
}

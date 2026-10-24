package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/dhcp"
)

type rogueState struct {
	mu     sync.Mutex
	alerts map[int64]rogueAlert
	kick   chan struct{}
}

// Another DHCP server on a network where Gatehouse serves addresses makes devices pick an address at
// random from the two. The check asks the network for offers the way a new device would, takes no
// address, and reports who answered.
type rogueFinding struct {
	Server string `json:"server"`
	Offer  string `json:"offer"`
}

// probeScope asks one network for DHCP offers the way a new device would. Our own answers are not a
// problem: the router address and any address this device has on the port.
func (a *App) probeScope(id int64) (verdict string, out []rogueFinding, name string, err error) {
	var iface, mode, router string
	if err := a.Config.QueryRow(`SELECT name,iface,mode,router FROM dhcp_scopes WHERE id=?`, id).Scan(&name, &iface, &mode, &router); err != nil {
		return "", nil, "", errors.New("that network does not exist")
	}
	var ours []net.IP
	if ip := net.ParseIP(strings.TrimSpace(router)); ip != nil {
		ours = append(ours, ip)
	}
	if ifi, err := net.InterfaceByName(iface); err == nil {
		addrs, _ := ifi.Addrs()
		for _, ad := range addrs {
			if n, ok := ad.(*net.IPNet); ok {
				ours = append(ours, n.IP)
			}
		}
	}
	ctx, cancel := contextTimeout(6 * time.Second)
	defer cancel()
	found, err := dhcp.Probe(ctx, iface, ours, 4*time.Second)
	if err != nil {
		return "", nil, name, err
	}
	out = []rogueFinding{}
	for _, f := range found {
		out = append(out, rogueFinding{f.Server.String(), f.Offer.String()})
	}
	verdict = "ok"
	switch {
	case len(out) > 0 && mode == "serve":
		verdict = "conflict" // someone else hands out addresses where Gatehouse does
	case len(out) == 0 && mode == "off":
		verdict = "none" // Gatehouse expects another device to serve here, and nothing answered
	}
	return verdict, out, name, nil
}

// rogueAlert is what the console shows when another DHCP server answers on a network Gatehouse serves.
type rogueAlert struct {
	Scope int64          `json:"scope"`
	Name  string         `json:"name"`
	Found []rogueFinding `json:"found"`
	Since int64          `json:"since"`
}

func (a *App) rogueAlerts() []rogueAlert {
	a.rogue.mu.Lock()
	defer a.rogue.mu.Unlock()
	out := []rogueAlert{}
	for _, v := range a.rogue.alerts {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// rogueCheck looks at every network Gatehouse serves and keeps the alert list current. A new
// problem is also written to the activity log once.
func (a *App) rogueCheck() {
	rows, err := a.Config.Query(`SELECT id FROM dhcp_scopes WHERE enabled=1 AND mode='serve'`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	seen := map[int64]bool{}
	for _, id := range ids {
		verdict, found, name, err := a.probeScope(id)
		if err != nil {
			continue
		}
		seen[id] = true
		a.rogue.mu.Lock()
		old, had := a.rogue.alerts[id]
		if verdict == "conflict" {
			al := rogueAlert{Scope: id, Name: name, Found: found, Since: time.Now().Unix()}
			if had {
				al.Since = old.Since
			}
			a.rogue.alerts[id] = al
		} else {
			delete(a.rogue.alerts, id)
		}
		a.rogue.mu.Unlock()
		if verdict == "conflict" && !had {
			var who []string
			for _, f := range found {
				who = append(who, f.Server)
			}
			a.event("dhcp", "warn", "Another DHCP server is answering on "+name,
				strings.Join(who, ", ")+" hands out addresses here too, so devices may get the wrong settings. Switch it off, or turn off DHCP on this network in Gatehouse.")
		}
	}
	a.rogue.mu.Lock()
	for id := range a.rogue.alerts {
		if !seen[id] {
			delete(a.rogue.alerts, id)
		}
	}
	a.rogue.mu.Unlock()
	a.Bus.Publish("dhcp.alerts", map[string]int{"n": len(a.rogueAlerts())})
}

func (a *App) rogueLoop(ctx context.Context) {
	t := time.NewTimer(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.rogueCheck()
			t.Reset(20 * time.Minute)
		case <-a.rogue.kick:
			time.Sleep(5 * time.Second) // let the new network come up first
			a.rogueCheck()
			t.Reset(20 * time.Minute)
		}
	}
}

func (a *App) registerRogueRoutes() {
	a.API.HandleAdmin("POST /api/dhcp/scopes/{id}/probe", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		verdict, found, _, err := a.probeScope(id)
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				api.Error(w, http.StatusNotFound, err.Error())
				return
			}
			fail(w, err)
			return
		}
		if found == nil {
			found = []rogueFinding{}
		}
		api.JSON(w, 200, map[string]any{"verdict": verdict, "found": found})
	})
	a.API.Handle("GET /api/dhcp/alerts", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.rogueAlerts())
	})
}

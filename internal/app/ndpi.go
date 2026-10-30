package app

import (
	"context"
	"log"
	"net/http"
	"sort"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/ndpi"
	"gatehouse/internal/netd"
)

// Traffic labels (nDPI). A separate small program watches copies of the packets on the inside ports
// and tells Gatehouse what each connection is ("YouTube", "Zoom", "BitTorrent"), including traffic
// that never asked Gatehouse's DNS. It is off by default because it uses some processor time, and
// because it looks at the first packets of every connection, which is the most private thing
// Gatehouse can do. It never changes or blocks traffic, and a failure only means the name-based
// labels carry on alone.

const (
	ndpiSetting = "ndpi.enabled"
	ndpiSocket  = netd.NDPISocketDir + "/ndpi.sock"
	ndpiGroup   = "gatehouse-ndpi"
)

func (a *App) ndpiOn() bool { return a.Config.Setting(ndpiSetting, "0") == "1" }

// ndpiTable is the live table; it is created once so lookups never need a nil check.
func (a *App) ndpiTable() *ndpi.Table {
	a.ndpiOnce.Do(func() { a.NDPI = ndpi.NewTable() })
	return a.NDPI
}

// ndpiLabel is the label for one logged connection, when the program is on and has seen it.
func (a *App) ndpiLabel(proto, src string, sp int, dst string, dp int) (name, cat string, ok bool) {
	if !a.ndpiOn() {
		return "", "", false
	}
	f, found := a.ndpiTable().Lookup(proto, src, sp, dst, dp)
	if !found {
		return "", "", false
	}
	name, ok = ndpi.Label(f.App)
	if !ok {
		return "", "", false
	}
	return name, f.Cat, true
}

// ndpiIfaces are the inside ports to watch: every network port, never an internet port.
func (a *App) ndpiIfaces() []string {
	wan := map[string]bool{}
	if rows, err := a.Config.Query(`SELECT iface FROM wan_links`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			wan[n] = true
		}
		rows.Close()
	}
	seen := map[string]bool{}
	var out []string
	if rows, err := a.Config.Query(`SELECT name FROM interfaces WHERE zone_id IS NOT NULL AND ipv4_cidr<>''`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			if n != "" && !wan[n] && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
		rows.Close()
	}
	sort.Strings(out)
	return out
}

// ndpiSync makes the program match the setting. It is safe to call at any time.
func (a *App) ndpiSync() error {
	on := a.ndpiOn()
	return a.Net.NDPIApply(netd.NDPIPlan{Enabled: on, Ifaces: a.ndpiIfaces()})
}

// ndpiLoop connects the table to the program's socket and re-applies the setting after a start.
func (a *App) ndpiLoop(ctx context.Context) {
	if a.Net == nil {
		return
	}
	go func() {
		// Give the helper a moment on boot, then make sure the program matches the setting.
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Second):
		}
		if a.ndpiOn() {
			if err := a.ndpiSync(); err != nil {
				log.Printf("ndpi: %v", err)
			}
		}
	}()
	for ctx.Err() == nil {
		if err := a.ndpiTable().Serve(ctx, ndpiSocket, ndpiGroup); err != nil && ctx.Err() == nil {
			// The socket folder only exists on a system built with the program; look again later.
			select {
			case <-ctx.Done():
			case <-time.After(time.Minute):
			}
		}
	}
}

func (a *App) ndpiStatus() map[string]any {
	t := a.ndpiTable()
	installed := a.Net != nil && a.Net.NDPIInstalled()
	warn := []map[string]any{}
	for i, f := range t.Warnings() {
		if i >= 20 {
			break
		}
		warn = append(warn, map[string]any{"src": f.Src, "dst": f.Dst, "dp": f.DP, "proto": f.Proto, "app": f.App, "risk": f.Risk, "seen": f.Seen})
	}
	return map[string]any{
		"installed": installed, "enabled": a.ndpiOn(), "connected": t.Connected.Load() > 0,
		"seen": t.Seen.Load(), "warnings": warn, "ifaces": a.ndpiIfaces(),
	}
}

func (a *App) registerNDPIRoutes() {
	s := a.API
	s.Handle("GET /api/ndpi", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.ndpiStatus())
	})
	s.HandleAdmin("PUT /api/ndpi", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Enabled && (a.Net == nil || !a.Net.NDPIInstalled()) {
			api.Error(w, 409, "This system does not have the traffic-labelling program. It comes with a fresh install image built with it.")
			return
		}
		v := "0"
		if in.Enabled {
			v = "1"
		}
		a.Config.SetSetting(ndpiSetting, v)
		if err := a.ndpiSync(); err != nil {
			a.Config.SetSetting(ndpiSetting, "0")
			a.Net.NDPIApply(netd.NDPIPlan{})
			api.Error(w, 500, err.Error())
			return
		}
		ok(w)
	})
}

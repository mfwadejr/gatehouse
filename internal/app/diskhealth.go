package app

import (
	"net/http"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/netd"
)

// Disk health from what each drive reports. The reading is kept for a few minutes because asking a
// drive is slow; the charts get the hottest drive and the most worn one from the same reading.

func (a *App) diskReport(maxAge time.Duration) netd.DiskReport {
	if a.Net == nil {
		return netd.DiskReport{}
	}
	return a.cached("disk-health", maxAge, func() any { return a.Net.DiskHealth() }).(netd.DiskReport)
}

func (a *App) diskHistPoints() []histPoint {
	var pts []histPoint
	temp, wear := 0, -1
	for _, d := range a.diskReport(30 * time.Minute).Drives {
		if d.Temp > temp {
			temp = d.Temp
		}
		if d.Wear > wear {
			wear = d.Wear
		}
	}
	if temp > 0 {
		pts = append(pts, histPoint{"disk.temp", float64(temp)})
	}
	if wear >= 0 {
		pts = append(pts, histPoint{"disk.wear", float64(wear)})
	}
	return pts
}

func (a *App) registerDiskRoutes() {
	a.API.Handle("GET /api/disk/health", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.diskReport(5*time.Minute))
	})
	// Installs the small program that reads drive health, then looks again.
	a.API.HandleAdmin("POST /api/disk/tool", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if err := a.Net.DiskToolInstall(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Installed the SMART tool", "smartmontools, to read drive health.")
		api.JSON(w, 200, a.diskReport(0))
	})
}

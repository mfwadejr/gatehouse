package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/devices"
	dnssrv "gatehouse/internal/dns"
	"gatehouse/internal/fw"
)

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func fail(w http.ResponseWriter, err error) {
	api.Error(w, http.StatusBadRequest, err.Error())
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func ok(w http.ResponseWriter) { api.JSON(w, 200, map[string]string{"status": "ok"}) }

func (a *App) registerRoutes() {
	s := a.API
	a.registerVPNRoutes()
	a.registerPortRoutes()
	a.registerProtectRoutes()
	a.registerUpdateRoutes()
	a.registerPowerRoutes()
	a.registerResetRoutes()
	a.registerBackupRoutes()
	a.registerSSHRoutes()
	a.registerWifiHealthRoutes()
	s.Handle("GET /api/logs", a.logSearch)
	s.HandleAdmin("DELETE /api/logs", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.QLog.Clear()
		a.Logs.Exec(`DELETE FROM fw_events`)
		a.Logs.Exec(`DELETE FROM events`)
		ok(w)
	})
	s.HandleAdmin("PUT /api/logs/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Allowed bool `json:"record_allowed"`
			Blocked bool `json:"record_blocked"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		// If nothing else is waiting to be applied, switch the change on straight away.
		_, before, err := a.currentRuleset()
		clean := err == nil && a.FW != nil && before == a.lastApplied()
		a.Config.SetSetting("fw.log_allowed", strconv.Itoa(b2i(in.Allowed)))
		a.Config.SetSetting("fw.log_drops", strconv.Itoa(b2i(in.Blocked)))
		if clean {
			if err := a.applyFirewall(c.User.Username); err != nil {
				fail(w, err)
				return
			}
		}
		a.markDirty()
		api.JSON(w, 200, map[string]any{"status": "ok", "applied": clean})
	})
	// DNS
	s.Handle("GET /api/dns/summary", a.dnsSummary)
	s.Handle("GET /api/dns/log", a.dnsLog)
	s.Handle("GET /api/dns/top", a.dnsTop)
	s.Handle("GET /api/dns/lists", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		l, err := a.Lists.Lists()
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, l)
	})
	s.HandleAdmin("POST /api/dns/lists", a.dnsAddList)
	s.HandleAdmin("POST /api/dns/lists/{id}/refresh", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if err := a.Lists.Refresh(ctx, id); err != nil {
			fail(w, err)
			return
		}
		a.Lists.Reload()
		ok(w)
	})
	s.HandleAdmin("PATCH /api/dns/lists/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !good || !api.Decode(w, r, &in) {
			return
		}
		if err := a.Lists.SetEnabled(id, in.Enabled); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("DELETE /api/dns/lists/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := a.Lists.Delete(id); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.Handle("GET /api/dns/rules", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		l, err := a.Lists.Customs()
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, l)
	})
	s.HandleAdmin("POST /api/dns/rules", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct{ Domain, Action, Note string }
		if !api.Decode(w, r, &in) {
			return
		}
		if err := a.Lists.AddCustom(in.Domain, in.Action, in.Note); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("DELETE /api/dns/rules/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := a.Lists.DeleteCustom(id); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.Handle("GET /api/dns/explain", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		g, _ := strconv.ParseInt(r.URL.Query().Get("group"), 10, 64)
		d := strings.TrimSpace(r.URL.Query().Get("domain"))
		if d == "" {
			fail(w, errBadID)
			return
		}
		api.JSON(w, 200, a.Lists.Explain(d, g))
	})
	s.Handle("GET /api/dns/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		g, err := a.Lists.Groups()
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, g)
	})
	s.HandleAdmin("PUT /api/dns/groups", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var g dnssrv.Group
		if !api.Decode(w, r, &g) {
			return
		}
		id, err := a.Lists.SaveGroup(g)
		if err != nil {
			fail(w, err)
			return
		}
		a.Devices.Rebuild()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/dns/groups/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := a.Lists.DeleteGroup(id); err != nil {
			fail(w, err)
			return
		}
		a.Devices.Rebuild()
		ok(w)
	})
	s.Handle("GET /api/dns/records", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rows, err := a.Config.Query(`SELECT id,name,type,value,ttl FROM dns_records ORDER BY name`)
		if err != nil {
			fail(w, err)
			return
		}
		defer rows.Close()
		type rec struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
			TTL   int    `json:"ttl"`
			// Problem is set when the resolver cannot use the record (saved by an older version).
			Problem string `json:"problem,omitempty"`
		}
		out := []rec{}
		for rows.Next() {
			var x rec
			rows.Scan(&x.ID, &x.Name, &x.Type, &x.Value, &x.TTL)
			if !dnssrv.UsableRecord(dnssrv.LocalRecord{Name: x.Name, Type: x.Type, Value: x.Value, TTL: uint32(x.TTL)}) {
				x.Problem = "Not working: this is not a valid " + x.Type + " value. Remove it and add it again."
			}
			out = append(out, x)
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("POST /api/dns/records", a.dnsAddRecord)
	s.HandleAdmin("DELETE /api/dns/records/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM dns_records WHERE id=?`, id)
		a.DNS.SetLocalRecords(a.localRecords())
		ok(w)
	})
	s.Handle("GET /api/dns/upstreams", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.DNS.Pool().Status())
	})
	s.HandleAdmin("POST /api/dns/upstreams", a.dnsAddUpstream)
	s.Handle("GET /api/dns/upstream-mode", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"mode": a.setting("dns.upstream_order", "fastest")})
	})
	s.HandleAdmin("PUT /api/dns/upstream-mode", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Mode string `json:"mode"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Mode != "fastest" && in.Mode != "ordered" {
			fail(w, errors.New("choose fastest first, or in my order"))
			return
		}
		a.Config.SetSetting("dns.upstream_order", in.Mode)
		a.DNS.Pool().SetOrdered(in.Mode == "ordered")
		ok(w)
	})
	s.HandleAdmin("PUT /api/dns/upstreams/order", a.dnsOrderUpstreams)
	s.HandleAdmin("POST /api/dns/upstreams/{id}/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		u := a.DNS.Pool().Get(id)
		if u == nil {
			fail(w, errors.New("that server is not in use (it may be switched off)"))
			return
		}
		api.JSON(w, 200, a.DNS.Pool().Test(r.Context(), u))
	})
	s.HandleAdmin("DELETE /api/dns/upstreams/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		var n int
		a.Config.QueryRow(`SELECT count(*) FROM dns_upstreams WHERE enabled=1 AND id<>?`, id).Scan(&n)
		if n == 0 {
			fail(w, errLastUpstream)
			return
		}
		a.Config.Exec(`DELETE FROM dns_upstreams WHERE id=?`, id)
		a.loadUpstreams()
		ok(w)
	})
	s.Handle("GET /api/dns/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		o := a.dnsOptions()
		api.JSON(w, 200, map[string]any{
			"block_mode": o.BlockMode, "local_domain": o.LocalDomain, "rate_limit": o.RateLimit, "rate_burst": o.RateBurst,
			"cache_size": o.CacheSize, "canary": o.Canary,
			"log_retention_days": a.settingFloat("dns.log_retention_days", 30), "anonymize": a.setting("dns.anonymize", "0") == "1",
		})
	})
	s.HandleAdmin("PUT /api/dns/settings", a.dnsSaveSettings)
	s.HandleAdmin("POST /api/dns/cache/flush", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.DNS.Cache().Flush()
		ok(w)
	})
	s.HandleAdmin("DELETE /api/dns/log", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.QLog.Clear()
		ok(w)
	})

	// Devices
	s.HandleAdmin("POST /api/devices", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			MAC  string `json:"mac"`
			Name string `json:"name"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		id, err := a.Devices.Create(in.MAC, in.Name)
		if err != nil {
			fail(w, err)
			return
		}
		a.fwSync()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("PATCH /api/devices/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		var u devices.Update
		if !good || !api.Decode(w, r, &u) {
			return
		}
		if err := a.Devices.Update(id, u); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	s.HandleAdmin("DELETE /api/devices/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		var mac string
		a.Config.QueryRow(`SELECT lower(mac) FROM devices WHERE id=?`, id).Scan(&mac)
		if err := fw.CheckDeviceRemovable(a.Config, mac); err != nil {
			fail(w, err)
			return
		}
		if err := a.Devices.Forget(id); err != nil {
			fail(w, err)
			return
		}
		a.Config.Exec(`DELETE FROM device_group_members WHERE lower(mac)=?`, mac)
		a.fwSync()
		ok(w)
	})

	a.registerDHCPRoutes()
	a.registerFWRoutes()
	a.registerFWObjectRoutes()
	a.registerNetRoutes()
	a.registerGroupRoutes()
	a.registerWifiRoutes()
	a.registerRogueRoutes()
	a.registerAddNetRoutes()
	a.registerTrafficRoutes()
	a.registerOverviewRoutes()
	a.registerSpeedRoutes()
	a.registerDevTrafficRoutes()
	a.registerLogExtraRoutes()
	a.registerAppRoutes()
	a.registerDeviceViewRoutes()
}

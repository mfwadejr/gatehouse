package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

const confirmWindow = 60 * time.Second

// pendingApply remembers which connection started an apply so the confirmation must come
// from a different one. Established connections survive a bad ruleset; new ones do not.
type applyState struct {
	mu      sync.Mutex
	connID  uint64
	version int64
}

func (a *App) registerFWRoutes() {
	s := a.API
	s.Handle("GET /api/fw/overview", a.fwOverview)
	// A small answer for a Save: what the firewall and the network would run now, and whether
	// either is waiting to be applied. The console asks before and after a Save to tell the
	// person what the Save really did.
	s.Handle("GET /api/applystate", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"fw": a.fwStatus(), "net": a.netStatus()})
	})
	s.Handle("GET /api/fw/presets", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{"presets": fw.Presets(), "services": fw.ServiceNames()})
	})
	s.Handle("GET /api/fw/zones", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		z, err := fw.Zones(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, z)
	})
	s.HandleAdmin("PUT /api/fw/zones", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var z fw.ZoneRow
		if !api.Decode(w, r, &z) {
			return
		}
		var id int64
		err := fw.Mutate(a.Config, func(tx *sql.Tx) (e error) { id, e = fw.SaveZone(tx, z); return })
		if err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/fw/zones/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		var imp *removalImpact
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error {
			var e error
			if imp, e = zoneImpact(tx, id); e != nil {
				return e
			}
			deleteScopes(tx, imp.Scopes)
			return fw.DeleteZone(tx, id)
		}); err != nil {
			fail(w, err)
			return
		}
		if len(imp.Scopes) > 0 {
			a.DHCP.Reload()
			a.publishLeases()
		}
		a.markDirty()
		api.JSON(w, 200, imp)
	})
	s.Handle("GET /api/fw/rules", a.fwRulesView)
	s.HandleAdmin("PUT /api/fw/rules", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in fw.RuleRow
		if !api.Decode(w, r, &in) {
			return
		}
		var id int64
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) (e error) { id, e = fw.SaveRule(tx, in); return }); err != nil {
			fail(w, err)
			return
		}
		a.fwSync()
		a.markDirty()
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/fw/rules/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.DeleteRule(tx, id) }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		ok(w)
	})
	s.HandleAdmin("POST /api/fw/rules/order", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Order []int64 `json:"order"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.MoveRule(tx, in.Order) }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		ok(w)
	})
	s.Handle("GET /api/fw/forwards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		l, err := fw.Forwards(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, l)
	})
	s.HandleAdmin("PUT /api/fw/forwards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in fw.ForwardRow
		if !api.Decode(w, r, &in) {
			return
		}
		var id int64
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) (e error) { id, e = fw.SaveForward(tx, in); return }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		a.refreshVPN() // a port clash with a VPN shows up at once
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/fw/forwards/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := fw.Mutate(a.Config, func(tx *sql.Tx) error { return fw.DeleteForward(tx, id) }); err != nil {
			fail(w, err)
			return
		}
		a.markDirty()
		a.refreshVPN()
		ok(w)
	})
	s.HandleAdmin("PUT /api/fw/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			DNSRedirect bool   `json:"dns_redirect"`
			DNSTarget   string `json:"dns_target"`
			BlockDoT    bool   `json:"block_dot"`
			BlockDoH    *bool  `json:"block_doh"`
			LogDrops    bool   `json:"log_drops"`
			StepUp      bool   `json:"step_up"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.DNSTarget = strings.TrimSpace(in.DNSTarget)
		if ip := net.ParseIP(in.DNSTarget); in.DNSTarget != "" && (ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast()) {
			fail(w, errors.New("Enter the DNS server as an IPv4 address such as 192.168.1.2, or leave it empty to use Gatehouse."))
			return
		}
		a.Config.SetSetting("fw.dns_target", in.DNSTarget)
		b := func(v bool) string { return strconv.Itoa(b2i(v)) }
		for k, v := range map[string]string{"fw.dns_redirect": b(in.DNSRedirect), "fw.block_dot": b(in.BlockDoT), "fw.log_drops": b(in.LogDrops), "fw.stepup": b(in.StepUp)} {
			a.Config.SetSetting(k, v)
		}
		if in.BlockDoH != nil {
			a.Config.SetSetting("fw.block_doh", b(*in.BlockDoH))
		}
		a.markDirty()
		ok(w)
	})
	s.HandleAdmin("POST /api/fw/preview", a.fwPreview)
	s.HandleAdmin("POST /api/fw/apply", a.fwApply)
	s.HandleAdmin("POST /api/fw/confirm", a.fwConfirm)
	s.HandleAdmin("POST /api/fw/rollback", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.FW == nil {
			fail(w, errNoHelper)
			return
		}
		if err := a.FW.Rollback(); err != nil {
			fail(w, err)
			return
		}
		a.Config.Exec(`UPDATE fw_revisions SET status='rolled_back' WHERE status='pending'`)
		a.fwSync()
		a.publishFW()
		ok(w)
	})
	s.Handle("GET /api/fw/history", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rows, err := a.Config.Query(`SELECT version,created_at,author,summary,status FROM fw_revisions ORDER BY version DESC LIMIT 100`)
		if err != nil {
			fail(w, err)
			return
		}
		defer rows.Close()
		type rev struct {
			Version int64  `json:"version"`
			At      int64  `json:"at"`
			Author  string `json:"author"`
			Summary string `json:"summary"`
			Status  string `json:"status"`
		}
		out := []rev{}
		for rows.Next() {
			var x rev
			rows.Scan(&x.Version, &x.At, &x.Author, &x.Summary, &x.Status)
			out = append(out, x)
		}
		api.JSON(w, 200, out)
	})
}

var errNoHelper = errors.New("the network helper is not available")

func (a *App) markDirty() { a.publishFW() }

// currentRuleset compiles the stored configuration.
func (a *App) currentRuleset() (*fw.Model, string, error) {
	m, err := fw.Load(a.Config)
	if err != nil {
		return nil, "", err
	}
	s, err := fw.Compile(m)
	return m, s, err
}

// lastAppliedRuleset is the most recent confirmed ruleset text.
func (a *App) lastApplied() string {
	var s string
	a.Config.QueryRow(`SELECT ruleset FROM fw_revisions WHERE status='confirmed' ORDER BY version DESC LIMIT 1`).Scan(&s)
	return s
}

func (a *App) fwStatus() map[string]any {
	st := map[string]any{"helper": a.FW != nil}
	_, script, err := a.currentRuleset()
	if err != nil {
		st["error"] = err.Error()
	}
	st["dirty"] = err == nil && script != a.lastApplied()
	st["rev"] = fmt.Sprintf("%x", fnvSum([]byte(script)))
	if a.FW != nil {
		if tok, dl, ok := a.FW.Pending(); ok {
			st["pending"] = map[string]any{"token": tok, "deadline": dl.Unix()}
		}
	}
	return st
}

func (a *App) publishFW() { a.Bus.Publish("fw.status", a.fwStatus()) }

func (a *App) fwOverview(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	m, err := fw.Load(a.Config)
	if err != nil {
		fail(w, err)
		return
	}
	out := map[string]any{"status": a.fwStatus()}
	if err := m.Validate(); err != nil {
		out["problems"] = strings.Split(err.Error(), "; ")
	} else {
		out["summary"] = fw.Describe(m)
	}
	zones, _ := fw.Zones(a.Config)
	out["zones"] = zones
	// What the network types alone allow for each pair, so the matrix and the server agree.
	pairs := map[string]bool{}
	for _, x := range m.Zones {
		for _, y := range m.Zones {
			pairs[x.Name+"|"+y.Name] = fw.PairOpen(x, y)
		}
	}
	out["pair_open"] = pairs
	out["matrix_zones"] = func() []map[string]any {
		var zs []map[string]any
		for _, z := range m.Zones {
			zs = append(zs, map[string]any{"name": z.Name, "kind": z.Kind})
		}
		return zs
	}()
	out["settings"] = map[string]any{
		"dns_redirect": m.DNSRedirect, "block_dot": m.BlockDoT, "block_doh": m.BlockDoH, "log_drops": m.LogDrops,
		"dns_target": m.DNSTarget,
		"step_up":    a.Config.Setting("fw.stepup", "0") == "1",
	}
	api.JSON(w, 200, out)
}

// ifaceOf finds the local interface on the same network as the client.
func ifaceOf(clientIP string) string {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return ""
	}
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		addrs, _ := ifi.Addrs()
		for _, ad := range addrs {
			if n, ok := ad.(*net.IPNet); ok && n.Contains(ip) {
				return ifi.Name
			}
		}
	}
	return ""
}

// sessionSafe reports whether the administrator's own connection keeps management access.
// checked is false when the client's interface cannot be determined or is the local loopback.
func sessionSafe(m *fw.Model, clientIP string) (checked, safe bool, iface string) {
	iface = ifaceOf(clientIP)
	if m.BridgeOn() {
		// In bridge mode the console is reached through the bridge; the bridge setup checks its own safety.
		return false, true, iface
	}
	if iface == "" || iface == "lo" {
		return false, true, iface
	}
	// The port the device was set up from keeps working whatever else changes.
	if iface == m.RescueIface && !contains(m.WanIfaces(), iface) {
		return true, true, iface
	}
	for _, z := range m.Zones {
		if z.Management {
			for _, i := range z.Ifaces {
				if i == iface {
					if len(m.MgmtAllow) > 0 && !inRanges(m.MgmtAllow, clientIP) {
						return true, false, iface
					}
					return true, true, iface
				}
			}
		}
	}
	return true, false, iface
}

// fwPreview says what would change and whether it is safe, without touching anything.
func (a *App) fwPreview(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	m, script, err := a.currentRuleset()
	if err != nil {
		api.JSON(w, 200, map[string]any{"ok": false, "problems": strings.Split(err.Error(), "; ")})
		return
	}
	res := map[string]any{"ok": true, "summary": fw.Describe(m), "changed": script != a.lastApplied()}
	if a.FW != nil {
		if err := a.FW.Check(a.fullScript(m, script)); err != nil {
			api.JSON(w, 200, map[string]any{"ok": false, "problems": []string{err.Error()}})
			return
		}
	}
	// Simulation: the administrator's own connection must still be able to manage the device.
	if checked, safe, ifc := sessionSafe(m, c.IP); checked {
		res["you_stay_connected"] = safe
		if !safe {
			res["ok"] = false
			res["problems"] = []string{"You are connected through " + ifc + ", which would no longer be allowed to manage this device (the network is not marked for management, or your address is outside the limit on who may manage). Fix that first, or connect from a network that is allowed."}
		}
	}
	api.JSON(w, 200, res)
}

func (a *App) fwApply(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	if a.FW == nil {
		fail(w, errNoHelper)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	if _, _, pending := a.FW.Pending(); pending {
		api.Error(w, http.StatusConflict, fw.ErrPending.Error())
		return
	}
	if a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled {
		if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
			api.Error(w, http.StatusForbidden, "enter a current code from your authenticator to apply firewall changes")
			return
		}
	}
	m, script, err := a.currentRuleset()
	if err != nil {
		fail(w, err)
		return
	}
	// Refuse changes that would cut off this very session.
	if checked, safe, _ := sessionSafe(m, c.IP); checked && !safe {
		fail(w, errors.New("this change would disconnect you; it was not applied"))
		return
	}
	if a.geoMissing(m) {
		fail(w, errors.New("the country lists are still being downloaded; try again in a minute"))
		a.geoMaybeRefreshIfNeeded(context.Background())
		return
	}
	a.restorePoint()
	tok, deadline, err := a.FW.Apply(a.fullScript(m, script), confirmWindow, c.User.Username)
	if err != nil {
		if errors.Is(err, fw.ErrPending) {
			api.Error(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, err)
		return
	}
	summary := strings.Join(fw.Describe(m), " ")
	if len(summary) > 500 {
		summary = summary[:500]
	}
	res, _ := a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status) VALUES(?,?,?,?,'pending')`, time.Now().Unix(), c.User.Username, summary, script)
	ver, _ := res.LastInsertId()
	a.apply.mu.Lock()
	a.apply.connID, a.apply.version = api.ConnID(r), ver
	a.apply.mu.Unlock()
	a.publishFW()
	// Close this connection so the confirmation has to prove a fresh one still works.
	w.Header().Set("Connection", "close")
	api.JSON(w, 200, map[string]any{"token": tok, "deadline": deadline.Unix(), "seconds": int(confirmWindow / time.Second)})
}

func (a *App) fwConfirm(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	if a.FW == nil {
		fail(w, errNoHelper)
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	a.apply.mu.Lock()
	same := a.apply.connID != 0 && a.apply.connID == api.ConnID(r)
	ver := a.apply.version
	a.apply.mu.Unlock()
	if same {
		api.Error(w, http.StatusConflict, "confirm from a new connection; the old one can outlive a broken change")
		return
	}
	if err := a.FW.Confirm(in.Token); err != nil {
		fail(w, err)
		return
	}
	a.Config.Exec(`UPDATE fw_revisions SET status='confirmed' WHERE version=?`, ver)
	a.Config.Exec(`UPDATE fw_revisions SET status='superseded' WHERE status='confirmed' AND version<?`, ver)
	a.fwSync()
	a.publishFW()
	ok(w)
}

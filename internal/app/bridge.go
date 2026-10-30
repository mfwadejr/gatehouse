package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/bridgemode"
	"gatehouse/internal/fw"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
)

// Bridge mode. Gatehouse sits between another router and the local network and filters instead of
// routing. The networks and internet connections already saved are NOT touched: bridge mode swaps
// them out when the plan and ruleset are built, so switching it off puts everything back exactly.
//
// A change is staged as a draft. Applying it loads the network files and the firewall together,
// each with its own independent 60-second timer; unless the change is confirmed from a new
// connection, both go back by themselves and the draft stays for another try.

const (
	bridgeDraftSetting    = "net.bridge.draft"
	bridgeApplyingSetting = "net.bridge.applying"
)

// portsLister lists the wired ports; tests replace it.
var portsLister = ports

var bridgeState struct {
	mu      sync.Mutex
	connID  uint64
	version int64
	print   string
}

func decodeBridge(raw string) *bridgemode.Config {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var c bridgemode.Config
	if json.Unmarshal([]byte(raw), &c) != nil {
		return nil
	}
	return &c
}

// bridgeLive is the setup that is confirmed and running (or nil).
func (a *App) bridgeLive() *bridgemode.Config { return fw.BridgeFrom(a.Config) }

// bridgeDraft is the staged setup; the live one when nothing is staged.
func (a *App) bridgeDraft() *bridgemode.Config {
	if d := decodeBridge(a.Config.Setting(bridgeDraftSetting, "")); d != nil {
		return d
	}
	return a.bridgeLive()
}

// applyBridgeToPlan swaps the saved networks and connections for the bridge. Wi-Fi networks, VPNs
// and port forwards are not part of a bridge: they stay saved and come back when it is switched off.
func applyBridgeToPlan(p *netd.Plan, b *bridgemode.Config) {
	if b == nil || !b.Enabled {
		return
	}
	p.Config = netcfg.Config{}
	for _, i := range b.Interfaces() {
		p.Config.Interfaces = append(p.Config.Interfaces, netcfg.Interface{Name: i.Name, Kind: i.Kind, Members: i.Members,
			IPv4: i.IPv4, DHCP: i.DHCP, Gateway: i.Gateway, DNS: i.DNS, Enabled: i.Enabled})
	}
	p.Links, p.Groups = nil, nil
}

// bridgePorts lists this machine's wired ports and what they are used for today.
func (a *App) bridgePorts() []bridgemode.PortInfo {
	use := map[string]string{}
	if ifs, err := a.listIfaces(a.Config); err == nil {
		for _, i := range ifs {
			if !i.Enabled {
				continue
			}
			for _, m := range append([]string{i.Name}, i.Members...) {
				use[m] = "network"
			}
		}
	}
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			if w.Enabled {
				use[w.Iface] = "internet"
			}
		}
	}
	var out []bridgemode.PortInfo
	for _, p := range portsLister() {
		n, _ := p["name"].(string)
		wifi, _ := p["wifi"].(bool)
		out = append(out, bridgemode.PortInfo{Name: n, Wireless: wifi, UsedFor: use[n]})
	}
	return out
}

// currentPorts are the physical ports the administrator's own connection comes in through.
func (a *App) currentPorts(clientIP string) []string {
	n := ifaceOf(clientIP)
	if n == "" || n == "lo" {
		return nil
	}
	if ifs, err := a.listIfaces(a.Config); err == nil {
		for _, i := range ifs {
			if i.Name == n && len(i.Members) > 0 {
				return i.Members
			}
		}
	}
	return []string{n}
}

func bridgeSummary(b *bridgemode.Config) string {
	if b == nil || !b.Enabled {
		return "Bridge mode switched off: Gatehouse routes again."
	}
	return "Bridge mode: " + b.Outside + " faces the other router, " + strings.Join(b.Inside, ", ") +
		" face the local network; the console is reachable from the inside only."
}

// bridgeCheck validates a staged setup in words, and returns cautions that do not block it.
func (a *App) bridgeCheck(b *bridgemode.Config, clientIP string) (warnings []string, err error) {
	if err = b.Validate(); err != nil {
		return nil, err
	}
	if !b.Enabled {
		return nil, nil
	}
	warnings, err = b.CheckPorts(a.bridgePorts(), a.currentPorts(clientIP))
	if err != nil {
		return nil, err
	}
	if len(b.Allow) == 0 {
		warnings = append(warnings, "Nothing from the outside can start a connection to your devices. Add an exception only for what you host.")
	}
	return warnings, nil
}

func (a *App) bridgeStatus() map[string]any {
	st := map[string]any{"helper": a.Net != nil && a.FW != nil}
	live, draft := a.bridgeLive(), a.bridgeDraft()
	st["live"] = live != nil && live.Enabled
	if a.Net != nil && a.FW != nil {
		_, ndl, nok := a.Net.NetPending()
		_, fdl, fok := a.FW.Pending()
		if nok || fok {
			dl := ndl
			if fok && (!nok || fdl.After(ndl)) {
				dl = fdl
			}
			st["pending"] = map[string]any{"deadline": dl.Unix()}
		} else if a.Config.Setting(bridgeApplyingSetting, "") == "1" {
			// The timers ran out: both the network files and the firewall went back by themselves.
			a.Config.SetSetting(bridgeApplyingSetting, "")
			a.Config.Exec(`UPDATE fw_revisions SET status='rolled_back' WHERE status='pending'`)
			a.event("system", "warning", "A bridge mode change was not confirmed, so it was undone", "Your previous settings are back. The change you made is still saved so you can try again.")
			st["undone"] = true
		}
	}
	st["config"] = draft
	st["dirty"] = !sameBridge(live, draft)
	st["summary"] = bridgeSummary(live)
	return st
}

func sameBridge(x, y *bridgemode.Config) bool {
	if x == nil {
		x = &bridgemode.Config{}
	}
	if y == nil {
		y = &bridgemode.Config{}
	}
	bx, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(bx) == string(by)
}

func (a *App) registerBridgeRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/bridge", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		st := a.bridgeStatus()
		st["ports"] = a.bridgePorts()
		st["current"] = a.currentPorts(c.IP)
		if d := a.bridgeDraft(); d != nil {
			warn, err := a.bridgeCheck(d, c.IP)
			if err != nil {
				st["problems"] = strings.Split(err.Error(), "; ")
			}
			st["warnings"] = warn
		}
		api.JSON(w, 200, st)
	})
	// Save a staged setup. Nothing changes on the machine until it is applied.
	s.HandleAdmin("PUT /api/bridge", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in bridgemode.Config
		if !api.Decode(w, r, &in) {
			return
		}
		warn, err := a.bridgeCheck(&in, c.IP)
		if err != nil {
			fail(w, err)
			return
		}
		b, _ := json.Marshal(in)
		a.Config.SetSetting(bridgeDraftSetting, string(b))
		api.JSON(w, 200, map[string]any{"status": "ok", "warnings": warn})
	})
	s.HandleAdmin("POST /api/bridge/discard", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.Config.SetSetting(bridgeDraftSetting, "")
		ok(w)
	})
	s.HandleAdmin("POST /api/bridge/apply", a.bridgeApply)
	s.HandleAdmin("POST /api/bridge/confirm", a.bridgeConfirm)
	s.HandleAdmin("POST /api/bridge/cancel", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil || a.FW == nil {
			fail(w, errNoHelper)
			return
		}
		e1 := a.Net.NetRollback()
		e2 := a.FW.Rollback()
		a.Config.SetSetting(bridgeApplyingSetting, "")
		a.Config.Exec(`UPDATE fw_revisions SET status='rolled_back' WHERE status='pending'`)
		a.publishFW()
		a.publishNet()
		if e1 != nil || e2 != nil {
			fail(w, errors.New("the settings were put back, but part of it reported a problem: "+firstErr(e1, e2).Error()))
			return
		}
		ok(w)
	})
}

func firstErr(es ...error) error {
	for _, e := range es {
		if e != nil {
			return e
		}
	}
	return nil
}

func (a *App) bridgeApply(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	if a.Net == nil || a.FW == nil {
		fail(w, errNoHelper)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	if _, _, pending := a.Net.NetPending(); pending {
		api.Error(w, http.StatusConflict, netcfg.ErrPending.Error())
		return
	}
	if _, _, pending := a.FW.Pending(); pending {
		api.Error(w, http.StatusConflict, fw.ErrPending.Error())
		return
	}
	if a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled {
		if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
			api.Error(w, http.StatusForbidden, "enter a current code from your authenticator to apply this change")
			return
		}
	}
	d := a.bridgeDraft()
	if d == nil || sameBridge(a.bridgeLive(), d) {
		fail(w, errors.New("there is nothing to apply"))
		return
	}
	if _, err := a.bridgeCheck(d, c.IP); err != nil {
		fail(w, err)
		return
	}
	// Build both halves first, so nothing is started unless both are valid.
	p, err := a.buildPlan(a.Config)
	if err != nil {
		fail(w, err)
		return
	}
	if d.Enabled {
		applyBridgeToPlan(p, d)
	} else {
		// Switching off: the saved networks come back as they were.
		p, err = a.buildPlanWithout(d)
		if err != nil {
			fail(w, err)
			return
		}
	}
	if err := a.validatePlan(p); err != nil {
		fail(w, err)
		return
	}
	m, err := fw.Load(a.Config)
	if err != nil {
		fail(w, err)
		return
	}
	m.Bridge = d
	script, err := fw.Compile(m)
	if err != nil {
		fail(w, err)
		return
	}
	full := a.fullScript(m, script)
	if err := a.FW.Check(full); err != nil {
		if d.Enabled && strings.Contains(err.Error(), "Protocol error") {
			err = errors.New("this machine's system cannot follow connections across a bridge (the nf_conntrack_bridge part is missing), so bridge mode cannot be turned on here. Nothing was changed")
		}
		fail(w, err)
		return
	}
	a.restorePoint()
	a.Config.SetSetting(bridgeApplyingSetting, "1")
	ntok, ndl, err := a.Net.NetApply(*p, confirmWindow)
	if err != nil {
		a.Config.SetSetting(bridgeApplyingSetting, "")
		fail(w, err)
		return
	}
	_, fdl, err := a.FW.Apply(full, confirmWindow, c.User.Username)
	if err != nil {
		// The firewall half failed after the network half started: put the network back at once.
		a.Net.NetRollback()
		a.Config.SetSetting(bridgeApplyingSetting, "")
		fail(w, err)
		return
	}
	res, _ := a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status) VALUES(?,?,?,?,'pending')`,
		time.Now().Unix(), c.User.Username, bridgeSummary(d), script)
	ver, _ := res.LastInsertId()
	bridgeState.mu.Lock()
	bridgeState.connID, bridgeState.version, bridgeState.print = api.ConnID(r), ver, fingerprint(p)
	bridgeState.mu.Unlock()
	_ = ntok
	dl := ndl
	if fdl.After(dl) {
		dl = fdl
	}
	a.publishFW()
	a.publishNet()
	w.Header().Set("Connection", "close")
	api.JSON(w, 200, map[string]any{"deadline": dl.Unix(), "seconds": int(confirmWindow / time.Second), "address": d.Addr, "mode": d.Mode})
}

// buildPlanWithout builds the plan from the saved networks, ignoring any live bridge setup.
func (a *App) buildPlanWithout(d *bridgemode.Config) (*netd.Plan, error) {
	return a.buildPlanFrom(a.Config, nil)
}

func (a *App) bridgeConfirm(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	if a.Net == nil || a.FW == nil {
		fail(w, errNoHelper)
		return
	}
	if !api.Decode(w, r, &struct{}{}) {
		return
	}
	bridgeState.mu.Lock()
	same := bridgeState.connID != 0 && bridgeState.connID == api.ConnID(r)
	ver, print := bridgeState.version, bridgeState.print
	bridgeState.mu.Unlock()
	if same {
		api.Error(w, http.StatusConflict, "confirm from a new connection; the old one can outlive a broken change")
		return
	}
	ntok, _, nok := a.Net.NetPending()
	ftok, _, fok := a.FW.Pending()
	if !nok && !fok {
		api.Error(w, http.StatusConflict, "there is no change waiting to be confirmed; it may already have been undone")
		return
	}
	if nok {
		if err := a.Net.NetConfirm(ntok); err != nil {
			fail(w, err)
			return
		}
	}
	if fok {
		if err := a.FW.Confirm(ftok); err != nil {
			fail(w, err)
			return
		}
	}
	// Both halves are in place for good: the staged setup becomes the live one.
	d := a.bridgeDraft()
	if d != nil {
		b, _ := json.Marshal(d)
		a.Config.SetSetting(fw.BridgeSetting, string(b))
	}
	a.Config.SetSetting(bridgeDraftSetting, "")
	a.Config.SetSetting(bridgeApplyingSetting, "")
	if print != "" {
		a.Config.SetSetting("net.applied", print)
	}
	a.Config.Exec(`UPDATE fw_revisions SET status='confirmed' WHERE version=?`, ver)
	a.Config.Exec(`UPDATE fw_revisions SET status='superseded' WHERE status='confirmed' AND version<?`, ver)
	a.fwSync()
	a.publishFW()
	a.publishNet()
	ok(w)
}

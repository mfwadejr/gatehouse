package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/ha"
	"gatehouse/internal/netd"
)

func (a *App) haOwns(addr string) bool {
	if a.haOwnsFn != nil {
		return a.haOwnsFn(addr)
	}
	as, _ := net.InterfaceAddrs()
	for _, x := range as {
		if n, ok := x.(*net.IPNet); ok && n.IP.String() == addr {
			return true
		}
	}
	return false
}

// haGate keeps the standby unit read-only: what is changed is changed on the active unit and copied.
func (a *App) haGate(method, path string) (bool, string) {
	if !a.haStandbyNow() {
		return true, ""
	}
	if ha.Allowed(ha.Standby, method, path) {
		return true, ""
	}
	where := ""
	if s := a.haLoad(); s != nil {
		where = " at " + s.peerAddr()
	}
	return false, "This is the standby unit, so settings are read-only here. Make the change on the active unit" + where + "; it is copied across within a minute."
}

// haNetworks lists the networks that get a shared address: every network with its own fixed address.
type haNet struct {
	Iface string
	CIDR  string
}

func (a *App) haNetworks() []haNet {
	var out []haNet
	if ifs, err := a.listIfaces(a.Config); err == nil {
		for _, i := range ifs {
			if i.Enabled && i.IPv4 != "" && !i.DHCP {
				out = append(out, haNet{i.Name, i.IPv4})
			}
		}
	}
	return out
}

func (a *App) haPeerStatus(s *haLocal) map[string]any {
	if s == nil || s.PeerFP == "" {
		return map[string]any{"reachable": false}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	b, err := ha.Call(ctx, s.peerHost(), s.Self, s.PeerFP, "GET", "/ha/status", nil, nil, "")
	if err != nil {
		return map[string]any{"reachable": false, "error": err.Error()}
	}
	var st ha.Status
	json.Unmarshal(b, &st)
	return map[string]any{"reachable": true, "version": st.Version, "active": st.Active, "role": st.Role, "seq": st.Seq}
}

func (a *App) haStatus() map[string]any {
	st := map[string]any{"helper": a.Net != nil, "bridge_on": a.bridgeLive() != nil && a.bridgeLive().Enabled}
	s := a.haLoad()
	st["paired"] = s != nil
	if s == nil {
		st["can_start"] = a.Net != nil && !(a.bridgeLive() != nil && a.bridgeLive().Enabled)
		return st
	}
	st["role"], st["activated"], st["applied"] = s.Role, s.Activated, s.Applied
	st["active"] = a.haActive(s)
	st["standby"] = a.haStandbyNow()
	st["own_addr"], st["peer_addr"] = s.ownAddr(), s.peerAddr()
	st["vips"], st["mgmt_iface"] = s.local().VIPs, s.MgmtIface
	st["synced_at"], st["error"], st["version"] = s.SyncedAt, s.LastError, a.Cfg.Version
	st["joined"] = s.PeerFP != "" && (s.Role == ha.Standby || s.PeerFP != "")
	st["peer"] = a.haPeerStatus(s)
	st["warm"] = a.haWarmOn()
	st["primary"] = s.isPreferred()
	if fb := a.haFailbackState(s); fb != nil {
		st["failback"] = fb
	}
	if own, peer, ok := s.heart(); ok {
		st["heart"] = map[string]any{"dev": s.HeartDev, "own": own.Addr().String(), "peer": peer.String(), "connected": heartCarrier(s.HeartDev)}
	} else {
		st["heart"] = nil
	}
	st["heart_choices"] = a.haHeartChoices(s)
	if s.Role == ha.Primary && !s.Activated || s.PeerFP == "" {
		if code, err := ha.Encode(s.pairingForCode()); err == nil && s.Role == ha.Primary {
			st["code"] = code
		}
	}
	return st
}

func (s *haLocal) pairingForCode() ha.Pairing { return s.Pairing }

func (a *App) registerHARoutes() {
	s := a.API
	s.Gate = a.haGate
	a.registerFailbackRoutes()
	s.HandleAdmin("GET /api/ha", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		st := a.haStatus()
		st["ports"] = a.haPortList()
		st["roles"] = a.haRoles()
		if s := a.haLoad(); s == nil {
			nets := []map[string]string{}
			for _, n := range a.haNetworks() {
				nets = append(nets, map[string]string{"iface": n.Iface, "cidr": n.CIDR})
			}
			st["networks"] = nets
		}
		api.JSON(w, 200, st)
	})

	// Primary: start a pairing. Stages the shared-address layout and returns the code.
	s.HandleAdmin("POST /api/ha/start", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			OwnAddr     string `json:"own_addr"`
			StandbyAddr string `json:"standby_addr"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if a.haLoad() != nil {
			fail(w, errors.New("this unit is already paired"))
			return
		}
		if l := a.bridgeLive(); l != nil && l.Enabled {
			fail(w, errors.New("bridge mode and high availability cannot be used together yet; switch bridge mode off first"))
			return
		}
		if a.Net == nil || a.FW == nil {
			fail(w, errNoHelper)
			return
		}
		nets := a.haNetworks()
		cip, _ := netip.ParseAddr(c.IP)
		mgmt, cidr := "", ""
		for _, n := range nets {
			if pf, err := netip.ParsePrefix(n.CIDR); err == nil && pf.Contains(cip) {
				mgmt, cidr = n.Iface, n.CIDR
			}
		}
		if mgmt == "" && cip.IsLoopback() && len(nets) > 0 {
			// Working on the machine itself: the first network is the one the units talk over.
			if pf, err := netip.ParsePrefix(nets[0].CIDR); err == nil {
				mgmt, cidr, cip = nets[0].Iface, nets[0].CIDR, pf.Addr()
			}
		}
		if mgmt == "" {
			fail(w, errors.New("open the console from a computer on one of your own networks to start; that network is the one the two units talk over"))
			return
		}
		pf, _ := netip.ParsePrefix(cidr)
		var vips []ha.VIP
		for _, n := range nets {
			vips = append(vips, ha.VIP{Iface: n.Iface, CIDR: n.CIDR})
		}
		for _, x := range []string{in.OwnAddr, in.StandbyAddr} {
			ad, err := netip.ParseAddr(strings.TrimSpace(x))
			if err != nil || !pf.Contains(ad) {
				fail(w, fmt.Errorf("%q is not an address inside %s", x, pf.Masked()))
				return
			}
		}
		id, err := ha.NewIdentity("primary")
		if err != nil {
			fail(w, err)
			return
		}
		secret, _ := ha.NewSecret()
		p := ha.Pairing{V: 1, ID: strings.ToLower(secret[:6]), Secret: secret, Primary: strings.TrimSpace(in.OwnAddr), Standby: strings.TrimSpace(in.StandbyAddr),
			VIPs: vips, FP: id.FP, Port: a.haPort()}
		if _, err := ha.Encode(p); err != nil {
			fail(w, err)
			return
		}
		// Stage the layout: the unit's own address on the shared network, shared addresses only
		// (no fixed address) on the others, and the gateway and DNS handed out by DHCP are the shared ones.
		tx, err := a.Config.Begin()
		if err != nil {
			fail(w, err)
			return
		}
		defer tx.Rollback()
		for _, n := range nets {
			addr := ""
			if n.Iface == mgmt {
				addr = fmt.Sprintf("%s/%d", p.Primary, pf.Bits())
			}
			if _, err := tx.Exec(`UPDATE interfaces SET ipv4_cidr=? WHERE name=?`, addr, n.Iface); err != nil {
				fail(w, err)
				return
			}
			vp, _ := netip.ParsePrefix(n.CIDR)
			if _, err := tx.Exec(`UPDATE dhcp_scopes SET router=? WHERE iface=? AND router=''`, vp.Addr().String(), n.Iface); err != nil {
				fail(w, err)
				return
			}
			if _, err := tx.Exec(`UPDATE dhcp_scopes SET dns=? WHERE iface=? AND dns=''`, vp.Addr().String(), n.Iface); err != nil {
				fail(w, err)
				return
			}
		}
		if err := tx.Commit(); err != nil {
			fail(w, err)
			return
		}
		st := &haLocal{Pairing: p, Role: ha.Primary, Self: id, MgmtIface: mgmt}
		if err := a.haSave(st); err != nil {
			fail(w, err)
			return
		}
		a.haSetFirewallPeer(st)
		a.publishNet()
		code, _ := ha.Encode(p)
		a.event("system", "info", "A backup unit is being set up", "Apply the network and firewall changes, then paste the code on the second machine.")
		api.JSON(w, 200, map[string]any{"code": code, "mgmt_iface": mgmt})
	})

	// Backup: look at the primary before joining.
	s.HandleAdmin("POST /api/ha/discover", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Code string `json:"code"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		p, err := ha.Decode(in.Code)
		if err != nil {
			fail(w, err)
			return
		}
		if cur := a.haLoad(); cur != nil && cur.Role == ha.Primary {
			fail(w, errors.New("this unit is already set up as a primary"))
			return
		}
		if !a.haOwns(p.Standby) {
			fail(w, fmt.Errorf("this machine does not have the address %s that the code gives to the backup. Give it that address (or start again on the primary with this machine's real address)", p.Standby))
			return
		}
		id, st, err := a.haJoinIdentity(p)
		if err != nil {
			fail(w, err)
			return
		}
		ov, err := a.haOverview(p, id)
		if err != nil {
			fail(w, err)
			return
		}
		ms, merr := ha.MapPorts(ov.Roles, a.haPortList(), nil)
		res := map[string]any{"roles": ov.Roles, "primary_version": ov.Version, "version": a.Cfg.Version, "ports": a.haPortList()}
		if ov.Version != a.Cfg.Version {
			res["warning"] = fmt.Sprintf("The primary runs %s and this machine runs %s. Update the older one first so both match.", ov.Version, a.Cfg.Version)
		}
		if merr != nil {
			res["refused"] = merr.Error()
		} else {
			res["mapping"] = ms
		}
		_ = st
		api.JSON(w, 200, res)
	})

	// Backup: join, with the port choices.
	s.HandleAdmin("POST /api/ha/join", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Code string            `json:"code"`
			Keep map[string]string `json:"keep"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		p, err := ha.Decode(in.Code)
		if err != nil {
			fail(w, err)
			return
		}
		id, st, err := a.haJoinIdentity(p)
		if err != nil {
			fail(w, err)
			return
		}
		ov, err := a.haOverview(p, id)
		if err != nil {
			fail(w, err)
			return
		}
		ms, err := ha.MapPorts(ov.Roles, a.haPortList(), in.Keep)
		if err != nil {
			fail(w, err)
			return
		}
		mi := mgmtIface(p)
		if mi == "" {
			fail(w, errors.New("the code does not say which network the units talk over"))
			return
		}
		st.Mapping, st.MgmtIface = ms, renamePort(mi, ms)
		if err := a.haSave(st); err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		_, err = ha.Call(ctx, st.peerHost(), id, p.FP, "POST", "/ha/join", nil, map[string]string{"X-GH-Port": fmt.Sprint(a.haPort())}, p.Secret)
		if err != nil {
			a.haSave(nil)
			fail(w, err)
			return
		}
		a.haSetFirewallPeer(st)
		api.JSON(w, 200, map[string]any{"status": "ok", "mapping": ms})
	})

	// Both units: after the network and firewall changes are applied, start the shared-address service.
	s.HandleAdmin("POST /api/ha/activate", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		st := a.haLoad()
		if st == nil {
			fail(w, errors.New("this unit is not paired"))
			return
		}
		if st.PeerFP == "" {
			fail(w, errors.New("the second machine has not joined yet"))
			return
		}
		if a.fwStatus()["dirty"] == true {
			fail(w, errors.New("apply the firewall changes first (Firewall page), so the two units may talk to each other"))
			return
		}
		if a.netDirty() {
			fail(w, errors.New("apply the network changes first (Networks page)"))
			return
		}
		if st.Role == ha.Standby && st.SyncedAt == 0 {
			fail(w, errors.New("the settings have not arrived from the primary yet; wait a moment and try again"))
			return
		}
		if err := a.haInstall(st); err != nil {
			fail(w, err)
			return
		}
		a.haUpdate(func(x *haLocal) { x.Activated, x.Applied = true, true })
		a.event("system", "info", "High availability is on", "This unit and its partner are now a pair.")
		ok(w)
	})

	s.HandleAdmin("POST /api/ha/sync", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		st := a.haLoad()
		if st == nil || st.PeerFP == "" {
			fail(w, errors.New("this unit is not paired"))
			return
		}
		if !a.haActive(st) && st.Activated {
			fail(w, errors.New("settings are sent from the active unit; do this on the active unit"))
			return
		}
		if err := a.haPush(true); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})

	// Make this unit the active one: ask the active unit to step aside.
	s.HandleAdmin("POST /api/ha/failover", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Code string `json:"code"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		st := a.haLoad()
		if st == nil || !st.Activated {
			fail(w, errors.New("high availability is not running yet"))
			return
		}
		if a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled {
			if err := a.Auth.VerifyStepUp(c.User.ID, in.Code); err != nil {
				api.Error(w, http.StatusForbidden, "enter a current code from your authenticator")
				return
			}
		}
		if a.haActive(st) {
			fail(w, errors.New("this unit is already the active one"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if _, err := ha.Call(ctx, st.peerHost(), st.Self, st.PeerFP, "POST", "/ha/handover", nil, nil, ""); err != nil {
			fail(w, err)
			return
		}
		a.event("system", "warning", "The active role was handed to this unit", "")
		ok(w)
	})

	// The optional cable to the partner: pick a spare port, or "" to stop using one.
	s.HandleAdmin("PUT /api/ha/heart", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Dev string `json:"dev"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		st := a.haLoad()
		if st == nil {
			fail(w, errors.New("this unit is not paired"))
			return
		}
		if in.Dev != "" {
			good := false
			for _, ch := range a.haHeartChoices(st) {
				good = good || ch == in.Dev
			}
			if !good {
				fail(w, errors.New(in.Dev+" cannot be used for the cable: it must be a wired port that nothing else uses"))
				return
			}
			own, _, ok := (&haLocal{Pairing: st.Pairing, Role: st.Role, HeartDev: in.Dev}).heart()
			if !ok {
				fail(w, errors.New("the cable network could not be worked out"))
				return
			}
			if clash := a.heartClash(own); clash != "" {
				fail(w, errors.New("the cable's private network ("+own.Masked().String()+") overlaps "+clash+"; change that network first"))
				return
			}
		}
		prev := st.HeartDev
		a.haUpdate(func(x *haLocal) { x.HeartDev = in.Dev })
		st = a.haLoad()
		if st.Activated {
			if err := a.haInstall(st); err != nil {
				a.haUpdate(func(x *haLocal) { x.HeartDev = prev })
				fail(w, err)
				return
			}
		}
		a.haSetHeartSetting(st)
		if err := a.refreshFirewallQuiet(c.User.Username); err != nil {
			fail(w, errors.New("the firewall could not be updated: "+err.Error()))
			return
		}
		a.publishFW()
		ok(w)
	})

	s.HandleAdmin("POST /api/ha/unpair", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		st := a.haLoad()
		if st == nil {
			ok(w)
			return
		}
		if st.PeerFP != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			ha.Call(ctx, st.peerHost(), st.Self, st.PeerFP, "POST", "/ha/leave", nil, nil, "")
			cancel()
		}
		if err := a.haLeave(st); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
}

// haLeave turns this unit back into a single, ordinary one.
func (a *App) haLeave(st *haLocal) error {
	if a.Net != nil && st.Activated {
		if err := a.Net.HAApply(haOff); err != nil {
			return err
		}
	}
	a.haSave(nil)
	a.haSetFirewallPeer(nil)
	a.Config.Exec(`DELETE FROM settings WHERE key='ha.heart'`)
	a.DHCP.Silent.Store(false)
	a.event("system", "info", "This unit is no longer part of a pair", "It runs on its own again. Apply the network changes if the addresses need to go back.")
	return nil
}

var haOff = netd.HAPlan{}

// haPort is the port this unit's link listens on.
func (a *App) haPort() int {
	if v, ok := a.haBound.Load().(string); ok {
		if _, p, err := net.SplitHostPort(v); err == nil {
			var n int
			fmt.Sscan(p, &n)
			if n > 0 {
				return n
			}
		}
	}
	return ha.LinkPort
}

// haJoinIdentity makes (or finds) this unit's side of a pairing it is about to join.
func (a *App) haJoinIdentity(p ha.Pairing) (ha.Identity, *haLocal, error) {
	if cur := a.haLoad(); cur != nil {
		if cur.Role == ha.Standby && cur.Pairing.Secret == p.Secret {
			return cur.Self, cur, nil
		}
		return ha.Identity{}, nil, errors.New("this unit is already paired; remove that pairing first")
	}
	id, err := ha.NewIdentity("standby")
	if err != nil {
		return id, nil, err
	}
	st := &haLocal{Pairing: p, Role: ha.Standby, Self: id, PeerFP: p.FP, PeerPort: p.Port}
	if err := a.haSave(st); err != nil {
		return id, nil, err
	}
	return id, st, nil
}

func (a *App) haOverview(p ha.Pairing, id ha.Identity) (ha.Overview, error) {
	var ov ha.Overview
	port := p.Port
	if port == 0 {
		port = ha.LinkPort
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := ha.Call(ctx, net.JoinHostPort(p.Primary, fmt.Sprint(port)), id, p.FP, "GET", "/ha/overview", nil, nil, p.Secret)
	if err != nil {
		return ov, fmt.Errorf("could not reach the primary at %s: %w", p.Primary, err)
	}
	if json.Unmarshal(b, &ov) != nil {
		return ov, errors.New("the primary's answer was not understood")
	}
	return ov, nil
}

// netDirty says whether network changes are waiting to be applied.
func (a *App) netDirty() bool {
	p, err := a.buildPlan(a.Config)
	if err != nil {
		return true
	}
	return fingerprint(p) != a.Config.Setting("net.applied", "")
}

// afterHACopy runs after settings arrive from the partner. The first copy waits for the person to
// apply it with the usual way back; later copies are applied straight away, so the standby always
// has the active unit's rules loaded and takes over without an apply.
func (a *App) afterHACopy(s *haLocal) {
	a.publishFW()
	a.publishNet()
	if !s.Applied {
		return
	}
	go a.haAutoApply()
}

func (a *App) haAutoApply() {
	a.haApplyMu.Lock()
	defer a.haApplyMu.Unlock()
	if a.FW == nil || a.Net == nil {
		return
	}
	note := func(msg string, err error) {
		a.haUpdate(func(x *haLocal) { x.LastError = msg + ": " + err.Error() })
		a.event("system", "warning", "The copied settings could not be loaded on this unit", msg+": "+err.Error())
	}
	if m, script, err := a.currentRuleset(); err == nil && script != a.lastApplied() {
		if _, _, pending := a.FW.Pending(); !pending {
			tok, _, err := a.FW.Apply(a.fullScript(m, script), confirmWindow, "partner unit")
			if err == nil {
				err = a.FW.Confirm(tok)
			}
			if err != nil {
				note("firewall", err)
			} else {
				a.Config.Exec(`UPDATE fw_revisions SET status='superseded' WHERE status='confirmed'`)
				a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status) VALUES(?,?,?,?,'confirmed')`,
					time.Now().Unix(), "partner unit", "Settings copied from the active unit.", script)
				a.fwSync()
			}
		}
	}
	if p, err := a.buildPlan(a.Config); err == nil && fingerprint(p) != a.Config.Setting("net.applied", "") {
		if _, _, pending := a.Net.NetPending(); !pending {
			tok, _, err := a.Net.NetApply(*p, confirmWindow)
			if err == nil {
				err = a.Net.NetConfirm(tok)
			}
			if err != nil {
				note("network", err)
			} else {
				a.Config.SetSetting("net.applied", fingerprint(p))
				// Keep the standby's internet ports down again: applying brings them up.
				if st := a.haLoad(); st != nil && st.Activated && !a.haActive(st) {
					a.haInstall(st)
				}
			}
		}
	}
	a.publishFW()
	a.publishNet()
}

// haHeartChoices are the ports that could carry the cable: wired, and used by nothing.
func (a *App) haHeartChoices(st *haLocal) []string {
	used := map[string]bool{}
	for _, r := range a.haRoles() {
		used[r.Port] = true
	}
	if ifs, err := a.listIfaces(a.Config); err == nil {
		for _, i := range ifs {
			used[i.Name], used[i.Parent] = true, true
			for _, m := range i.Members {
				used[m] = true
			}
		}
	}
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			used[w.Iface] = true
		}
	}
	out := []string{}
	for _, p := range a.haPortList() {
		if !p.Wireless && !used[p.Name] || st != nil && p.Name == st.HeartDev && !p.Wireless {
			out = append(out, p.Name)
		}
	}
	return out
}

// heartClash names a network of this unit that overlaps the cable's, or "".
func (a *App) heartClash(own netip.Prefix) string {
	ifs, err := a.listIfaces(a.Config)
	if err != nil {
		return ""
	}
	for _, i := range ifs {
		if pf, err := netip.ParsePrefix(i.IPv4); err == nil && pf.Masked().Overlaps(own.Masked()) {
			return i.Name
		}
	}
	return ""
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/netd"
)

// The management port: one spare port that carries only a management address. The console and
// SSH answer there, nothing is routed through it, and (when asked) the console is closed on the
// LAN while the port is healthy. See fw/compile.go for the rules, which never change at runtime:
// only the contents of two sets do.

const (
	mgmtSetting    = "mgmt.port"
	mgmtPendingKV  = "mgmt.port.pending"
	mgmtConfirm    = 60 * time.Second
	mgmtCloseAfter = 30 * time.Second // healthy this long before the LAN console closes
	mgmtOpenAfter  = 60 * time.Second // unhealthy this long before the LAN console opens again
)

type mgmtPortCfg struct {
	Iface    string `json:"iface"`
	Mode     string `json:"mode"` // dhcp or static
	CIDR     string `json:"cidr"`
	Gateway  string `json:"gateway"`
	CloseLAN bool   `json:"close_lan"`
}

type mgmtPending struct {
	Until int64  `json:"until"`
	Prev  string `json:"prev"` // the setting as it was ("" = none)
}

// mgmtAddrOf reports the IPv4 address on a port and whether it has carrier. A seam for tests.
var mgmtAddrOf = func(dev string) (netip.Addr, bool) {
	ifi, err := net.InterfaceByName(dev)
	if err != nil {
		return netip.Addr{}, false
	}
	carrier := ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagRunning != 0
	addrs, _ := ifi.Addrs()
	for _, ad := range addrs {
		if n, ok := ad.(*net.IPNet); ok {
			if v, ok := netip.AddrFromSlice(n.IP.To4()); ok && v.Is4() {
				return v, carrier
			}
		}
	}
	return netip.Addr{}, carrier
}

var mgmtState struct {
	sync.Mutex
	lanOpen        bool // the console is open on the LAN (the default until the port is proven)
	init           bool
	healthySince   time.Time
	unhealthySince time.Time
	healthy        bool
}

func (a *App) mgmtCfg() mgmtPortCfg {
	var c mgmtPortCfg
	json.Unmarshal([]byte(a.Config.Setting(mgmtSetting, "")), &c)
	return c
}

func (a *App) mgmtPendingGet() *mgmtPending {
	var p mgmtPending
	if json.Unmarshal([]byte(a.Config.Setting(mgmtPendingKV, "")), &p) != nil || p.Until == 0 {
		return nil
	}
	return &p
}

// mgmtLanOpen says whether the console is open on the LAN right now.
func (a *App) mgmtLanOpen() bool {
	mgmtState.Lock()
	defer mgmtState.Unlock()
	if !mgmtState.init {
		return true
	}
	return mgmtState.lanOpen
}

func (a *App) mgmtAddress(c mgmtPortCfg) (netip.Addr, bool) {
	if c.Iface == "" {
		return netip.Addr{}, false
	}
	return mgmtAddrOf(c.Iface)
}

// mgmtHealthy: carrier, an IPv4 address, and a neighbour (the gateway when one is set) seen.
func (a *App) mgmtHealthy(c mgmtPortCfg) bool {
	addr, carrier := a.mgmtAddress(c)
	if !carrier || !addr.IsValid() {
		return false
	}
	gw := strings.TrimSpace(c.Gateway)
	for ip, e := range arpTable() {
		if e.Dev != c.Iface {
			continue
		}
		if gw == "" || ip == gw {
			return true
		}
	}
	return false
}

// mgmtChoices are the wired ports free for the management port.
func (a *App) mgmtChoices(cur string) []string {
	used := map[string]bool{}
	for _, r := range a.haRoles() {
		used[r.Port] = true
	}
	if d, _, ok := strings.Cut(a.Config.Setting("ha.heart", ""), "|"); ok {
		used[d] = true
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
		if !used[p.Name] || p.Name == cur {
			out = append(out, p.Name)
		}
	}
	return out
}

// mgmtClash names something this device already serves that overlaps the address.
func (a *App) mgmtClash(own netip.Prefix) string {
	if n := a.heartClash(own); n != "" {
		return n
	}
	if rows, err := a.Config.Query(`SELECT name, subnet FROM dhcp_scopes WHERE subnet<>''`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var n, s string
			rows.Scan(&n, &s)
			if pf, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil && pf.Masked().Overlaps(own.Masked()) {
				return n
			}
		}
	}
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			if pf, err := netip.ParsePrefix(strings.TrimSpace(w.Addr)); err == nil && pf.Masked().Overlaps(own.Masked()) {
				return w.Name
			}
		}
	}
	return ""
}

func (a *App) mgmtValidate(c *mgmtPortCfg) error {
	if c.Iface == "" {
		*c = mgmtPortCfg{}
		return nil
	}
	if a.bridgeLive() != nil {
		return fmt.Errorf("Bridge mode has its own out-of-band port. A separate management port is not used there.")
	}
	okPort := false
	for _, p := range a.mgmtChoices(a.mgmtCfg().Iface) {
		okPort = okPort || p == c.Iface
	}
	if !okPort {
		return fmt.Errorf("%s is not a free port. It may already serve a network, face the internet, or be used for high availability.", c.Iface)
	}
	switch c.Mode {
	case "dhcp":
		c.CIDR, c.Gateway = "", ""
	case "static":
		pf, err := netip.ParsePrefix(strings.TrimSpace(c.CIDR))
		if err != nil || !pf.Addr().Is4() {
			return fmt.Errorf("Type the address with its prefix, like 10.20.0.5/24.")
		}
		if n := a.mgmtClash(pf); n != "" {
			return fmt.Errorf("That address overlaps %s, which this device already serves. Pick a different range.", n)
		}
		c.CIDR = pf.String()
		c.Gateway = strings.TrimSpace(c.Gateway)
		if c.Gateway != "" {
			gw, err := netip.ParseAddr(c.Gateway)
			if err != nil || !gw.Is4() || !pf.Masked().Contains(gw) {
				return fmt.Errorf("The gateway must be an address on the same subnet.")
			}
		}
	default:
		return fmt.Errorf("Choose DHCP or a fixed address.")
	}
	return nil
}

func (a *App) mgmtPlan(c mgmtPortCfg) netd.MgmtPlan {
	return netd.MgmtPlan{Dev: c.Iface, Mode: c.Mode, CIDR: c.CIDR, Gateway: c.Gateway}
}

// mgmtApplyCfg stores a configuration, sets the port up and refreshes the firewall sets.
func (a *App) mgmtApplyCfg(c mgmtPortCfg) error {
	raw := ""
	if c.Iface != "" {
		b, _ := json.Marshal(c)
		raw = string(b)
	}
	if a.Net != nil {
		if err := a.Net.MgmtApply(a.mgmtPlan(c)); err != nil {
			return err
		}
	}
	if raw == "" {
		a.Config.Exec(`DELETE FROM settings WHERE key=?`, mgmtSetting)
	} else {
		a.Config.SetSetting(mgmtSetting, raw)
	}
	mgmtState.Lock()
	mgmtState.healthy, mgmtState.healthySince, mgmtState.unhealthySince = false, time.Time{}, time.Time{}
	mgmtState.init, mgmtState.lanOpen = true, true
	mgmtState.Unlock()
	a.fwSync()
	a.publishFW()
	return nil
}

// mgmtConfirmFrom confirms a pending change when a signed-in request arrived on the new address.
func (a *App) mgmtSeen(r *http.Request) {
	p := a.mgmtPendingGet()
	if p == nil {
		return
	}
	la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return
	}
	host, _, err := net.SplitHostPort(la.String())
	if err != nil {
		return
	}
	addr, _ := a.mgmtAddress(a.mgmtCfg())
	if addr.IsValid() && host == addr.String() {
		a.Config.Exec(`DELETE FROM settings WHERE key=?`, mgmtPendingKV)
		a.event("system", "info", "Management port confirmed", "The console was reached on the management port, so the change was kept.")
	}
}

func (a *App) mgmtRevert(why string) {
	p := a.mgmtPendingGet()
	if p == nil {
		return
	}
	var prev mgmtPortCfg
	if p.Prev != "" {
		json.Unmarshal([]byte(p.Prev), &prev)
	}
	a.Config.Exec(`DELETE FROM settings WHERE key=?`, mgmtPendingKV)
	if err := a.mgmtApplyCfg(prev); err != nil {
		a.event("system", "warn", "Management port could not be put back", err.Error())
		return
	}
	a.event("system", "warn", "Management port change undone", why)
}

// mgmtFill is what the two firewall sets should hold now.
func (a *App) mgmtIfElems() []string {
	c := a.mgmtCfg()
	if c.Iface == "" || a.bridgeLive() != nil {
		return nil
	}
	return []string{c.Iface}
}

func (a *App) mgmtLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.mgmtTick(time.Now())
		}
	}
}

func (a *App) mgmtTick(now time.Time) {
	if p := a.mgmtPendingGet(); p != nil && now.Unix() >= p.Until {
		a.mgmtRevert("Nobody opened the console through the management port within a minute.")
		return
	}
	c := a.mgmtCfg()
	mgmtState.Lock()
	defer mgmtState.Unlock()
	if !mgmtState.init {
		mgmtState.init, mgmtState.lanOpen = true, true
	}
	if c.Iface == "" || !c.CloseLAN {
		if !mgmtState.lanOpen {
			mgmtState.lanOpen = true
			go a.fwSync()
		}
		return
	}
	h := a.mgmtHealthy(c)
	if h {
		if !mgmtState.healthy {
			mgmtState.healthy, mgmtState.healthySince = true, now
		}
		mgmtState.unhealthySince = time.Time{}
		if mgmtState.lanOpen && now.Sub(mgmtState.healthySince) >= mgmtCloseAfter {
			mgmtState.lanOpen = false
			go a.fwSync()
			go a.event("system", "info", "Console closed on the LAN", "The management port is healthy, so the console now answers there only.")
		}
		return
	}
	if mgmtState.healthy || mgmtState.unhealthySince.IsZero() {
		mgmtState.healthy = false
		if mgmtState.unhealthySince.IsZero() {
			mgmtState.unhealthySince = now
		}
	}
	mgmtState.healthySince = time.Time{}
	if !mgmtState.lanOpen && now.Sub(mgmtState.unhealthySince) >= mgmtOpenAfter {
		mgmtState.lanOpen = true
		go a.fwSync()
		go a.event("system", "warn", "Console opened on the LAN again", "The management port stopped answering, so the console is available on the LAN until it returns.")
	}
}

func (a *App) mgmtStatus() map[string]any {
	c := a.mgmtCfg()
	addr, carrier := a.mgmtAddress(c)
	state, word := "off", "No management port is set."
	switch {
	case c.Iface == "":
	case !carrier:
		state, word = "down", "No cable or nothing on the other end of "+c.Iface+"."
	case !addr.IsValid() && c.Mode == "dhcp":
		state, word = "waiting", "Waiting for an address from DHCP."
	case !addr.IsValid():
		state, word = "waiting", "The port has no address yet."
	case a.mgmtHealthy(c):
		state, word = "ok", "The management port answers on "+addr.String()+"."
	default:
		state, word = "quiet", addr.String()+" is set, but no other device has been seen on this port yet."
	}
	out := map[string]any{
		"config": c, "choices": a.mgmtChoices(c.Iface), "state": state, "status": word,
		"address": "", "lan_open": a.mgmtLanOpen(),
		"bridge":    a.bridgeLive() != nil,
		"dhcp_note": "If no DHCP server answers, this port has no address. Use a fixed address if you need to be sure of reaching it.",
	}
	if addr.IsValid() {
		out["address"] = addr.String()
	}
	if p := a.mgmtPendingGet(); p != nil {
		out["pending"] = map[string]any{"until": p.Until, "seconds": int(time.Until(time.Unix(p.Until, 0)).Seconds())}
	}
	// The ruleset text for this feature reaches the live firewall only with the next apply.
	if c.Iface != "" && !strings.Contains(a.lastApplied(), "set "+"mgmt_if") {
		out["needs_apply"] = true
	}
	return out
}

func (a *App) registerMgmtRoutes() {
	s := a.API
	a.API.Seen = a.mgmtSeen
	s.Handle("GET /api/mgmt/port", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.mgmtStatus())
	})
	s.HandleAdmin("PUT /api/mgmt/port", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in mgmtPortCfg
		if !api.Decode(w, r, &in) {
			return
		}
		if a.mgmtPendingGet() != nil {
			fail(w, fmt.Errorf("A change is waiting to be confirmed. Open the console on the management address, or undo it first."))
			return
		}
		if err := a.mgmtValidate(&in); err != nil {
			fail(w, err)
			return
		}
		old := a.Config.Setting(mgmtSetting, "")
		if in.CloseLAN {
			if in.Iface == "" {
				in.CloseLAN = false
			} else {
				addr, _ := a.mgmtAddress(in)
				via := a.mgmtCfg().Iface == in.Iface && addr.IsValid() && localHost(r) == addr.String()
				if !via || !a.mgmtHealthy(in) {
					fail(w, fmt.Errorf("Open this console through the management port first, and check that it is healthy. Then the LAN can be closed safely."))
					return
				}
			}
		}
		changedPort := in.Iface != a.mgmtCfg().Iface || in.Mode != a.mgmtCfg().Mode || in.CIDR != a.mgmtCfg().CIDR || in.Gateway != a.mgmtCfg().Gateway
		if changedPort && in.Iface != "" {
			pend, _ := json.Marshal(mgmtPending{Until: time.Now().Add(mgmtConfirm).Unix(), Prev: old})
			a.Config.SetSetting(mgmtPendingKV, string(pend))
		}
		if err := a.mgmtApplyCfg(in); err != nil {
			a.Config.Exec(`DELETE FROM settings WHERE key=?`, mgmtPendingKV)
			fail(w, err)
			return
		}
		a.auditEvent(c.User.Username, "info", "Management port changed", mgmtSummary(in))
		api.JSON(w, 200, a.mgmtStatus())
	})
	s.HandleAdmin("POST /api/mgmt/port/revert", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.mgmtRevert("Undone by " + c.User.Username + ".")
		api.JSON(w, 200, a.mgmtStatus())
	})
}

func mgmtSummary(c mgmtPortCfg) string {
	if c.Iface == "" {
		return "removed"
	}
	if c.Mode == "dhcp" {
		return c.Iface + " from DHCP"
	}
	return c.Iface + " at " + c.CIDR
}

// localHost is the address the request came in on.
func localHost(r *http.Request) string {
	if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if h, _, err := net.SplitHostPort(la.String()); err == nil {
			return h
		}
	}
	return ""
}

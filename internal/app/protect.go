package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/fw"
)

// Lock-out protection: the rules that keep this device manageable no matter what else is edited,
// shown in plain words, plus a limit on who may manage and a check that answers "can I still get in?".

const mgmtAllowSetting = "mgmt.allow"

// rescueIface is the port the device was first set up from, if it is still a sensible one.
func (a *App) rescueIface() string {
	return strings.TrimSpace(a.Config.Setting("mgmt.rescue_iface", ""))
}

// limitCovers says whether a person at ip, arriving through iface, can still manage with the limit
// in force. The port the device was set up from and the device itself are never cut off.
func limitCovers(resolved []string, ip, iface, rescue string) bool {
	if len(resolved) == 0 || iface == "" || iface == "lo" || (rescue != "" && iface == rescue) {
		return true
	}
	return inRanges(resolved, ip)
}

func inRanges(ranges []string, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, r := range ranges {
		if p, err := netip.ParsePrefix(r); err == nil && p.Contains(addr) {
			return true
		}
	}
	return false
}

type protectedRule struct {
	Title  string `json:"title"`
	Text   string `json:"text"`
	Active bool   `json:"active"`
	Note   string `json:"note,omitempty"`
}

func joinNames(s []string) string {
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s[0]
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

func portsText(p []int) string {
	var s []string
	for _, n := range p {
		s = append(s, strconv.Itoa(n))
	}
	if len(s) == 1 {
		return "port " + s[0]
	}
	return "ports " + strings.Join(s, " and ")
}

func (a *App) protectedRules(m *fw.Model) []protectedRule {
	var out []protectedRule
	var names, ifaces []string
	ssh := false
	for _, z := range m.Zones {
		if z.Management && len(z.Ifaces) > 0 {
			names = append(names, z.Name)
			ifaces = append(ifaces, z.Ifaces...)
			ssh = ssh || z.SSH
		}
	}
	text := "No management network is set. Mark one network as able to manage this device."
	if len(names) > 0 {
		text = fmt.Sprintf("Devices on %s can always open this console on %s (through %s).", joinNames(names), portsText(m.ConsolePorts), joinNames(ifaces))
	}
	out = append(out, protectedRule{Title: "Always reach this console", Text: text, Active: len(names) > 0,
		Note: "This rule sits above every rule you can edit. It cannot be removed, reordered or switched off."})
	if len(m.MgmtAllow) > 0 {
		out = append(out, protectedRule{Title: "Only these addresses may manage", Text: "Management is limited to " + strings.Join(m.MgmtAllow, ", ") + ". Everyone else on those networks is turned away.", Active: true,
			Note: "Empty the list to allow every management network again."})
	}
	if r := a.rescueIface(); r != "" {
		on, note := true, "This is the last resort. Even if every network were edited wrongly, this port can still reach the console."
		switch {
		case contains(m.WanIfaces(), r):
			on, note = false, "Not in use: this port now faces the internet, and the console is never opened to the internet."
		case contains(ifaces, r):
			on, note = false, "Not needed right now: this port already belongs to a management network. It takes over if that changes."
		}
		out = append(out, protectedRule{Title: "The port you set this up from", Text: fmt.Sprintf("The device was first set up from %s, so devices plugged into %s can always open the console on %s, whatever else changes.", r, r, portsText(m.ConsolePorts)), Active: on, Note: note})
	}
	if ssh {
		out = append(out, protectedRule{Title: "Remote login (SSH)", Text: "Allowed from the networks you marked for remote login.", Active: true})
	}
	out = append(out, protectedRule{Title: "Safe starting rules", Text: "If you are ever locked out, the recovery screen on the machine (Alt+F2) can restore the safe starting rules, reset the console port to 443 and remove any limit on who may manage.", Active: true})
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func (a *App) registerProtectRoutes() {
	s := a.API
	s.Handle("GET /api/fw/protected", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		m, err := fw.Load(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		spec := fw.MgmtAllowFrom(a.Config)
		zs, _ := fw.Zones(a.Config)
		type netChoice struct {
			Name    string   `json:"name"`
			Subnets []string `json:"subnets"`
		}
		var nets []netChoice
		for _, z := range zs {
			if z.Kind == fw.KindWan {
				continue
			}
			nc := netChoice{Name: z.Name, Subnets: []string{}}
			if rows, err := a.Config.Query(`SELECT ipv4_cidr FROM interfaces WHERE enabled=1 AND ipv4_cidr<>'' AND zone_id=?`, z.ID); err == nil {
				for rows.Next() {
					var cidr string
					rows.Scan(&cidr)
					if p, err := netip.ParsePrefix(strings.TrimSpace(cidr)); err == nil {
						nc.Subnets = append(nc.Subnets, p.Masked().String())
					}
				}
				rows.Close()
			}
			if len(nc.Subnets) > 0 {
				nets = append(nets, nc)
			}
		}
		if spec.Networks == nil {
			spec.Networks = []string{}
		}
		if spec.CIDRs == nil {
			spec.CIDRs = []string{}
		}
		api.JSON(w, 200, map[string]any{
			"rules": a.protectedRules(m), "allow": spec, "networks": nets,
			"you": map[string]any{"ip": c.IP, "iface": ifaceOf(c.IP)},
		})
	})

	s.HandleAdmin("PUT /api/fw/management-access", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in fw.MgmtAllowSpec
		if !api.Decode(w, r, &in) {
			return
		}
		clean := fw.MgmtAllowSpec{Networks: []string{}, CIDRs: []string{}}
		for _, n := range in.Networks {
			if n = strings.TrimSpace(n); n != "" {
				clean.Networks = append(clean.Networks, n)
			}
		}
		for _, c := range in.CIDRs {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if p, err := netip.ParsePrefix(c); err == nil {
				clean.CIDRs = append(clean.CIDRs, p.Masked().String())
			} else if ad, err := netip.ParseAddr(c); err == nil {
				clean.CIDRs = append(clean.CIDRs, netip.PrefixFrom(ad, ad.BitLen()).String())
			} else {
				fail(w, fmt.Errorf("%q is not an address range like 192.168.1.0/24 or a single address", c))
				return
			}
		}
		if len(clean.Networks)+len(clean.CIDRs) == 0 {
			a.Config.Exec(`DELETE FROM settings WHERE key=?`, mgmtAllowSetting)
			a.publishFW()
			ok(w)
			return
		}
		raw, _ := json.Marshal(clean)
		old := a.Config.Setting(mgmtAllowSetting, "")
		a.Config.SetSetting(mgmtAllowSetting, string(raw))
		spec := fw.MgmtAllowFrom(a.Config)
		revert := func() {
			if old == "" {
				a.Config.Exec(`DELETE FROM settings WHERE key=?`, mgmtAllowSetting)
			} else {
				a.Config.SetSetting(mgmtAllowSetting, old)
			}
		}
		if len(spec.Resolved) == 0 {
			revert()
			fail(w, errors.New("the networks you picked have no addresses yet, so nothing would be allowed. Pick other networks or add an address range"))
			return
		}
		// Never lock out the person making the change.
		if !limitCovers(spec.Resolved, c.IP, ifaceOf(c.IP), a.rescueIface()) {
			revert()
			fail(w, fmt.Errorf("you are connecting from %s, which is not in this list, so you would lock yourself out. Add it, or pick the network it belongs to", c.IP))
			return
		}
		a.publishFW()
		api.JSON(w, 200, map[string]any{"ok": true, "resolved": spec.Resolved})
	})

	s.Handle("POST /api/fw/access-check", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		m, err := fw.Load(a.Config)
		if err != nil {
			fail(w, err)
			return
		}
		type check struct {
			OK   bool   `json:"ok"`
			Text string `json:"text"`
		}
		var out []check
		add := func(ok bool, f string, args ...any) { out = append(out, check{ok, fmt.Sprintf(f, args...)}) }
		if err := m.Validate(); err != nil {
			add(false, "The saved firewall has a problem: %s", strings.Split(err.Error(), "; ")[0])
		} else {
			add(true, "The saved firewall is valid.")
		}
		mg := 0
		for _, z := range m.Zones {
			if z.Management && len(z.Ifaces) > 0 {
				mg++
			}
		}
		add(mg > 0, "%d network(s) can manage this device.", mg)
		if checked, safe, ifc := sessionSafe(m, c.IP); checked {
			add(safe, "You are connected through %s; it %s manage this device.", ifc, map[bool]string{true: "can", false: "can NOT"}[safe])
		} else {
			add(true, "You are connected from this device itself, so the network cannot affect you.")
		}
		if len(m.MgmtAllow) > 0 {
			in := limitCovers(m.MgmtAllow, c.IP, ifaceOf(c.IP), m.RescueIface)
			add(in, "Management is limited to %s. Your address %s %s.", strings.Join(m.MgmtAllow, ", "), c.IP, map[bool]string{true: "is covered", false: "is NOT covered"}[in])
		} else {
			add(true, "Management is allowed from every management network (no address limit).")
		}
		for _, p := range m.ConsolePorts {
			conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(p), time.Second)
			if err == nil {
				conn.Close()
			}
			add(err == nil || a.Cfg.WebAddr != DefaultConfig().WebAddr, "The console answers on port %d.", p)
		}
		if r := a.rescueIface(); r != "" {
			add(true, "The port you set this up from (%s) is kept as a last-resort way in.", r)
		} else {
			add(true, "No setup port is recorded; the management networks above are the only way in.")
		}
		good := true
		for _, ch := range out {
			good = good && ch.OK
		}
		api.JSON(w, 200, map[string]any{"ok": good, "checks": out})
	})
}

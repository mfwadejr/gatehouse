package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/installer"
	"gatehouse/internal/seed"
)

// First-run setup. A freshly installed device has no account. The first person to open it from
// their own network creates the administrator account, then a short wizard sets up how Gatehouse
// is used on the network. There is no default login anywhere. Until the account exists the
// page only answers devices on private addresses, never the internet side, and once it exists
// the page is closed for good. The machine's own screen says "setup not finished" until
// both steps are done.

const wizardSetting = "setup.wizard" // "done" once the wizard was finished or skipped

func (a *App) setupStatePath() string { return filepath.Join(a.Cfg.DataDir, "setup-state") }

// setupState is "account" (nobody has signed up yet), "wizard" (account made, wizard open) or "ready".
func (a *App) setupState() string {
	switch {
	case !a.Auth.HasUsers():
		return "account"
	case a.wizardPending():
		return "wizard"
	}
	return "ready"
}

// wizardPending is true until the network wizard is finished or skipped. A device that was
// already configured some other way never shows it.
func (a *App) wizardPending() bool {
	if a.Config.Setting(wizardSetting, "") == "done" {
		return false
	}
	var zones, wans int
	a.Config.QueryRow(`SELECT count(*) FROM zones`).Scan(&zones)
	a.Config.QueryRow(`SELECT count(*) FROM wan_links`).Scan(&wans)
	return zones == 0 && wans == 0
}

// writeSetupState tells the machine's own screen how far setup has got.
func (a *App) writeSetupState() {
	os.WriteFile(a.setupStatePath(), []byte(a.setupState()+"\n"), 0o644)
}

func (a *App) prepareSetup() { a.writeSetupState() }

func privateIP(ip net.IP) bool {
	x, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	x = x.Unmap()
	return x.IsPrivate() || x.IsLoopback() || x.IsLinkLocalUnicast() || netip.MustParsePrefix("100.64.0.0/10").Contains(x)
}

func remotePrivate(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	return privateIP(net.ParseIP(h))
}

// localPort finds the network port this very connection arrived on.
func localPort(r *http.Request) (name, addr string) {
	la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return "", ""
	}
	h, _, err := net.SplitHostPort(la.String())
	if err != nil {
		return "", ""
	}
	ip := net.ParseIP(h)
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		as, _ := ifi.Addrs()
		for _, x := range as {
			if n, ok := x.(*net.IPNet); ok && n.IP.Equal(ip) {
				return ifi.Name, ip.String()
			}
		}
	}
	return "", ip.String()
}

func currentZone() string {
	b, err := os.ReadFile("/etc/timezone")
	if err != nil {
		return "UTC"
	}
	return strings.TrimSpace(string(b))
}

func (a *App) registerSetupRoutes() {
	s := a.API
	s.Public("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		api.JSON(w, 200, map[string]bool{"needed": !a.Auth.HasUsers()})
	})
	s.Public("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !api.SameOrigin(r) {
			api.Error(w, http.StatusForbidden, "request rejected")
			return
		}
		if !remotePrivate(r) {
			api.Error(w, http.StatusForbidden, "first-time setup is only available from your own network")
			return
		}
		// The installer asked which port the person would manage Gatehouse from; until the account
		// exists, only that port answers setup, so a cable plugged in somewhere else cannot reach it.
		if mp := installer.MgmtPort(); mp != "" {
			if name, _ := localPort(r); name != "" && name != "lo" && name != mp {
				api.Error(w, http.StatusForbidden, "first-time setup is only available through the management port ("+mp+"); plug the computer into that port")
				return
			}
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		// One at a time: whoever gets here first creates the account, everyone else is told it exists.
		a.setupMu.Lock()
		defer a.setupMu.Unlock()
		if a.Auth.HasUsers() {
			api.Error(w, http.StatusConflict, "this device is already set up")
			return
		}
		if len(in.Password) < 12 {
			api.Error(w, http.StatusBadRequest, "the password must be at least 12 characters")
			return
		}
		if _, err := a.Auth.CreateUser(in.Username, in.Password, "admin"); err != nil {
			api.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		a.writeSetupState()
		// Remember the port this device was first set up from: it stays a last-resort way in.
		if name, _ := localPort(r); name != "" && name != "lo" && a.Config.Setting("mgmt.rescue_iface", "") == "" {
			a.Config.SetSetting("mgmt.rescue_iface", name)
		}
		api.JSON(w, 200, map[string]string{"status": "ok"})
	})

	// The wizard that follows: how Gatehouse is used on the network.
	s.HandleAdmin("GET /api/setup/wizard", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		here, hereAddr := localPort(r)
		var used []netip.Prefix
		ifs, _ := net.Interfaces()
		for _, ifi := range ifs {
			if ifi.Flags&net.FlagLoopback != 0 {
				continue
			}
			as, _ := ifi.Addrs()
			for _, x := range as {
				if n, ok := x.(*net.IPNet); ok && n.IP.To4() != nil {
					if p, err := netip.ParsePrefix(n.String()); err == nil && !p.Addr().IsLinkLocalUnicast() {
						used = append(used, p)
					}
				}
			}
		}
		host, _ := os.Hostname()
		api.JSON(w, 200, map[string]any{
			"pending": a.wizardPending(), "ports": ports(), "here": here, "here_addr": hereAddr,
			"suggested_lan": seed.Suggest(used), "hostname": host, "timezone": currentZone(),
			"timezones": installer.Timezones(),
		})
	})
	s.HandleAdmin("POST /api/setup/wizard", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Skip     bool         `json:"skip"`
			Network  seed.Network `json:"network"`
			Hostname string       `json:"hostname"`
			Timezone string       `json:"timezone"`
			MFA      bool         `json:"require_mfa"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		a.setupMu.Lock()
		defer a.setupMu.Unlock()
		if !a.wizardPending() {
			api.Error(w, http.StatusConflict, "setup is already finished")
			return
		}
		if in.Skip {
			a.Config.SetSetting(wizardSetting, "done")
			a.writeSetupState()
			api.JSON(w, 200, map[string]any{"status": "skipped"})
			return
		}
		if err := in.Network.Validate(); err != nil {
			fail(w, err)
			return
		}
		have := map[string]bool{}
		for _, p := range ports() {
			have[p["name"].(string)] = true
		}
		all := []string{in.Network.LAN.Port}
		if in.Network.WAN != nil {
			all = append(all, in.Network.WAN.Port)
		}
		all = append(all, in.Network.Bridge...)
		for _, p := range all {
			if !have[p] {
				fail(w, errors.New("this machine has no network port called "+p))
				return
			}
		}
		if a.Net != nil {
			if err := a.Net.SetSystem(strings.TrimSpace(in.Hostname), strings.TrimSpace(in.Timezone)); err != nil {
				fail(w, err)
				return
			}
		}
		if err := seed.Apply(a.Config, a.Box, &in.Network); err != nil {
			fail(w, err)
			return
		}
		if in.MFA {
			a.Auth.SetMFARequired(true)
		}
		a.Config.SetSetting(wizardSetting, "done")
		a.writeSetupState()
		// The change moves this very connection when Gatehouse becomes the router, so it is
		// applied a moment after the answer has gone out.
		go func() {
			time.Sleep(2 * time.Second)
			a.firstBoot(context.Background())
		}()
		out := map[string]any{"status": "applying", "mode": in.Network.LAN.Mode}
		switch {
		case in.Network.LAN.Mode == "router":
			out["address"] = strings.Split(in.Network.LAN.Address, "/")[0]
		case in.Network.LAN.Addressing == "static":
			out["address"] = strings.Split(in.Network.LAN.Static, "/")[0]
		}
		api.JSON(w, 200, out)
	})
}

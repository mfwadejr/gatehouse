package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
	"gatehouse/internal/update"
)

// Remote login (SSH) with the console's administrator names and passwords. Off until switched on.
// The password is checked by Gatehouse itself each time (see `gatehouse ssh-auth`), so nothing is
// copied and a changed password applies at once.

const gatehouseBinary = "/usr/local/bin/gatehouse"

func (a *App) sshOn() bool { return a.Config.Setting("ssh.enabled", "0") == "1" }

// adminNames lists the console administrators.
func (a *App) adminNames() []string {
	var out []string
	rows, err := a.Config.Query(`SELECT username FROM users WHERE role='admin' ORDER BY username`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

// sshSync puts the SSH server in line with the setting and the list of administrators.
func (a *App) sshSync() error {
	if a.Net == nil {
		return nil
	}
	return a.Net.SSHApply(netd.SSHPlan{Enabled: a.sshOn(), Users: a.adminNames(), Binary: gatehouseBinary})
}

// sshNetworks lists the networks remote login is reachable from, with the router's address there.
func (a *App) sshNetworks() []map[string]string {
	out := []map[string]string{}
	rows, err := a.Config.Query(`SELECT z.name, COALESCE(i.ipv4_cidr,'') FROM zones z LEFT JOIN interfaces i ON i.zone_id=z.id WHERE z.management=1 AND z.ssh=1 ORDER BY z.name, i.id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var name, cidr string
		rows.Scan(&name, &cidr)
		addr := ""
		if ip, _, err := net.ParseCIDR(cidr); err == nil {
			addr = ip.String()
		}
		out = append(out, map[string]string{"network": name, "address": addr})
	}
	return out
}

func (a *App) registerSSHRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/system/ssh", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		type user struct {
			Name   string `json:"name"`
			Login  string `json:"login"`
			Usable bool   `json:"usable"`
		}
		var us []user
		for _, n := range a.adminNames() {
			l, ok := netd.SSHUserName(n)
			us = append(us, user{Name: n, Login: l, Usable: ok})
		}
		api.JSON(w, 200, map[string]any{"enabled": a.sshOn(), "users": us, "networks": a.sshNetworks(), "last_error": a.Config.Setting("ssh.last_error", "")})
	})
	s.HandleAdmin("PUT /api/system/ssh", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Enabled  bool   `json:"enabled"`
			Code     string `json:"code"`
			Password string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		// Turning remote login on is serious; turning it off never asks.
		if in.Enabled {
			if err := a.serious(c, in.Code, in.Password); err != nil {
				api.Error(w, http.StatusForbidden, err.Error())
				return
			}
		}
		if st := update.ReadState(a.updateState()); st != "idle" {
			api.Error(w, http.StatusConflict, "an update is running ("+st+"); wait for it to finish")
			return
		}
		if in.Enabled {
			// Reachable from the networks that may manage the device, unless a network was set otherwise.
			a.Config.Exec(`UPDATE zones SET ssh=1 WHERE management=1 AND NOT EXISTS (SELECT 1 FROM settings WHERE key='ssh.zones_set')`)
			a.Config.SetSetting("ssh.zones_set", "1")
		}
		a.Config.SetSetting("ssh.enabled", map[bool]string{true: "1", false: "0"}[in.Enabled])
		if err := a.sshSync(); err != nil {
			a.Config.SetSetting("ssh.enabled", "0")
			a.sshSync()
			a.Config.SetSetting("ssh.last_error", err.Error())
			fail(w, fmt.Errorf("remote login could not be turned on: %w", err))
			return
		}
		a.Config.SetSetting("ssh.last_error", "")
		if err := a.refreshFirewallQuiet(c.User.Username); err != nil {
			fail(w, fmt.Errorf("the firewall could not be updated: %w", err))
			return
		}
		a.event(c.User.Username, "warning", map[bool]string{true: "Remote login (SSH) was turned on", false: "Remote login (SSH) was turned off"}[in.Enabled], "")
		a.publishFW()
		ok(w)
	})
	// Used only by `gatehouse ssh-auth`, which sshd runs to check a password. It answers only the
	// machine itself, and only while remote login is on.
	s.Public("POST /api/auth/ssh-verify", func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() || !a.sshOn() {
			api.Error(w, http.StatusForbidden, "not available")
			return
		}
		var in struct {
			User     string `json:"user"`
			Password string `json:"password"`
			From     string `json:"from"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		// The Linux login is the lower-case form of the console name.
		name := ""
		for _, n := range a.adminNames() {
			if l, ok := netd.SSHUserName(n); ok && l == in.User {
				name = n
			}
		}
		if name == "" {
			a.Auth.CheckPassword("-", in.Password, in.From) // same time and counting as a wrong password
			api.Error(w, http.StatusUnauthorized, "no")
			return
		}
		if err := a.Auth.CheckPassword(name, in.Password, in.From); err != nil {
			api.Error(w, http.StatusUnauthorized, "no")
			return
		}
		ok(w)
	})
}

// sshLoop keeps the logins in step with the administrators, and writes sign-ins to the event log.
func (a *App) sshLoop(ctx context.Context) {
	if a.Net == nil {
		return
	}
	last := time.Now().Unix()
	// Whatever a package install left running is put in line with the setting at start.
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}
	if err := a.sshSync(); err != nil && a.sshOn() {
		a.event("ssh", "warning", "Remote login could not be started", err.Error())
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	synced := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !a.sshOn() {
			synced = ""
			continue
		}
		if now := strings.Join(a.adminNames(), ","); now != synced {
			if err := a.sshSync(); err == nil {
				synced = now
			}
		}
		lines, err := a.Net.SSHLog(last)
		last = time.Now().Unix()
		if err != nil || len(lines) == 0 {
			continue
		}
		fails, okN := 0, 0
		for _, l := range lines {
			if strings.Contains(l, "Accepted password for") {
				okN++
				if f := strings.Fields(l); len(f) > 6 {
					a.event("ssh", "info", "Remote login: "+f[6]+" signed in", l[strings.Index(l, "Accepted"):])
				}
			} else {
				fails++
			}
		}
		if fails > 0 {
			a.event("ssh", "warning", "Remote login: "+strconv.Itoa(fails)+" failed attempt"+map[bool]string{true: "", false: "s"}[fails == 1], "Wrong names or passwords were tried over SSH in the last minute.")
		}
		_ = okN
	}
}

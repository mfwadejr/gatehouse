package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
)

// The packet size of an outbound tunnel, and the addresses and services used to measure it and to
// learn the address the internet sees. The helper does the measuring (netd/vpn_size.go); here are
// the two shared lists (which the owner can change), the manual test, and what the tile shows.

const (
	vpnPingSetting = "vpn.ping_addrs"
	vpnExitSetting = "vpn.exit_sources"
	maxTestList    = 8
)

var reTunMTU = regexp.MustCompile(`(?mi)^\s*tun-mtu\s+([0-9]{3,4})\b`)

// profileTunMTU is the packet size a provider's profile asks for, or 0 when it sets none.
func profileTunMTU(profile string) int {
	if m := reTunMTU.FindStringSubmatch(profile); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1280 && n <= 9000 {
			return n
		}
	}
	return 0
}

func (a *App) loadList(q querier, key string) []string {
	var out []string
	// Read through the caller's own connection: inside a change in progress a second one would
	// wait for the first to finish.
	rows, err := q.Query(`SELECT value FROM settings WHERE key=?`, key)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var raw string
	if rows.Next() {
		rows.Scan(&raw)
	}
	if raw != "" {
		json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// vpnTestLists are the lists sent to the helper. Nil means the built-in list.
func (a *App) vpnTestLists(q querier) (ping, exit []string) {
	return a.loadList(q, vpnPingSetting), a.loadList(q, vpnExitSetting)
}

func cleanTestList(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// fillTunnelInfo copies the helper's size and exit-address answers onto a tunnel's tile.
func (a *App) fillTunnelInfo(v *VPNView, t netd.TunnelInfo) {
	if t.ExitIP != "" || t.ExitError != "" {
		v.ExitIP, v.ExitError = t.ExitIP, t.ExitError
	}
	v.PacketSize, v.PacketSource, v.PacketAt, v.PacketNote, v.PacketPassing = t.MTU, t.Source, t.At, t.Note, t.Passing
}

func (a *App) registerVPNSizeRoutes() {
	s := a.API
	s.Handle("GET /api/vpn/test-lists", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		ping, exit := a.vpnTestLists(a.Config)
		custom := len(ping) > 0 || len(exit) > 0
		if len(ping) == 0 {
			ping = netd.DefaultPingAddrs
		}
		if len(exit) == 0 {
			exit = netd.DefaultExitSources
		}
		api.JSON(w, 200, map[string]any{
			"ping": ping, "exit": exit, "custom": custom,
			"default_ping": netd.DefaultPingAddrs, "default_exit": netd.DefaultExitSources, "max": maxTestList,
		})
	})
	s.HandleAdmin("PUT /api/vpn/test-lists", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Ping    []string `json:"ping"`
			Exit    []string `json:"exit"`
			Restore bool     `json:"restore"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Restore {
			in.Ping, in.Exit = nil, nil
		}
		in.Ping, in.Exit = cleanTestList(in.Ping), cleanTestList(in.Exit)
		if !in.Restore && (len(in.Ping) == 0 || len(in.Exit) == 0) {
			fail(w, errors.New("each list needs at least one entry. Use Restore defaults to go back to the built-in lists"))
			return
		}
		if len(in.Ping) > maxTestList || len(in.Exit) > maxTestList {
			fail(w, errors.New("each list can hold at most 8 entries"))
			return
		}
		for _, p := range in.Ping {
			ip := net.ParseIP(p)
			if ip == nil || ip.To4() == nil {
				fail(w, errors.New(p+" is not an IPv4 address. Test addresses are numbers such as 1.1.1.1, because a name would need DNS to work first"))
				return
			}
		}
		for _, e := range in.Exit {
			if !netd.ValidExitSource(e) {
				fail(w, errors.New(e+" is not a service Gatehouse can ask. Use dns:google, dns:cloudflare, or web:https://host/path"))
				return
			}
		}
		save := func(key string, list []string) {
			if len(list) == 0 {
				a.Config.SetSetting(key, "")
				return
			}
			b, _ := json.Marshal(list)
			a.Config.SetSetting(key, string(b))
		}
		save(vpnPingSetting, in.Ping)
		save(vpnExitSetting, in.Exit)
		// The lists only change what the helper measures with, so they go to it directly and
		// never touch a tunnel or a firewall rule.
		if a.Net != nil {
			if vp, err := a.buildVPNPlan(a.Config); err == nil {
				if err := a.Net.ApplyVPN(*vp); err != nil {
					fail(w, errors.New("saved, but the helper could not take the new lists yet: "+err.Error()))
					return
				}
			}
		}
		api.JSON(w, 200, map[string]any{"ok": true})
	})
	// Measure one tunnel now, set its size, and show what each address answered.
	s.HandleAdmin("POST /api/vpn/{kind}/{id}/packet/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		kind, good := kindOf(r)
		id, idOK := pathID(r)
		if !good || !idOK || !a.needHelper(w) {
			if !good || !idOK {
				fail(w, errBadID)
			}
			return
		}
		table := "vpn_wg"
		if kind == "openvpn" {
			table = "vpn_ovpn"
		}
		var dev, role string
		if err := a.Config.QueryRow(`SELECT name,mode FROM `+table+` WHERE id=?`, id).Scan(&dev, &role); err != nil {
			fail(w, errors.New("that VPN no longer exists"))
			return
		}
		if role != "client" {
			fail(w, errors.New("the packet size is measured for connections that go out to a provider or another site, not for the VPN server"))
			return
		}
		rep, err := a.Net.VPNSizeTest(dev)
		if err != nil {
			fail(w, err)
			return
		}
		a.refreshVPN()
		api.JSON(w, 200, rep)
	})
}

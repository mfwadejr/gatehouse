package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/fw"
)

// Blocking mode for threat watching ("Watch and block"). When the engine reports a serious alert and
// the other end is an outside address, that address is put on a firewall block list for a while and
// then taken off by itself. Traffic never passes through the engine, so if the engine stops, nothing
// is blocked and nothing is held up. Your own devices, the addresses your DNS servers use and anything
// that is not an ordinary outside address are never blocked.

const (
	threatBlockSetting    = "threat.block"         // "1" when blocking is switched on
	threatBlockMinSetting = "threat.block_minutes" // how long an address stays blocked
	threatBlocksSetting   = "threat.blocks"        // the list, as JSON
	threatBlockMax        = 500
)

type threatBlockRow struct {
	IP     string `json:"ip"`
	Until  int64  `json:"until"`
	At     int64  `json:"at"`
	SID    int64  `json:"sid"`
	Why    string `json:"why"`
	Device string `json:"device"`
}

var threatBlockMu sync.Mutex

func (a *App) threatBlocking() bool {
	return a.threatMode() == "watch" && a.Config.Setting(threatBlockSetting, "") == "1"
}

func (a *App) threatBlockMinutes() int { return a.threatInt(threatBlockMinSetting, 60, 5, 1440) }

// threatBlockList returns the blocks that have not ended yet. Call with threatBlockMu held.
func (a *App) threatBlockList(now time.Time) []threatBlockRow {
	var rows []threatBlockRow
	json.Unmarshal([]byte(a.Config.Setting(threatBlocksSetting, "")), &rows)
	keep := rows[:0]
	for _, r := range rows {
		if r.Until > now.Unix() {
			keep = append(keep, r)
		}
	}
	return keep
}

func (a *App) threatBlockSave(rows []threatBlockRow) {
	b, _ := json.Marshal(rows)
	a.Config.SetSetting(threatBlocksSetting, string(b))
}

// threatBlockable says whether an address may be blocked: an ordinary outside IPv4 address that is
// not ours and not one of the DNS servers in use.
func (a *App) threatBlockable(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil || p.To4() == nil {
		return false
	}
	if p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast() || p.IsUnspecified() || p.IsMulticast() || p.Equal(net.IPv4bcast) {
		return false
	}
	if _, cg, _ := net.ParseCIDR("100.64.0.0/10"); cg.Contains(p) {
		return false
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, x := range addrs {
			if n, ok := x.(*net.IPNet); ok && n.IP.Equal(p) {
				return false
			}
		}
	}
	if rows, err := a.Config.Query(`SELECT address FROM dns_upstreams`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var ad string
			if rows.Scan(&ad) == nil {
				h, _, err := net.SplitHostPort(ad)
				if err != nil {
					h = ad
				}
				if h == ip {
					return false
				}
			}
		}
	}
	return true
}

// threatMaybeBlock is called for every alert that was kept. Only a serious alert (severity 1) with an
// outside address on the other end leads to a block.
func (a *App) threatMaybeBlock(e evePacket, local, device string, now time.Time) {
	if e.Alert.Severity != 1 || !a.threatBlocking() {
		return
	}
	remote := e.SrcIP
	if remote == local {
		remote = e.DestIP
	}
	if !a.threatBlockable(remote) {
		return
	}
	mins := a.threatBlockMinutes()
	threatBlockMu.Lock()
	rows := a.threatBlockList(now)
	until := now.Add(time.Duration(mins) * time.Minute).Unix()
	found := false
	for i := range rows {
		if rows[i].IP == remote {
			rows[i].Until, found = until, true
		}
	}
	if !found {
		if len(rows) >= threatBlockMax {
			threatBlockMu.Unlock()
			return
		}
		rows = append(rows, threatBlockRow{IP: remote, Until: until, At: now.Unix(), SID: e.Alert.SID, Why: e.Alert.Signature, Device: device})
	}
	a.threatBlockSave(rows)
	threatBlockMu.Unlock()
	if !found {
		a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now.Unix(), "threat", "warning", "system",
			fmt.Sprintf("Blocked %s for %d minutes", remote, mins), e.Alert.Signature+" ("+device+")")
	}
	a.fwSync()
}

// threatBlockFill is what the firewall's block list should hold now.
func (a *App) threatBlockFill(now time.Time) fw.SetFill {
	f := fw.SetFill{Replace: true}
	threatBlockMu.Lock()
	defer threatBlockMu.Unlock()
	for _, r := range a.threatBlockList(now) {
		left := r.Until - now.Unix()
		if left < 1 {
			continue
		}
		if left > 9999999 {
			left = 9999999
		}
		f.Elems = append(f.Elems, r.IP+" timeout "+strconv.FormatInt(left, 10)+"s")
	}
	return f
}

func (a *App) registerThreatBlockRoutes() {
	s := a.API
	s.HandleAdmin("GET /api/threat/blocks", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		now := time.Now()
		threatBlockMu.Lock()
		rows := a.threatBlockList(now)
		threatBlockMu.Unlock()
		sort.Slice(rows, func(i, j int) bool { return rows[i].At > rows[j].At })
		type out struct {
			threatBlockRow
			Left int64 `json:"left"`
		}
		var list []out
		for _, x := range rows {
			list = append(list, out{x, x.Until - now.Unix()})
		}
		api.JSON(w, 200, map[string]any{"on": a.threatBlocking(), "minutes": a.threatBlockMinutes(), "blocks": list})
	})
	s.HandleAdmin("DELETE /api/threat/blocks/{ip}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		ip := r.PathValue("ip")
		threatBlockMu.Lock()
		rows := a.threatBlockList(time.Now())
		keep := rows[:0]
		for _, x := range rows {
			if x.IP != ip {
				keep = append(keep, x)
			}
		}
		a.threatBlockSave(keep)
		threatBlockMu.Unlock()
		a.fwSync()
		a.event(c.User.Username, "info", "Unblocked "+ip, "It was blocked by threat watching.")
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/blocks/clear", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		threatBlockMu.Lock()
		a.threatBlockSave(nil)
		threatBlockMu.Unlock()
		a.fwSync()
		a.event(c.User.Username, "info", "Removed every block made by threat watching", "")
		ok(w)
	})
	s.HandleAdmin("PUT /api/threat/block-time", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Minutes int `json:"minutes"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Minutes < 5 || in.Minutes > 1440 {
			fail(w, errors.New("choose between 5 minutes and 24 hours (1440 minutes)"))
			return
		}
		a.Config.SetSetting(threatBlockMinSetting, strconv.Itoa(in.Minutes))
		ok(w)
	})
}

func (a *App) threatBlockingText() string {
	if a.threatBlocking() {
		return fmt.Sprintf("Blocking is on. When a serious alert comes from an outside address, that address is blocked for %d minutes and then released by itself. Your own devices and your DNS servers are never blocked.", a.threatBlockMinutes())
	}
	return "Blocking is not switched on. NopeWarden watches and tells you; it never holds up your traffic."
}

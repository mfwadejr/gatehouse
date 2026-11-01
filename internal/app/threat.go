package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
)

// Threat protection. A detection engine (Suricata) writes what it notices to a log file; Gatehouse
// reads that file, drops what the person has let through, and turns the rest into plain-language
// alerts in the Logs. It watches and tells; it does not block traffic yet.
//
// It fails open by construction: Gatehouse only reads the engine's log. If the engine is missing,
// stopped or crashes, or this reader fails, traffic is not touched in any way.

const (
	threatModeSetting = "threat.mode" // off, watch
	threatEveSetting  = "threat.eve"  // Advanced: where the engine's log file is
	defaultEve        = "/var/log/suricata/eve.json"
	noisyPerWindow    = 50 // the same check this many times in ten minutes is silenced for a week
)

var errBadPath = errors.New("enter a full path that starts with /")

type threatState struct {
	mu     sync.Mutex
	recent map[int64][]int64 // sid -> alert times in the last window
}

func (a *App) threatMode() string {
	if a.Config.Setting(threatModeSetting, "watch") == "off" {
		return "off"
	}
	return "watch"
}

func (a *App) threatEvePath() string {
	if p := strings.TrimSpace(a.Config.Setting(threatEveSetting, "")); p != "" {
		return p
	}
	return defaultEve
}

// plainThreat says what an engine category means in everyday words.
func plainThreat(category, sig string) string {
	c := strings.ToLower(category)
	switch {
	case strings.Contains(c, "trojan"), strings.Contains(c, "malware"), strings.Contains(c, "command and control"), strings.Contains(c, "c2"):
		return "A device may be infected and talking to a known bad server."
	case strings.Contains(c, "scan"), strings.Contains(c, "reconnaissance"):
		return "Someone is probing your network for open doors."
	case strings.Contains(c, "exploit"), strings.Contains(c, "attack"), strings.Contains(c, "shellcode"), strings.Contains(c, "web application"):
		return "Someone tried to break into a device."
	case strings.Contains(c, "phish"), strings.Contains(c, "social"):
		return "A device went to a site that tries to trick people."
	case strings.Contains(c, "denial"), strings.Contains(c, "dos"):
		return "Traffic that looks like an attempt to overwhelm a device."
	case strings.Contains(c, "policy"), strings.Contains(c, "p2p"), strings.Contains(c, "misc"), strings.Contains(c, "info"):
		return "Unusual traffic that is usually harmless. Allow it if you know why it happens."
	}
	return "Something that looks suspicious: " + sig
}

type evePacket struct {
	EventType string `json:"event_type"`
	SrcIP     string `json:"src_ip"`
	DestIP    string `json:"dest_ip"`
	DestPort  int    `json:"dest_port"`
	Proto     string `json:"proto"`
	Alert     struct {
		SID       int64  `json:"signature_id"`
		Signature string `json:"signature"`
		Category  string `json:"category"`
		Severity  int    `json:"severity"`
	} `json:"alert"`
}

func privateAddr(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && (p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast())
}

// threatIngest handles one line of the engine's log. It returns true when an alert was kept.
func (a *App) threatIngest(line []byte, now time.Time) bool {
	if a.threatMode() == "off" {
		return false
	}
	var e evePacket
	if json.Unmarshal(line, &e) != nil || e.EventType != "alert" || e.Alert.SID == 0 {
		return false
	}
	// The device is whichever end is one of ours.
	local := e.SrcIP
	if !privateAddr(local) {
		local = e.DestIP
	}
	mac := ""
	if privateAddr(local) {
		mac = strings.ToLower(a.DHCP.Lookups().MACFor(local))
	}
	var n int
	a.Config.QueryRow(`SELECT COUNT(*) FROM threat_trusted WHERE mac=? AND mac<>''`, mac).Scan(&n)
	if n > 0 {
		return false
	}
	a.Config.QueryRow(`SELECT COUNT(*) FROM threat_allowed WHERE sid=? AND (mac='' OR mac=?) AND (until=0 OR until>?)`, e.Alert.SID, mac, now.Unix()).Scan(&n)
	if n > 0 {
		return false
	}
	// A check that keeps firing is silenced for a week, and the person is told once.
	st := &a.threats
	st.mu.Lock()
	if st.recent == nil {
		st.recent = map[int64][]int64{}
	}
	cut := now.Unix() - 600
	keep := st.recent[e.Alert.SID][:0]
	for _, t := range st.recent[e.Alert.SID] {
		if t >= cut {
			keep = append(keep, t)
		}
	}
	keep = append(keep, now.Unix())
	st.recent[e.Alert.SID] = keep
	noisy := len(keep) > noisyPerWindow
	if noisy {
		delete(st.recent, e.Alert.SID)
	}
	st.mu.Unlock()
	if noisy {
		a.Config.Exec(`INSERT INTO threat_allowed(sid,mac,until,note,auto) VALUES(?,?,?,?,1)`, e.Alert.SID, "", now.Add(7*24*time.Hour).Unix(), "Fired too often, so Gatehouse quieted it for a week")
		a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now.Unix(), "threat", "info", "system",
			"Threat protection quieted a noisy check", e.Alert.Signature+" fired too often. You can see it under Firewall, Threat protection.")
		return false
	}
	device := local
	if mac != "" {
		device = a.fwdDeviceName(mac, local)
	}
	plain := plainThreat(e.Alert.Category, e.Alert.Signature)
	a.Logs.Exec(`INSERT INTO threat_alerts(ts,sid,signature,plain,category,severity,src,dst,dport,proto,mac,device) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		now.Unix(), e.Alert.SID, e.Alert.Signature, plain, e.Alert.Category, e.Alert.Severity, e.SrcIP, e.DestIP, e.DestPort, strings.ToLower(e.Proto), mac, device)
	sev := "warning"
	if e.Alert.Severity >= 3 {
		sev = "info"
	}
	a.Logs.Exec(`INSERT INTO events(ts,area,severity,actor,title,detail) VALUES(?,?,?,?,?,?)`, now.Unix(), "threat", sev, device, plain, e.Alert.Signature+" ("+e.SrcIP+" to "+e.DestIP+")")
	return true
}

// threatLoop follows the engine's log file. It never writes to it and never fails the program.
func (a *App) threatLoop(ctx context.Context) {
	defer func() { recover() }()
	var off int64 = -1
	var path string
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
		p := a.threatEvePath()
		fi, err := os.Stat(p)
		if err != nil {
			off = -1
			continue
		}
		if p != path || off < 0 {
			// Start at the end: only what happens from now on is news.
			path, off = p, fi.Size()
			continue
		}
		if fi.Size() < off {
			off = 0 // the log was rotated
		}
		if fi.Size() == off {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		f.Seek(off, 0)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		read := off
		for sc.Scan() {
			read += int64(len(sc.Bytes())) + 1
			a.threatIngest(sc.Bytes(), time.Now())
		}
		f.Close()
		off = read
	}
}

func (a *App) threatEngine() map[string]any {
	p := a.threatEvePath()
	fi, err := os.Stat(p)
	switch {
	case err != nil:
		return map[string]any{"state": "missing", "text": "The detection engine is not installed or not running on this machine, so there is nothing to watch yet."}
	case time.Since(fi.ModTime()) > 10*time.Minute:
		return map[string]any{"state": "quiet", "text": "The engine has not written anything for a while. That is normal on a quiet network."}
	}
	return map[string]any{"state": "running", "text": "The engine is running and Gatehouse is reading what it sees."}
}

func (a *App) registerThreatRoutes() {
	s := a.API
	s.Handle("GET /api/threat", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		type alert struct {
			ID        int64  `json:"id"`
			TS        int64  `json:"ts"`
			SID       int64  `json:"sid"`
			Signature string `json:"signature"`
			Plain     string `json:"plain"`
			Device    string `json:"device"`
			MAC       string `json:"mac"`
			Src       string `json:"src"`
			Dst       string `json:"dst"`
			Severity  int    `json:"severity"`
		}
		alerts := []alert{}
		if rows, err := a.Logs.Query(`SELECT id,ts,sid,signature,plain,device,mac,src,dst,severity FROM threat_alerts ORDER BY id DESC LIMIT 50`); err == nil {
			for rows.Next() {
				var x alert
				rows.Scan(&x.ID, &x.TS, &x.SID, &x.Signature, &x.Plain, &x.Device, &x.MAC, &x.Src, &x.Dst, &x.Severity)
				alerts = append(alerts, x)
			}
			rows.Close()
		}
		type allowed struct {
			ID     int64  `json:"id"`
			SID    int64  `json:"sid"`
			MAC    string `json:"mac"`
			Device string `json:"device"`
			Until  int64  `json:"until"`
			Note   string `json:"note"`
			Auto   bool   `json:"auto"`
		}
		al := []allowed{}
		if rows, err := a.Config.Query(`SELECT id,sid,mac,until,note,auto FROM threat_allowed ORDER BY id DESC`); err == nil {
			for rows.Next() {
				var x allowed
				var au int
				rows.Scan(&x.ID, &x.SID, &x.MAC, &x.Until, &x.Note, &au)
				x.Auto = au == 1
				if x.MAC != "" {
					x.Device = a.fwdDeviceName(x.MAC, "")
				}
				al = append(al, x)
			}
			rows.Close()
		}
		type trusted struct {
			MAC    string `json:"mac"`
			Device string `json:"device"`
		}
		tr := []trusted{}
		if rows, err := a.Config.Query(`SELECT mac FROM threat_trusted ORDER BY mac`); err == nil {
			for rows.Next() {
				var m string
				rows.Scan(&m)
				tr = append(tr, trusted{m, a.fwdDeviceName(m, "")})
			}
			rows.Close()
		}
		api.JSON(w, 200, map[string]any{
			"mode": a.threatMode(), "engine": a.threatEngine(), "eve_path": a.threatEvePath(), "default_eve": defaultEve,
			"alerts": alerts, "allowed": al, "trusted": tr,
			"blocking":  "Blocking is not switched on yet. Gatehouse watches and tells you.",
			"fail_open": "If the engine stops or fails, your traffic is not touched. Gatehouse only reads what the engine notices.",
		})
	})
	s.HandleAdmin("PUT /api/threat/settings", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Mode string `json:"mode"`
			Eve  string `json:"eve_path"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Mode != "off" && in.Mode != "watch" {
			in.Mode = "watch"
		}
		in.Eve = strings.TrimSpace(in.Eve)
		if in.Eve != "" && (!strings.HasPrefix(in.Eve, "/") || strings.Contains(in.Eve, "..")) {
			fail(w, errBadPath)
			return
		}
		a.Config.SetSetting(threatModeSetting, in.Mode)
		a.Config.SetSetting(threatEveSetting, in.Eve)
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/allow", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			SID  int64  `json:"sid"`
			MAC  string `json:"mac"`
			Days int    `json:"days"` // 0: until removed
			Note string `json:"note"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.SID <= 0 {
			fail(w, errBadID)
			return
		}
		until := int64(0)
		if in.Days > 0 {
			until = time.Now().Add(time.Duration(in.Days) * 24 * time.Hour).Unix()
		}
		a.Config.Exec(`INSERT INTO threat_allowed(sid,mac,until,note) VALUES(?,?,?,?)`, in.SID, strings.ToLower(in.MAC), until, in.Note)
		ok(w)
	})
	s.HandleAdmin("DELETE /api/threat/allow/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		a.Config.Exec(`DELETE FROM threat_allowed WHERE id=?`, id)
		ok(w)
	})
	s.HandleAdmin("POST /api/threat/trust", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			MAC string `json:"mac"`
		}
		if !api.Decode(w, r, &in) || in.MAC == "" {
			return
		}
		a.Config.Exec(`INSERT OR IGNORE INTO threat_trusted(mac) VALUES(?)`, strings.ToLower(in.MAC))
		ok(w)
	})
	s.HandleAdmin("DELETE /api/threat/trust/{mac}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.Config.Exec(`DELETE FROM threat_trusted WHERE mac=?`, strings.ToLower(r.PathValue("mac")))
		ok(w)
	})
}

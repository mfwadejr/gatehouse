package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/speedtest"
)

// Speed tests for each internet connection: on demand, and optionally once a day at a quiet hour.
// Every test goes out through the one connection being measured, even a standby one. Results are
// kept for a year so a slow patch can be compared with the plan.

const speedKeep = 365 * 24 * time.Hour

type speedLink struct {
	DownMbps float64 `json:"down_mbps"` // what the plan promises; 0 when not set
	UpMbps   float64 `json:"up_mbps"`
	Metered  *bool   `json:"metered,omitempty"` // has a data limit; unset means "guess from the kind of port"
}

// speedServer is a test server the person can choose. Cloudflare's is built in and cannot be removed.
type speedServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type speedConfig struct {
	Server   string               `json:"server"`  // the default server's address; empty means Cloudflare
	Servers  []speedServer        `json:"servers"` // ones the person added
	Schedule bool                 `json:"schedule"`
	Hour     int                  `json:"hour"` // hour of the day the scheduled test starts
	Links    map[string]speedLink `json:"links"`
}

type speedRun struct {
	mu      sync.Mutex
	running bool
	link    string
	since   time.Time
	last    map[string]string // link -> last error, empty when the last run worked
}

func (a *App) speedConfig() speedConfig {
	c := speedConfig{Hour: 3, Links: map[string]speedLink{}}
	if raw := a.setting("speedtest.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.Links == nil {
		c.Links = map[string]speedLink{}
	}
	// A server typed in by an older version becomes an entry in the list.
	if c.Server != "" && c.Server != speedtest.DefaultServer {
		found := false
		for _, v := range c.Servers {
			found = found || v.URL == c.Server
		}
		if !found {
			c.Servers = append(c.Servers, speedServer{Name: strings.TrimPrefix(c.Server, "https://"), URL: c.Server})
		}
	}
	if c.Hour < 0 || c.Hour > 23 {
		c.Hour = 3
	}
	return c
}

// isMetered says whether a connection probably has a data limit: the person's own answer when
// there is one, otherwise yes for a wireless card or a USB phone, which is how a hotspot appears.
func (a *App) isMetered(name, iface string, c speedConfig) bool {
	if l, ok := c.Links[name]; ok && l.Metered != nil {
		return *l.Metered
	}
	if iface == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", iface, "wireless")); err == nil {
		return true
	}
	if real, err := filepath.EvalSymlinks(filepath.Join("/sys/class/net", iface)); err == nil && strings.Contains(real, "/usb") {
		return true
	}
	return false
}

type speedRow struct {
	ID       int64   `json:"id"`
	TS       int64   `json:"ts"`
	Link     string  `json:"link"`
	Source   string  `json:"source"`
	Quick    bool    `json:"quick"`
	Server   string  `json:"server"`
	DownBps  float64 `json:"down_bps"`
	UpBps    float64 `json:"up_bps"`
	DelayMS  float64 `json:"delay_ms"`
	JitterMS float64 `json:"jitter_ms"`
	LoadedMS float64 `json:"loaded_ms"`
	Grade    string  `json:"grade"`
	Bytes    int64   `json:"bytes"`
	Error    string  `json:"error,omitempty"`
}

func (a *App) speedHistory(link string, since int64) []speedRow {
	q := `SELECT id,ts,link,source,quick,server,down_bps,up_bps,delay_ms,jitter_ms,loaded_ms,grade,bytes,error FROM speedtests WHERE ts>=?`
	args := []any{since}
	if link != "" {
		q += ` AND link=?`
		args = append(args, link)
	}
	q += ` ORDER BY ts DESC LIMIT 2000`
	out := []speedRow{}
	rows, err := a.Logs.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r speedRow
		var quick int
		rows.Scan(&r.ID, &r.TS, &r.Link, &r.Source, &quick, &r.Server, &r.DownBps, &r.UpBps, &r.DelayMS, &r.JitterMS, &r.LoadedMS, &r.Grade, &r.Bytes, &r.Error)
		r.Quick = quick == 1
		out = append(out, r)
	}
	return out
}

var errSpeedBusy = errors.New("another speed test is running. Wait for it to finish")

// runSpeed measures one connection and saves the result. It blocks for about twenty seconds.
func (a *App) runSpeed(link, source string, full bool) (speedRow, error) {
	return a.runSpeedOn(link, source, full, "")
}

// runSpeedOn is runSpeed against a chosen server; empty means the default one.
func (a *App) runSpeedOn(link, source string, full bool, server string) (speedRow, error) {
	if a.Net == nil {
		return speedRow{}, errors.New("speed tests are not available on this machine")
	}
	devs := a.wanDevices()
	dev, ok := devs[link]
	if !ok {
		return speedRow{}, errors.New("that internet connection is not switched on")
	}
	cfg := a.speedConfig()
	iface := ""
	if wans, err := a.listWans(a.Config); err == nil {
		for _, w := range wans {
			if w.Name == link {
				iface = w.Iface
			}
		}
	}
	quick := a.isMetered(link, iface, cfg) && !full
	a.speed.mu.Lock()
	if a.speed.running {
		a.speed.mu.Unlock()
		return speedRow{}, errSpeedBusy
	}
	a.speed.running, a.speed.link, a.speed.since = true, link, time.Now()
	a.speed.mu.Unlock()
	a.Bus.Publish("speedtest", map[string]any{"running": true, "link": link})
	defer func() {
		a.speed.mu.Lock()
		a.speed.running, a.speed.link = false, ""
		a.speed.mu.Unlock()
		a.Bus.Publish("speedtest", map[string]any{"running": false})
	}()
	if server == "" {
		server = cfg.Server
	}
	res, err := a.Net.SpeedTest(speedtest.Params{Dev: dev, Server: server, Quick: quick})
	row := speedRow{TS: time.Now().Unix(), Link: link, Source: source, Quick: quick, Server: res.Server,
		DownBps: res.DownBps, UpBps: res.UpBps, DelayMS: res.DelayMS, JitterMS: res.JitterMS, LoadedMS: res.LoadedMS, Grade: res.Grade, Bytes: res.BytesUsed}
	if err != nil {
		row.Error = err.Error()
	}
	if r, e := a.Logs.Exec(`INSERT INTO speedtests(ts,link,source,quick,server,down_bps,up_bps,delay_ms,jitter_ms,loaded_ms,grade,bytes,error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.TS, row.Link, row.Source, b2i(row.Quick), row.Server, row.DownBps, row.UpBps, row.DelayMS, row.JitterMS, row.LoadedMS, row.Grade, row.Bytes, row.Error); e == nil {
		row.ID, _ = r.LastInsertId()
	}
	a.speed.mu.Lock()
	if a.speed.last == nil {
		a.speed.last = map[string]string{}
	}
	a.speed.last[link] = row.Error
	a.speed.mu.Unlock()
	if err != nil {
		return row, err
	}
	return row, nil
}

// speedLoop runs the daily test. It looks once a minute, starts at the chosen hour, and does each
// connection that is not metered, one after another. A day is never run twice.
func (a *App) speedLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			cfg := a.speedConfig()
			if !cfg.Schedule || now.Hour() != cfg.Hour {
				continue
			}
			day := now.Format("2006-01-02")
			if a.setting("speedtest.last_day", "") == day {
				continue
			}
			a.Config.SetSetting("speedtest.last_day", day)
			wans, err := a.listWans(a.Config)
			if err != nil {
				continue
			}
			for _, w := range wans {
				if !w.Enabled || a.isMetered(w.Name, w.Iface, cfg) {
					continue
				}
				if ctx.Err() != nil {
					return
				}
				if _, err := a.runSpeed(w.Name, "scheduled", false); errors.Is(err, errSpeedBusy) {
					time.Sleep(30 * time.Second)
				}
			}
		}
	}
}

// setSpeedConfigForTest stores a configuration the way the settings route does.
func (a *App) setSpeedConfigForTest(c speedConfig) {
	b, _ := json.Marshal(c)
	a.Config.SetSetting("speedtest.config", string(b))
}

func (a *App) pruneSpeed() {
	a.Logs.Exec(`DELETE FROM speedtests WHERE ts<?`, time.Now().Add(-speedKeep).Unix())
}

func (a *App) registerSpeedRoutes() {
	s := a.API
	s.Handle("GET /api/speedtest", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		cfg := a.speedConfig()
		type linkView struct {
			Name     string  `json:"name"`
			Iface    string  `json:"iface"`
			Metered  bool    `json:"metered"`
			DownMbps float64 `json:"down_mbps"`
			UpMbps   float64 `json:"up_mbps"`
			LastErr  string  `json:"last_error,omitempty"`
		}
		links := []linkView{}
		if wans, err := a.listWans(a.Config); err == nil {
			a.speed.mu.Lock()
			for _, wn := range wans {
				if !wn.Enabled {
					continue
				}
				l := cfg.Links[wn.Name]
				links = append(links, linkView{Name: wn.Name, Iface: wn.Iface, Metered: a.isMetered(wn.Name, wn.Iface, cfg), DownMbps: l.DownMbps, UpMbps: l.UpMbps, LastErr: a.speed.last[wn.Name]})
			}
			a.speed.mu.Unlock()
		}
		a.speed.mu.Lock()
		run := map[string]any{"running": a.speed.running, "link": a.speed.link, "since": a.speed.since.Unix()}
		a.speed.mu.Unlock()
		server := cfg.Server
		if server == "" {
			server = speedtest.DefaultServer
		}
		api.JSON(w, 200, map[string]any{"server": server, "default_server": speedtest.DefaultServer, "servers": append([]speedServer{{Name: "Cloudflare", URL: speedtest.DefaultServer}}, cfg.Servers...), "schedule": cfg.Schedule, "hour": cfg.Hour, "links": links, "run": run})
	})
	s.Handle("GET /api/speedtest/history", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if days <= 0 || days > 365 {
			days = 30
		}
		api.JSON(w, 200, a.speedHistory(r.URL.Query().Get("link"), time.Now().Add(-time.Duration(days)*24*time.Hour).Unix()))
	})
	s.HandleAdmin("PUT /api/speedtest", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Server   string `json:"server"` // older screens still send it; the default is changed by its own call
			Schedule bool   `json:"schedule"`
			Hour     int    `json:"hour"`
			Links    map[string]struct {
				DownMbps float64 `json:"down_mbps"`
				UpMbps   float64 `json:"up_mbps"`
				Metered  bool    `json:"metered"`
			} `json:"links"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Hour < 0 || in.Hour > 23 {
			fail(w, errors.New("choose an hour from 0 to 23"))
			return
		}
		old := a.speedConfig()
		cfg := speedConfig{Server: old.Server, Servers: old.Servers, Schedule: in.Schedule, Hour: in.Hour, Links: map[string]speedLink{}}
		for n, l := range in.Links {
			if l.DownMbps < 0 || l.UpMbps < 0 || l.DownMbps > 100000 || l.UpMbps > 100000 {
				fail(w, errors.New("the plan speed must be between 0 and 100000 Mbps"))
				return
			}
			m := l.Metered
			cfg.Links[n] = speedLink{DownMbps: l.DownMbps, UpMbps: l.UpMbps, Metered: &m}
		}
		b, _ := json.Marshal(cfg)
		if err := a.Config.SetSetting("speedtest.config", string(b)); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	// The list of test servers: add one, remove one, choose the default.
	s.HandleAdmin("POST /api/speedtest/servers", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in speedServer
		if !api.Decode(w, r, &in) {
			return
		}
		u, err := cleanSpeedServer(in.URL)
		if err != nil {
			fail(w, err)
			return
		}
		in.URL = u
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			in.Name = strings.TrimPrefix(u, "https://")
		}
		if len(in.Name) > 40 {
			fail(w, errors.New("the name can be up to 40 characters"))
			return
		}
		cfg := a.speedConfig()
		if len(cfg.Servers) >= 10 {
			fail(w, errors.New("you can keep up to 10 test servers; remove one first"))
			return
		}
		if u == speedtest.DefaultServer {
			fail(w, errors.New("Cloudflare is already in the list"))
			return
		}
		for _, v := range cfg.Servers {
			if v.URL == u {
				fail(w, errors.New("that server is already in the list"))
				return
			}
		}
		// Make sure it answers the way a test server must before it is kept.
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := speedtest.Check(ctx, u); err != nil {
			fail(w, fmt.Errorf("%s did not answer as a test server: %v", u, err))
			return
		}
		cfg.Servers = append(cfg.Servers, in)
		a.saveSpeedConfig(cfg)
		ok(w)
	})
	s.HandleAdmin("DELETE /api/speedtest/servers", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		u := r.URL.Query().Get("url")
		cfg := a.speedConfig()
		keep := cfg.Servers[:0]
		for _, v := range cfg.Servers {
			if v.URL != u {
				keep = append(keep, v)
			}
		}
		cfg.Servers = keep
		if cfg.Server == u {
			cfg.Server = "" // the default falls back to Cloudflare
		}
		a.saveSpeedConfig(cfg)
		ok(w)
	})
	s.HandleAdmin("PUT /api/speedtest/default", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			URL string `json:"url"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		cfg := a.speedConfig()
		known := in.URL == speedtest.DefaultServer || in.URL == ""
		for _, v := range cfg.Servers {
			known = known || v.URL == in.URL
		}
		if !known {
			fail(w, errors.New("that test server is not in your list"))
			return
		}
		cfg.Server = in.URL
		if cfg.Server == speedtest.DefaultServer {
			cfg.Server = ""
		}
		a.saveSpeedConfig(cfg)
		ok(w)
	})
	s.HandleAdmin("POST /api/speedtest/run", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Link   string `json:"link"`
			Full   bool   `json:"full"`
			Server string `json:"server"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		a.speed.mu.Lock()
		busy := a.speed.running
		a.speed.mu.Unlock()
		if busy {
			fail(w, errSpeedBusy)
			return
		}
		if _, ok := a.wanDevices()[in.Link]; !ok {
			fail(w, errors.New("that internet connection is not switched on"))
			return
		}
		if in.Server != "" {
			known := in.Server == speedtest.DefaultServer
			for _, v := range a.speedConfig().Servers {
				known = known || v.URL == in.Server
			}
			if !known {
				fail(w, errors.New("that test server is not in your list"))
				return
			}
		}
		go a.runSpeedOn(in.Link, "manual", in.Full, in.Server)
		api.JSON(w, 202, map[string]string{"status": "started"})
	})
}

func (a *App) saveSpeedConfig(cfg speedConfig) {
	b, _ := json.Marshal(cfg)
	a.Config.SetSetting("speedtest.config", string(b))
}

// cleanSpeedServer accepts an https address and returns its base form.
func cleanSpeedServer(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("the test server address must look like https://speed.example.com")
	}
	return "https://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

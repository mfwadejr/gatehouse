package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/lcdproto"
	"nopewarden/internal/netd"
)

// The front panel display. LCDproc's LCDd drives the hardware; NopeWarden draws on it through LCDd's
// local port. The person chooses the driver and the screens, tests the display, and sets what each
// button does. The test never changes what is saved.

const (
	lcdAddr    = "127.0.0.1:13666"
	lcdSetting = "lcd.config"
)

// lcdScreens are the things the panel can show, in the order they are offered.
var lcdScreens = []struct{ ID, Title, Text string }{
	{"name", "Name and version", "NopeWarden, its version and this machine's name"},
	{"internet", "Internet speed", "Each internet connection's download and upload"},
	{"networks", "Each network", "A network's address and speed, one network at a time"},
	{"dhcp", "Addresses in use", "How many DHCP addresses are in use"},
	{"system", "This machine", "Processor load, memory and how long it has been running"},
	{"alerts", "Alerts", "Warnings and threat alerts in the last day"},
}

// lcdActions are what a button can do.
var lcdActions = []struct{ ID, Title string }{
	{"none", "Nothing"},
	{"next", "Next screen"},
	{"prev", "Previous screen"},
	{"hold", "Hold this screen (press again to carry on)"},
	{"backlight", "Backlight on or off"},
	{"flash", "Flash the backlight"},
	{"alerts", "Show the alerts screen"},
	{"ip", "Show the networks screen"},
}

type lcdConfig struct {
	Enabled  bool              `json:"enabled"`
	Driver   string            `json:"driver"`
	Settings map[string]string `json:"settings"`
	Screens  []string          `json:"screens"`
	Rotate   int               `json:"rotate"`    // seconds on each screen
	DimAfter int               `json:"dim_after"` // minutes with no button press before the backlight goes off; 0 never
	Warn     bool              `json:"warn"`      // jump to the alerts screen when a new alert arrives
	Buttons  map[string]string `json:"buttons"`   // key name -> action
}

func defaultLCD() lcdConfig {
	return lcdConfig{
		Settings: map[string]string{},
		Screens:  []string{"name", "internet", "networks", "dhcp", "system"},
		Rotate:   4,
		Buttons:  map[string]string{"Up": "prev", "Down": "next", "Left": "prev", "Right": "next", "Enter": "hold", "Escape": "backlight"},
	}
}

func (a *App) lcdConfig() lcdConfig {
	c := defaultLCD()
	if raw := a.Config.Setting(lcdSetting, ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.Settings == nil {
		c.Settings = map[string]string{}
	}
	if c.Buttons == nil {
		c.Buttons = map[string]string{}
	}
	if c.Rotate < 2 || c.Rotate > 60 {
		c.Rotate = 4
	}
	return c
}

func (c lcdConfig) plan() netd.LCDPlan {
	return netd.LCDPlan{Enabled: c.Enabled, Driver: c.Driver, Settings: c.Settings}
}

// Check says what is wrong with a display setting.
func (c lcdConfig) check() error {
	if err := c.plan().Check(); err != nil {
		return err
	}
	valid := map[string]bool{}
	for _, s := range lcdScreens {
		valid[s.ID] = true
	}
	for _, s := range c.Screens {
		if !valid[s] {
			return errors.New("not a screen the display can show: " + s)
		}
	}
	if c.Enabled && len(c.Screens) == 0 {
		return errors.New("choose at least one screen to show")
	}
	acts := map[string]bool{}
	for _, x := range lcdActions {
		acts[x.ID] = true
	}
	for k, v := range c.Buttons {
		if !acts[v] {
			return errors.New("not something a button can do: " + v)
		}
		if len(k) == 0 || len(k) > 16 || strings.ContainsAny(k, " {}\n") {
			return errors.New("not a button name: " + k)
		}
	}
	if c.Rotate < 2 || c.Rotate > 60 {
		return errors.New("each screen can stay for 2 to 60 seconds")
	}
	if c.DimAfter < 0 || c.DimAfter > 1440 {
		return errors.New("the backlight can go off after 0 to 1440 minutes")
	}
	return nil
}

// lcdMirror is what the panel shows now, for the console's picture of it.
type lcdMirror struct {
	Lines     []string `json:"lines"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Screen    string   `json:"screen"`
	Backlight bool     `json:"backlight"`
	Hold      bool     `json:"hold"`
	Connected bool     `json:"connected"`
	Error     string   `json:"error,omitempty"`
	At        int64    `json:"at"`
}

type lcdKeyEvent struct {
	N   int    `json:"n"`
	Key string `json:"key"`
	At  int64  `json:"at"`
}

type lcdManager struct {
	mu      sync.Mutex
	mirror  lcdMirror
	test    *lcdTest
	poke    chan struct{}
	changed bool
}

// lcdTest is one test of a display: the draft settings are applied, a connection is held, and the
// buttons pressed are collected so the console can show them as they happen.
type lcdTest struct {
	conn     *lcdproto.Conn
	keys     []lcdKeyEvent
	n        int
	lastUsed time.Time
	done     chan struct{}
}

func (m *lcdManager) setMirror(f func(*lcdMirror)) {
	m.mu.Lock()
	f(&m.mirror)
	m.mirror.At = time.Now().Unix()
	m.mu.Unlock()
}

var lcdKeyNames = []string{"Up", "Down", "Left", "Right", "Enter", "Escape", "A", "B", "C", "D", "E", "F"}

// ---- the running display ----

func (a *App) lcdLoop(ctx context.Context) {
	m := &a.lcdm
	m.poke = make(chan struct{}, 1)
	var (
		conn      *lcdproto.Conn
		cfg       lcdConfig
		slide     int
		hold      bool
		light     = true
		lastKey   = time.Now()
		lastSwap  = time.Now()
		lastTry   time.Time
		alertsAt  int64
		pages     [][]string
		pageScr   []string
		pageBuilt time.Time
	)
	closeConn := func() {
		if conn != nil {
			conn.Close()
			conn = nil
		}
		m.setMirror(func(x *lcdMirror) { x.Connected = false })
	}
	defer closeConn()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		var keys <-chan string
		if conn != nil {
			keys = conn.Keys()
		}
		select {
		case <-ctx.Done():
			return
		case <-m.poke:
			closeConn()
			lastTry = time.Time{}
		case k, open := <-keys:
			if !open {
				closeConn()
				continue
			}
			lastKey = time.Now()
			act := cfg.Buttons[k]
			if !light && act != "backlight" {
				light = true
				conn.Backlight("on")
				m.setMirror(func(x *lcdMirror) { x.Backlight = true })
			}
			switch act {
			case "next":
				slide++
				lastSwap = time.Now()
			case "prev":
				slide--
				lastSwap = time.Now()
			case "hold":
				hold = !hold
			case "backlight":
				light = !light
				mode := "off"
				if light {
					mode = "on"
				}
				conn.Backlight(mode)
				m.setMirror(func(x *lcdMirror) { x.Backlight = light })
			case "flash":
				conn.Backlight("flash")
			case "alerts", "ip":
				want := map[string]string{"alerts": "alerts", "ip": "networks"}[act]
				for i, s := range pageScr {
					if s == want {
						slide = i
						hold = true
						lastSwap = time.Now()
						break
					}
				}
			}
			a.lcdDraw(conn, &slide, hold, pages, pageScr)
		case <-tick.C:
		}
		if a.lcdTesting() {
			closeConn()
			continue
		}
		cfg = a.lcdConfig()
		if !cfg.Enabled || a.Net == nil {
			closeConn()
			m.setMirror(func(x *lcdMirror) { x.Lines = nil })
			continue
		}
		if conn == nil {
			if time.Since(lastTry) < 5*time.Second {
				continue
			}
			lastTry = time.Now()
			c, err := lcdproto.Dial(lcdAddr, 3*time.Second)
			if err == nil {
				err = c.Setup("NopeWarden", lcdKeyNames)
				if err != nil {
					c.Close()
				}
			}
			if err != nil {
				m.setMirror(func(x *lcdMirror) { x.Connected, x.Error = false, "The display program is not answering: "+err.Error() })
				continue
			}
			conn, light, lastKey, lastSwap = c, true, time.Now(), time.Now()
			m.setMirror(func(x *lcdMirror) {
				x.Connected, x.Error, x.Width, x.Height, x.Backlight = true, "", c.Width, c.Height, true
			})
		}
		// Rebuild the screens every few seconds, so numbers stay fresh while a page stays up.
		if time.Since(pageBuilt) >= 2*time.Second || len(pages) == 0 {
			pages, pageScr = a.lcdPages(cfg, conn.Width, conn.Height)
			pageBuilt = time.Now()
		}
		if a.lcdWarnNew(cfg, &alertsAt) {
			for i, s := range pageScr {
				if s == "alerts" {
					slide, hold, lastSwap = i, false, time.Now()
				}
			}
		}
		if !hold && time.Since(lastSwap) >= time.Duration(cfg.Rotate)*time.Second {
			slide++
			lastSwap = time.Now()
		}
		if cfg.DimAfter > 0 && light && time.Since(lastKey) > time.Duration(cfg.DimAfter)*time.Minute {
			light = false
			conn.Backlight("off")
			m.setMirror(func(x *lcdMirror) { x.Backlight = false })
		}
		if err := a.lcdDraw(conn, &slide, hold, pages, pageScr); err != nil {
			closeConn()
		}
	}
}

// lcdDraw shows the current page and records it for the console's picture.
func (a *App) lcdDraw(conn *lcdproto.Conn, slide *int, hold bool, pages [][]string, scr []string) error {
	if conn == nil || len(pages) == 0 {
		return nil
	}
	n := len(pages)
	*slide = ((*slide % n) + n) % n
	lines := pages[*slide]
	if err := conn.Lines(lines); err != nil {
		return err
	}
	shown := make([]string, conn.Height)
	for i := range shown {
		if i < len(lines) {
			shown[i] = conn.Fit(lines[i])
		} else {
			shown[i] = conn.Fit("")
		}
	}
	a.lcdm.setMirror(func(x *lcdMirror) {
		x.Lines, x.Screen, x.Hold, x.Width, x.Height = shown, scr[*slide], hold, conn.Width, conn.Height
	})
	return nil
}

func (a *App) lcdWarnNew(cfg lcdConfig, last *int64) bool {
	if !cfg.Warn || a.Logs == nil {
		return false
	}
	var ts int64
	a.Logs.QueryRow(`SELECT COALESCE(MAX(ts),0) FROM threat_alerts WHERE ts>=?`, time.Now().Add(-time.Hour).Unix()).Scan(&ts)
	if *last == 0 {
		*last = ts
		return false
	}
	if ts > *last {
		*last = ts
		return true
	}
	return false
}

func lcdRate(bits float64) string {
	switch {
	case bits >= 1e9:
		return fmt.Sprintf("%.1fG", bits/1e9)
	case bits >= 1e6:
		return fmt.Sprintf("%.1fM", bits/1e6)
	case bits >= 1e3:
		return fmt.Sprintf("%.0fK", bits/1e3)
	}
	return fmt.Sprintf("%.0f", bits)
}

func lcdDur(sec int64) string {
	d, h, m := sec/86400, sec%86400/3600, sec%3600/60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// lcdPages turns the chosen screens into pages of text for a display of this size. A screen can be
// several pages (each network has its own).
func (a *App) lcdPages(cfg lcdConfig, w, h int) (pages [][]string, scr []string) {
	add := func(id string, lines ...string) {
		pages = append(pages, lines)
		scr = append(scr, id)
	}
	for _, id := range cfg.Screens {
		switch id {
		case "name":
			host, _ := os.Hostname()
			l := []string{"NopeWarden " + a.Cfg.Version, host}
			if h >= 3 {
				l = append(l, "", time.Now().Format("Mon 2 Jan 15:04"))
			}
			add(id, l...)
		case "internet":
			wans := a.wanDevices()
			var names []string
			for n := range wans {
				names = append(names, n)
			}
			sort.Strings(names)
			lat := a.traffic.latest()
			if len(names) == 0 {
				add(id, "Internet", "no connection set up")
			}
			for _, n := range names {
				v := lat[n]
				add(id, "Internet: "+n, fmt.Sprintf("v%s ^%s", lcdRate(v[0]), lcdRate(v[1])))
			}
		case "networks":
			speeds := a.netTraffic.latest()
			rows, err := a.Config.Query(`SELECT z.name, COALESCE(NULLIF(i.ipv4_cidr,''),'no address') FROM zones z JOIN interfaces i ON i.zone_id=z.id WHERE z.kind NOT IN ('wan') AND i.enabled=1 ORDER BY z.id, i.id`)
			if err != nil {
				break
			}
			for rows.Next() {
				var name, addr string
				if rows.Scan(&name, &addr) != nil {
					continue
				}
				if i := strings.Index(addr, "/"); i > 0 {
					addr = addr[:i]
				}
				v := speeds[name] // a port's received bytes are what its devices sent
				add(id, name, addr, fmt.Sprintf("v%s ^%s", lcdRate(v[1]), lcdRate(v[0])))
			}
			rows.Close()
		case "dhcp":
			used, size := 0, 0
			if a.DHCP != nil {
				for _, u := range a.DHCP.Utilisation() {
					used += u.Active
					size += u.Size
				}
			}
			add(id, "DHCP addresses", fmt.Sprintf("%d of %d in use", used, size))
		case "system":
			load, up := "?", int64(0)
			if b, err := os.ReadFile("/proc/loadavg"); err == nil {
				if f := strings.Fields(string(b)); len(f) > 0 {
					load = f[0]
				}
			}
			if b, err := os.ReadFile("/proc/uptime"); err == nil {
				if f := strings.Fields(string(b)); len(f) > 0 {
					v, _ := strconv.ParseFloat(f[0], 64)
					up = int64(v)
				}
			}
			mem := lcdMemory()
			l2 := "Up " + lcdDur(up)
			if t := a.cpuTemp(); t > 0 {
				l2 += fmt.Sprintf(" CPU %dC", t)
			}
			add(id, "Load "+load+" Mem "+mem, l2)
		case "alerts":
			var warn, thr int
			since := time.Now().Add(-24 * time.Hour).Unix()
			if a.Logs != nil {
				a.Logs.QueryRow(`SELECT COUNT(*) FROM events WHERE ts>=? AND severity IN ('warn','warning','error','critical')`, since).Scan(&warn)
				a.Logs.QueryRow(`SELECT COUNT(*) FROM threat_alerts WHERE ts>=?`, since).Scan(&thr)
			}
			add(id, fmt.Sprintf("Warnings 24h: %d", warn), fmt.Sprintf("Threats 24h: %d", thr))
		}
	}
	if len(pages) == 0 {
		add("name", "NopeWarden", "no screens chosen")
	}
	return pages, scr
}

func lcdMemory() string {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "?"
	}
	var total, avail float64
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total <= 0 {
		return "?"
	}
	return fmt.Sprintf("%.0f%%", (total-avail)/total*100)
}

// ---- testing ----

func (a *App) lcdTesting() bool {
	a.lcdm.mu.Lock()
	defer a.lcdm.mu.Unlock()
	return a.lcdm.test != nil
}

// lcdTestStart applies the draft settings, connects, and draws a first screen. Nothing is saved.
func (a *App) lcdTestStart(c lcdConfig) (map[string]any, error) {
	if a.Net == nil {
		return nil, errors.New("the network helper is not running")
	}
	c.Enabled = true
	if err := c.plan().Check(); err != nil {
		return nil, err
	}
	a.lcdTestEnd(false)
	// Hold the main loop off while the test runs.
	t := &lcdTest{lastUsed: time.Now(), done: make(chan struct{})}
	a.lcdm.mu.Lock()
	a.lcdm.test = t
	a.lcdm.mu.Unlock()
	fail := func(err error) (map[string]any, error) {
		a.lcdTestEnd(true)
		return nil, err
	}
	if err := a.Net.LCDApply(c.plan()); err != nil {
		return fail(err)
	}
	var conn *lcdproto.Conn
	var derr error
	for i := 0; i < 12; i++ {
		conn, derr = lcdproto.Dial(lcdAddr, 2*time.Second)
		if derr == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if derr != nil {
		st := a.Net.LCDStatus()
		return fail(fmt.Errorf("the display program did not start with these settings (%v). Running: %v. Check the driver and the settings, and that the display is plugged in", derr, st.Running))
	}
	if err := conn.Setup("NopeWarden test", lcdKeyNames); err != nil {
		conn.Close()
		return fail(err)
	}
	t.conn = conn
	conn.Lines([]string{"NopeWarden", "display test"})
	go func() { // collect the buttons pressed, and end by itself when nobody is using it
		idle := time.NewTicker(30 * time.Second)
		defer idle.Stop()
		for {
			select {
			case k, open := <-conn.Keys():
				if !open {
					return
				}
				a.lcdm.mu.Lock()
				t.n++
				t.keys = append(t.keys, lcdKeyEvent{N: t.n, Key: k, At: time.Now().Unix()})
				if len(t.keys) > 100 {
					t.keys = t.keys[len(t.keys)-100:]
				}
				t.lastUsed = time.Now()
				a.lcdm.mu.Unlock()
			case <-idle.C:
				a.lcdm.mu.Lock()
				stale := time.Since(t.lastUsed) > 10*time.Minute
				a.lcdm.mu.Unlock()
				if stale {
					a.lcdTestEnd(true)
					return
				}
			case <-t.done:
				return
			}
		}
	}()
	return map[string]any{"width": conn.Width, "height": conn.Height, "cell_width": conn.CellW, "cell_height": conn.CellH, "server": conn.Server}, nil
}

// lcdTestEnd closes the test and puts the saved settings back (the display runs as saved, or stops).
func (a *App) lcdTestEnd(restore bool) {
	a.lcdm.mu.Lock()
	t := a.lcdm.test
	a.lcdm.test = nil
	a.lcdm.mu.Unlock()
	if t == nil {
		return
	}
	close(t.done)
	if t.conn != nil {
		t.conn.Close()
	}
	if restore && a.Net != nil {
		a.Net.LCDApply(a.lcdConfig().plan())
	}
	select {
	case a.lcdm.poke <- struct{}{}:
	default:
	}
}

func (a *App) lcdTestConn() (*lcdproto.Conn, *lcdTest, error) {
	a.lcdm.mu.Lock()
	defer a.lcdm.mu.Unlock()
	t := a.lcdm.test
	if t == nil || t.conn == nil {
		return nil, nil, errors.New("no test is running. Start one first")
	}
	t.lastUsed = time.Now()
	return t.conn, t, nil
}

// lcdPattern draws one of the test pictures: rows and columns numbered, every character, the built-in
// symbols, or the backlight changing.
func lcdPattern(c *lcdproto.Conn, kind string, page int) (string, error) {
	switch kind {
	case "numbers":
		lines := make([]string, c.Height)
		for y := range lines {
			var b strings.Builder
			for x := 0; x < c.Width; x++ {
				if y%2 == 0 {
					b.WriteByte(byte('0' + (x+1)%10))
				} else {
					b.WriteByte(byte('A' + x%26))
				}
			}
			lines[y] = b.String()
		}
		if c.Height >= 3 {
			lines[2] = fmt.Sprintf("Row 3 of %d, %d wide", c.Height, c.Width)
		}
		return fmt.Sprintf("Drawn %d rows of %d characters. Count them on the display.", c.Height, c.Width), c.Lines(lines)
	case "chars":
		per := c.Width * c.Height
		start := 33 + page*per
		if start > 126 {
			start = 33
		}
		lines := make([]string, c.Height)
		ch := start
		for y := range lines {
			var b strings.Builder
			for x := 0; x < c.Width; x++ {
				if ch > 126 {
					ch = 33
				}
				b.WriteByte(byte(ch))
				ch++
			}
			lines[y] = b.String()
		}
		return fmt.Sprintf("Characters from %q on. Look for gaps or odd shapes.", string(rune(start))), c.Lines(lines)
	case "icons":
		if err := c.Lines([]string{"Symbols:", ""}); err != nil {
			return "", err
		}
		y := 2
		if c.Height < 2 {
			y = 1
		}
		for i, n := range []string{"BLOCK_FILLED", "HEART_FILLED", "ARROW_UP", "ARROW_DOWN", "CHECKBOX_ON", "CHECKBOX_OFF"} {
			if i >= c.Width {
				break
			}
			if err := c.Icon("i"+strconv.Itoa(i), i+1, y, n); err != nil {
				return "", err
			}
		}
		return "Drawn a few of the display's built-in symbols on the last row.", nil
	case "backlight":
		for _, m := range []string{"off", "on", "flash"} {
			if err := c.Backlight(m); err != nil {
				return "", err
			}
			time.Sleep(700 * time.Millisecond)
		}
		c.Backlight("on")
		return "The backlight went off, came on, then flashed.", nil
	}
	return "", errors.New("not a test picture")
}

// ---- routes ----

func (a *App) registerLCDRoutes() {
	s := a.API
	s.Handle("GET /api/lcd", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		st := netd.LCDInfo{}
		if a.Net != nil {
			st = a.Net.LCDStatus()
		}
		a.lcdm.mu.Lock()
		mir := a.lcdm.mirror
		testing := a.lcdm.test != nil
		a.lcdm.mu.Unlock()
		scr := make([]map[string]string, 0, len(lcdScreens))
		for _, x := range lcdScreens {
			scr = append(scr, map[string]string{"id": x.ID, "title": x.Title, "text": x.Text})
		}
		act := make([]map[string]string, 0, len(lcdActions))
		for _, x := range lcdActions {
			act = append(act, map[string]string{"id": x.ID, "title": x.Title})
		}
		api.JSON(w, 200, map[string]any{"config": a.lcdConfig(), "status": st, "mirror": mir, "testing": testing, "screens": scr, "actions": act, "keys": lcdKeyNames, "drivers": lcdDriverHints})
	})
	s.HandleAdmin("POST /api/lcd/install", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errors.New("the network helper is not running"))
			return
		}
		if err := a.Net.LCDInstallStart(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Installing the front panel display software", "")
		ok(w)
	})
	s.HandleAdmin("POST /api/lcd/remove", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errors.New("the network helper is not running"))
			return
		}
		a.lcdTestEnd(false)
		cfg := a.lcdConfig()
		cfg.Enabled = false
		b, _ := json.Marshal(cfg)
		a.Config.SetSetting(lcdSetting, string(b))
		if err := a.Net.LCDRemove(); err != nil {
			fail(w, err)
			return
		}
		a.event(c.User.Username, "info", "Removed the front panel display software", "")
		ok(w)
	})
	s.HandleAdmin("POST /api/lcd/detect", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.Net == nil {
			fail(w, errors.New("the network helper is not running"))
			return
		}
		f, err := a.Net.LCDDetect()
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, f)
	})
	s.HandleAdmin("PUT /api/lcd/config", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		cfg := defaultLCD()
		if !api.Decode(w, r, &cfg) {
			return
		}
		if err := cfg.check(); err != nil {
			fail(w, err)
			return
		}
		if a.Net == nil {
			fail(w, errors.New("the network helper is not running"))
			return
		}
		a.lcdTestEnd(false)
		if err := a.Net.LCDApply(cfg.plan()); err != nil {
			fail(w, err)
			return
		}
		b, _ := json.Marshal(cfg)
		a.Config.SetSetting(lcdSetting, string(b))
		a.event(c.User.Username, "info", "Front panel display settings changed", map[bool]string{true: "On, driver " + cfg.Driver, false: "Off"}[cfg.Enabled])
		select {
		case a.lcdm.poke <- struct{}{}:
		default:
		}
		ok(w)
	})
	s.HandleAdmin("POST /api/lcd/test/start", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		cfg := defaultLCD()
		if !api.Decode(w, r, &cfg) {
			return
		}
		out, err := a.lcdTestStart(cfg)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, out)
	})
	s.HandleAdmin("POST /api/lcd/test/pattern", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Kind string `json:"kind"`
			Page int    `json:"page"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		conn, _, err := a.lcdTestConn()
		if err != nil {
			fail(w, err)
			return
		}
		msg, err := lcdPattern(conn, in.Kind, in.Page)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]string{"message": msg})
	})
	s.HandleAdmin("GET /api/lcd/test/keys", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		since, _ := strconv.Atoi(r.URL.Query().Get("since"))
		_, t, err := a.lcdTestConn()
		if err != nil {
			api.JSON(w, 200, map[string]any{"keys": []lcdKeyEvent{}, "next": since, "running": false})
			return
		}
		a.lcdm.mu.Lock()
		out := []lcdKeyEvent{}
		for _, k := range t.keys {
			if k.N > since {
				out = append(out, k)
			}
		}
		next := t.n
		a.lcdm.mu.Unlock()
		api.JSON(w, 200, map[string]any{"keys": out, "next": next, "running": true})
	})
	s.HandleAdmin("POST /api/lcd/test/end", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		a.lcdTestEnd(true)
		ok(w)
	})
}

// lcdDriverHints are the drivers the console offers first, with the settings each one needs. Any
// other driver LCDd has can be used by choosing it and typing its settings.
var lcdDriverHints = []map[string]any{
	{"id": "CFontz633", "title": "Crystalfontz CFA-631, 633, 635 (USB or serial)", "settings": []string{"Device", "Model", "Speed", "Size"}},
	{"id": "hd44780", "title": "HD44780 text display (parallel, I2C and many appliance panels)", "settings": []string{"ConnectionType", "Device", "Port", "Size"}},
	{"id": "sdeclcd", "title": "SDEC panels (WatchGuard Firebox and similar)", "settings": []string{"Size"}},
	{"id": "mtxorb", "title": "Matrix Orbital (serial or USB)", "settings": []string{"Device", "Size", "Speed"}},
}

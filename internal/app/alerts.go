package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
)

// Alerts in plain words, by email or ntfy: a new device joined, the internet went down and came
// back, a watched device went offline, Gatehouse repaired or updated something itself. Each kind
// can be switched off, quiet hours hold messages until the morning, and a test button proves the
// channel works.

type alertEmail struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // starttls, tls or none
	User     string `json:"user"`
	From     string `json:"from"`
	To       string `json:"to"`
	Pass     string `json:"pass,omitempty"` // sealed; never sent to the console
}

type alertNtfy struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Topic   string `json:"topic"`
	Token   string `json:"token,omitempty"` // sealed; never sent to the console
}

type alertQuiet struct {
	Enabled bool   `json:"enabled"`
	From    string `json:"from"` // 22:00
	To      string `json:"to"`   // 07:00
}

type alertCfg struct {
	Email alertEmail      `json:"email"`
	Ntfy  alertNtfy       `json:"ntfy"`
	On    map[string]bool `json:"on"`
	Quiet alertQuiet      `json:"quiet"`
	Watch []string        `json:"watch"` // devices (MAC) to tell about when they go offline
}

var alertKinds = []struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
}{
	{"new_device", "A new device joins", "A device Gatehouse has never seen gets an address."},
	{"internet", "The internet goes down or comes back", "Sent after the connection has been down for a minute."},
	{"device_offline", "A device I choose goes offline", "Pick the devices below. Sent when one has been gone for 10 minutes."},
	{"repaired", "Gatehouse repaired or updated something", "For example it restarted the Wi-Fi or installed an update."},
}

func defaultAlerts() alertCfg {
	return alertCfg{
		Email: alertEmail{Port: 587, Security: "starttls"},
		Ntfy:  alertNtfy{Server: "https://ntfy.sh"},
		On:    map[string]bool{},
		Quiet: alertQuiet{From: "22:00", To: "07:00"},
		Watch: []string{},
	}
}

func (a *App) alertConfig() alertCfg {
	c := defaultAlerts()
	if raw := a.setting("alerts.config", ""); raw != "" {
		json.Unmarshal([]byte(raw), &c)
	}
	if c.On == nil {
		c.On = map[string]bool{}
	}
	if c.Watch == nil {
		c.Watch = []string{}
	}
	return c
}

func (a *App) saveAlertConfig(c alertCfg) {
	raw, _ := json.Marshal(c)
	a.Config.SetSetting("alerts.config", string(raw))
}

// inQuiet says whether a moment falls inside the quiet hours (which may cross midnight).
func inQuiet(q alertQuiet, now time.Time) bool {
	if !q.Enabled {
		return false
	}
	f, ok1 := clockMinutes(q.From)
	t, ok2 := clockMinutes(q.To)
	if !ok1 || !ok2 || f == t {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if f < t {
		return m >= f && m < t
	}
	return m >= f || m < t
}

func clockMinutes(s string) (int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, false
	}
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	if e1 != nil || e2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

type alertMsg struct {
	At    int64  `json:"at"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type alertState struct {
	mu       sync.Mutex
	held     []alertMsg
	sent     []time.Time
	wasDown  time.Time
	downTold bool
	offline  map[string]time.Time
	told     map[string]bool
}

func (a *App) localNow() time.Time {
	loc := time.Local
	if name := strings.TrimSpace(a.Config.Setting("system.timezone", "")); name != "" {
		if l, err := time.LoadLocation(name); err == nil {
			loc = l
		}
	}
	return time.Now().In(loc)
}

// alert is how the rest of Gatehouse says something. It checks the kind is on, honours quiet hours
// and limits the rate so a flapping connection cannot flood a phone.
func (a *App) alert(kind, title, body string) {
	c := a.alertConfig()
	if !c.On[kind] || (!c.Email.Enabled && !c.Ntfy.Enabled) {
		return
	}
	msg := alertMsg{At: time.Now().Unix(), Title: title, Body: body}
	a.alerts.mu.Lock()
	if inQuiet(c.Quiet, a.localNow()) {
		a.alerts.held = append(a.alerts.held, msg)
		if len(a.alerts.held) > 50 {
			a.alerts.held = a.alerts.held[len(a.alerts.held)-50:]
		}
		a.alerts.mu.Unlock()
		return
	}
	cut := time.Now().Add(-time.Hour)
	keep := a.alerts.sent[:0]
	for _, t := range a.alerts.sent {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	a.alerts.sent = keep
	if len(a.alerts.sent) >= 20 {
		a.alerts.mu.Unlock()
		return
	}
	a.alerts.sent = append(a.alerts.sent, time.Now())
	a.alerts.mu.Unlock()
	if err := a.deliverAlert(c, title, body); err != nil {
		a.event("alerts", "warning", "An alert could not be sent", err.Error())
	}
}

// flushHeld sends what was held during quiet hours as one message.
func (a *App) flushHeld() {
	c := a.alertConfig()
	if inQuiet(c.Quiet, a.localNow()) {
		return
	}
	a.alerts.mu.Lock()
	held := a.alerts.held
	a.alerts.held = nil
	a.alerts.mu.Unlock()
	if len(held) == 0 {
		return
	}
	var b strings.Builder
	for _, m := range held {
		fmt.Fprintf(&b, "%s: %s", time.Unix(m.At, 0).In(a.localNow().Location()).Format("15:04"), m.Title)
		if m.Body != "" {
			b.WriteString(" - " + m.Body)
		}
		b.WriteString("\n")
	}
	title := fmt.Sprintf("%d thing%s happened during quiet hours", len(held), plural(len(held)))
	if err := a.deliverAlert(c, title, strings.TrimSpace(b.String())); err != nil {
		a.event("alerts", "warning", "An alert could not be sent", err.Error())
	}
}

func (a *App) deliverAlert(c alertCfg, title, body string) error {
	var errs []string
	if c.Ntfy.Enabled {
		if err := a.sendNtfy(c.Ntfy, title, body); err != nil {
			errs = append(errs, "ntfy: "+err.Error())
		}
	}
	if c.Email.Enabled {
		if err := a.sendEmail(c.Email, title, body); err != nil {
			errs = append(errs, "email: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (a *App) sendNtfy(n alertNtfy, title, body string) error {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(n.Server), "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("the ntfy server address must start with https://")
	}
	topic := strings.TrimSpace(n.Topic)
	if topic == "" || strings.ContainsAny(topic, "/ ?#") {
		return errors.New("enter a topic name with no spaces or slashes")
	}
	req, _ := http.NewRequest("POST", u.String()+"/"+topic, strings.NewReader(body+" "))
	req.Header.Set("Title", title)
	req.Header.Set("Tags", "shield")
	if n.Token != "" {
		if tok, err := a.Box.Open(n.Token); err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	cl := &http.Client{Timeout: 15 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return errors.New("could not reach the ntfy server: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("the ntfy server said %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func emailMessage(from, to, title, body string) []byte {
	clean := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	return []byte("From: " + clean(from) + "\r\nTo: " + clean(to) + "\r\nSubject: " + clean("Gatehouse: "+title) +
		"\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n") + "\r\n")
}

func (a *App) sendEmail(e alertEmail, title, body string) error {
	if e.Host == "" || e.From == "" || e.To == "" {
		return errors.New("fill in the mail server, your address and where to send alerts")
	}
	from, err := mail.ParseAddress(e.From)
	if err != nil {
		return errors.New("the From address is not a valid email address")
	}
	to, err := mail.ParseAddress(e.To)
	if err != nil {
		return errors.New("the To address is not a valid email address")
	}
	addr := net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
	d := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if e.Security == "tls" {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: e.Host})
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return errors.New("could not reach the mail server: " + err.Error())
	}
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	cl, err := smtp.NewClient(conn, e.Host)
	if err != nil {
		conn.Close()
		return errors.New("the mail server did not answer properly: " + err.Error())
	}
	defer cl.Close()
	if e.Security == "starttls" {
		if err := cl.StartTLS(&tls.Config{ServerName: e.Host}); err != nil {
			return errors.New("the mail server would not start a secure connection: " + err.Error())
		}
	}
	if e.User != "" {
		pass := ""
		if e.Pass != "" {
			pass, _ = a.Box.Open(e.Pass)
		}
		if e.Security == "none" {
			return errors.New("a password is not sent over an unencrypted connection; choose a secure option")
		}
		if err := cl.Auth(smtp.PlainAuth("", e.User, pass, e.Host)); err != nil {
			return errors.New("the mail server refused the login: " + err.Error())
		}
	}
	if err := cl.Mail(from.Address); err != nil {
		return errors.New("the mail server refused the From address: " + err.Error())
	}
	if err := cl.Rcpt(to.Address); err != nil {
		return errors.New("the mail server refused the To address: " + err.Error())
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(emailMessage(from.String(), to.String(), title, body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// repairEvent says whether an event is Gatehouse repairing or updating something itself.
func repairEvent(actor, title string) bool {
	switch title {
	case "Gatehouse was updated", "System updates were installed", "An update did not work and was undone":
		return true
	}
	return actor == "wifi" && (strings.HasSuffix(title, "is working again") || strings.HasSuffix(title, "keeps failing"))
}

func (a *App) alertFromEvent(actor, title, detail string) {
	if repairEvent(actor, title) {
		a.alert("repaired", title, detail)
	}
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return fmt.Sprintf("%d hours %d minutes", int(d.Hours()), int(d.Minutes())%60)
}

func (a *App) alertLoop(ctx context.Context) {
	a.alerts.offline = map[string]time.Time{}
	a.alerts.told = map[string]bool{}
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.alertTick(time.Now())
		}
	}
}

func (a *App) alertTick(now time.Time) {
	c := a.alertConfig()
	a.flushHeld()
	if !c.Email.Enabled && !c.Ntfy.Enabled {
		return
	}
	// Internet down and back.
	down := a.internetLooksDown()
	if c.On["internet"] {
		a.alerts.mu.Lock()
		switch {
		case down && a.alerts.wasDown.IsZero():
			a.alerts.wasDown = now
		case down && !a.alerts.downTold && now.Sub(a.alerts.wasDown) >= time.Minute:
			a.alerts.downTold = true
			a.alerts.mu.Unlock()
			a.alert("internet", "The internet is down", "Gatehouse cannot reach the internet. It will tell you when it is back.")
			a.alerts.mu.Lock()
		case !down && !a.alerts.wasDown.IsZero():
			was, told := a.alerts.wasDown, a.alerts.downTold
			a.alerts.wasDown, a.alerts.downTold = time.Time{}, false
			if told {
				a.alerts.mu.Unlock()
				a.alert("internet", "The internet is back", "It was down for "+fmtDur(now.Sub(was))+".")
				a.alerts.mu.Lock()
			}
		}
		a.alerts.mu.Unlock()
	}
	// New devices: anything first seen since the last look.
	since := a.setting("alerts.new_since", "")
	if since == "" {
		a.Config.SetSetting("alerts.new_since", strconv.FormatInt(now.Unix(), 10))
	} else if s, err := strconv.ParseInt(since, 10, 64); err == nil {
		type nd struct{ mac, name, ip string }
		var found []nd
		var top int64 = s
		if rows, err := a.Config.Query(`SELECT mac,name,last_ip,first_seen FROM devices WHERE first_seen>? ORDER BY first_seen`, s); err == nil {
			for rows.Next() {
				var d nd
				var fs int64
				rows.Scan(&d.mac, &d.name, &d.ip, &fs)
				found = append(found, d)
				if fs > top {
					top = fs
				}
			}
			rows.Close()
		}
		if top > s {
			a.Config.SetSetting("alerts.new_since", strconv.FormatInt(top, 10))
		}
		if c.On["new_device"] {
			for _, d := range found {
				who := d.name
				if who == "" {
					who = d.ip
				}
				a.alert("new_device", "A new device joined: "+who, "Hardware address "+d.mac+", address "+d.ip+".")
			}
		}
	}
	// Watched devices going offline.
	if c.On["device_offline"] && len(c.Watch) > 0 {
		watch := map[string]bool{}
		for _, m := range c.Watch {
			watch[strings.ToLower(m)] = true
		}
		if list, err := a.Devices.List(); err == nil {
			for _, d := range list {
				m := strings.ToLower(d.MAC)
				if !watch[m] {
					continue
				}
				name := d.Name
				if name == "" {
					name = orWord(d.Hostname, d.MAC)
				}
				a.alerts.mu.Lock()
				switch {
				case !d.Online && a.alerts.offline[m].IsZero():
					a.alerts.offline[m] = now
				case !d.Online && !a.alerts.told[m] && now.Sub(a.alerts.offline[m]) >= 10*time.Minute:
					a.alerts.told[m] = true
					a.alerts.mu.Unlock()
					a.alert("device_offline", name+" went offline", "It has not been seen for 10 minutes.")
					a.alerts.mu.Lock()
				case d.Online && !a.alerts.offline[m].IsZero():
					was, told := a.alerts.offline[m], a.alerts.told[m]
					delete(a.alerts.offline, m)
					delete(a.alerts.told, m)
					if told {
						a.alerts.mu.Unlock()
						a.alert("device_offline", name+" is back online", "It was gone for "+fmtDur(now.Sub(was))+".")
						a.alerts.mu.Lock()
					}
				}
				a.alerts.mu.Unlock()
			}
		}
	}
}

type alertView struct {
	Email struct {
		alertEmail
		HasPass bool `json:"has_pass"`
	} `json:"email"`
	Ntfy struct {
		alertNtfy
		HasToken bool `json:"has_token"`
	} `json:"ntfy"`
	On    map[string]bool `json:"on"`
	Quiet alertQuiet      `json:"quiet"`
	Watch []string        `json:"watch"`
	Kinds any             `json:"kinds"`
	Held  int             `json:"held"`
}

func (a *App) alertView() alertView {
	c := a.alertConfig()
	var v alertView
	v.Email.alertEmail, v.Email.HasPass = c.Email, c.Email.Pass != ""
	v.Email.Pass = ""
	v.Ntfy.alertNtfy, v.Ntfy.HasToken = c.Ntfy, c.Ntfy.Token != ""
	v.Ntfy.Token = ""
	v.On, v.Quiet, v.Watch, v.Kinds = c.On, c.Quiet, c.Watch, alertKinds
	a.alerts.mu.Lock()
	v.Held = len(a.alerts.held)
	a.alerts.mu.Unlock()
	return v
}

type alertIn struct {
	Email struct {
		alertEmail
		NewPass string `json:"new_pass"`
	} `json:"email"`
	Ntfy struct {
		alertNtfy
		NewToken string `json:"new_token"`
	} `json:"ntfy"`
	On    map[string]bool `json:"on"`
	Quiet alertQuiet      `json:"quiet"`
	Watch []string        `json:"watch"`
}

func (a *App) mergeAlerts(in alertIn) (alertCfg, error) {
	old := a.alertConfig()
	c := alertCfg{Email: in.Email.alertEmail, Ntfy: in.Ntfy.alertNtfy, On: map[string]bool{}, Quiet: in.Quiet, Watch: []string{}}
	c.Email.Pass, c.Ntfy.Token = old.Email.Pass, old.Ntfy.Token
	if in.Email.NewPass != "" {
		c.Email.Pass = a.Box.Seal(in.Email.NewPass)
	}
	if in.Ntfy.NewToken != "" {
		c.Ntfy.Token = a.Box.Seal(in.Ntfy.NewToken)
	}
	if c.Email.Port == 0 {
		c.Email.Port = 587
	}
	if c.Email.Port < 1 || c.Email.Port > 65535 {
		return c, errors.New("the mail server port must be between 1 and 65535")
	}
	switch c.Email.Security {
	case "starttls", "tls", "none":
	default:
		return c, errors.New("choose how to connect to the mail server")
	}
	if c.Email.Enabled {
		if c.Email.Host == "" || c.Email.From == "" || c.Email.To == "" {
			return c, errors.New("fill in the mail server, your address and where to send alerts, or switch email off")
		}
	}
	if c.Ntfy.Enabled && strings.TrimSpace(c.Ntfy.Topic) == "" {
		return c, errors.New("enter a ntfy topic, or switch ntfy off")
	}
	for _, k := range alertKinds {
		c.On[k.ID] = in.On[k.ID]
	}
	for _, q := range []string{c.Quiet.From, c.Quiet.To} {
		if _, ok := clockMinutes(q); !ok && c.Quiet.Enabled {
			return c, errors.New("quiet hours need times like 22:00 and 07:00")
		}
	}
	seen := map[string]bool{}
	for _, m := range in.Watch {
		hw, err := net.ParseMAC(strings.TrimSpace(m))
		if err != nil {
			continue
		}
		s := strings.ToLower(hw.String())
		if !seen[s] {
			seen[s] = true
			c.Watch = append(c.Watch, s)
		}
	}
	return c, nil
}

func (a *App) registerAlertRoutes() {
	s := a.API
	s.Handle("GET /api/alerts", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.alertView())
	})
	s.HandleAdmin("PUT /api/alerts", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in alertIn
		if !api.Decode(w, r, &in) {
			return
		}
		cfg, err := a.mergeAlerts(in)
		if err != nil {
			fail(w, err)
			return
		}
		a.saveAlertConfig(cfg)
		api.JSON(w, 200, a.alertView())
	})
	s.HandleAdmin("POST /api/alerts/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Channel string `json:"channel"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		cfg := a.alertConfig()
		title, body := "Test alert", "If you can read this, Gatehouse can reach you."
		var err error
		switch in.Channel {
		case "email":
			err = a.sendEmail(cfg.Email, title, body)
		case "ntfy":
			err = a.sendNtfy(cfg.Ntfy, title, body)
		default:
			err = errors.New("choose email or ntfy")
		}
		if err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
}

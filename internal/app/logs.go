package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
	dnssrv "gatehouse/internal/dns"
	"gatehouse/internal/fw"
	"gatehouse/internal/netd"
)

// One log for everything. DNS questions, firewall decisions about new connections, and
// events (VPN changes, alerts) are searched together, and every row says why it happened.

// LogItem is one row of the combined log.
type LogItem struct {
	Key     string `json:"key"`
	TS      int64  `json:"ts"`
	Kind    string `json:"kind"`    // dns, firewall, event
	Verdict string `json:"verdict"` // allowed, blocked, info, warning
	Who     string `json:"who"`     // the device, by name when known
	Addr    string `json:"addr"`    // its address
	What    string `json:"what"`
	Detail  string `json:"detail"`
	Why     string `json:"why"`

	// The parts of a connection, so any of them can be clicked to filter by it or to make a rule.
	Src       string `json:"src,omitempty"`
	Dst       string `json:"dst,omitempty"`
	DstName   string `json:"dst_name,omitempty"`
	Port      int    `json:"port,omitempty"`
	Proto     string `json:"proto,omitempty"`
	Service   string `json:"service,omitempty"`
	Country   string `json:"country,omitempty"`
	FromZone  string `json:"from_zone,omitempty"`
	ToZone    string `json:"to_zone,omitempty"`
	Direction string `json:"direction,omitempty"`
	Rule      string `json:"rule,omitempty"`
	App       string `json:"app,omitempty"`
	Category  string `json:"category,omitempty"`
}

// fwReason turns the comment of the rule that decided a connection into a sentence.
func fwReason(verdict, tag, from, to string) string {
	kind, rest, _ := strings.Cut(tag, ":")
	allowed := verdict == "allow"
	switch kind {
	case "internet":
		return fmt.Sprintf("The %q network is allowed to use the internet.", rest)
	case "trusted":
		return fmt.Sprintf("%q is a trusted network, so it may reach your other networks.", rest)
	case "within":
		return fmt.Sprintf("Devices inside %q are allowed to talk to each other.", rest)
	case "rule":
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) == 3 {
			if allowed {
				return fmt.Sprintf("Allowed by your rule %q (%s to %s).", parts[2], parts[0], parts[1])
			}
			return fmt.Sprintf("Blocked by your rule %q (%s to %s).", parts[2], parts[0], parts[1])
		}
	case "killswitch":
		return fmt.Sprintf("Blocked by the kill switch for VPN %q. Devices in that VPN never fall back to your normal internet connection.", rest)
	case "vpn":
		return fmt.Sprintf("Incoming connection to your VPN %q.", rest)
	case "forward":
		return "Allowed by a port forward."
	case "default":
		if rest == "in" {
			return fmt.Sprintf("Blocked: nothing allows %s to connect to the router itself on this port.", orAny(from))
		}
		return fmt.Sprintf("Blocked: no rule allows %s to reach %s.", orAny(from), orAny(to))
	}
	if allowed {
		return "Allowed by the firewall."
	}
	return "Blocked by the firewall."
}

func orAny(s string) string {
	if s == "" {
		return "that network"
	}
	return fmt.Sprintf("%q", s)
}

// fwNames maps interfaces to network names and addresses to device names.
type fwNames struct {
	mu    sync.Mutex
	zones map[string]string
	at    time.Time
}

func (a *App) zoneOf(iface string) string {
	n := &a.fwn
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.zones == nil || time.Since(n.at) > 30*time.Second {
		n.zones = map[string]string{}
		if m, err := fw.Load(a.Config); err == nil {
			for _, z := range m.Zones {
				for _, i := range z.Ifaces {
					n.zones[i] = z.Name
				}
			}
		}
		n.at = time.Now()
	}
	return n.zones[iface]
}

func (a *App) deviceName(ip string) string {
	p := net.ParseIP(ip)
	if p == nil || !(p.IsPrivate() || p.IsLinkLocalUnicast()) {
		return ""
	}
	return a.Devices.Client(ip).Name
}

// storeFW records events in the log database and tells the console.
func (a *App) storeFW(evs []netd.FWEvent) {
	if len(evs) == 0 {
		return
	}
	tx, err := a.Logs.Begin()
	if err != nil {
		return
	}
	for _, e := range evs {
		from, to := a.zoneOf(e.In), a.zoneOf(e.Out)
		if e.Out == "" {
			to = "Router"
		}
		sn, dn := a.deviceName(e.Src), a.deviceName(e.Dst)
		if dn == "" {
			dn = a.names.get(e.Dst) // the name a device asked for before connecting
		}
		tx.Exec(`INSERT INTO fw_events(ts,action,from_zone,from_addr,to_zone,to_addr,proto,dport,rule,sport,src_name,dst_name,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			e.TS, e.Verdict, from, e.Src, to, e.Dst, e.Proto, e.DPort, e.Tag, e.SPort, sn, dn, fwReason(e.Verdict, e.Tag, from, to))
	}
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return
	}
	a.Bus.Publish("logs.live", map[string]int{"fw": len(evs)})
}

// collectFW pulls firewall decisions from the helper about once a second.
func (a *App) collectFW(ctx context.Context) {
	if a.Net == nil {
		return
	}
	var cursor uint64
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		evs, last, err := a.Net.FWEvents(cursor, 500)
		if err != nil {
			continue
		}
		if last < cursor { // the helper restarted
			cursor = 0
			continue
		}
		if len(evs) > 0 {
			a.storeFW(evs)
			cursor = evs[len(evs)-1].Seq
		} else {
			cursor = last
		}
	}
}

func (a *App) pruneFWLog(days int) {
	if days <= 0 {
		return
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	a.Logs.Exec(`DELETE FROM fw_events WHERE ts<?`, cut)
	a.Logs.Exec(`DELETE FROM fw_events WHERE id < (SELECT COALESCE(MAX(id),0)-500000 FROM fw_events)`)
	a.Logs.Exec(`DELETE FROM events WHERE ts<?`, cut)
}

// Search ------------------------------------------------------------------------------------

func like(s string) string {
	return "%" + strings.NewReplacer("%", "", "_", "", `\`, "").Replace(strings.TrimSpace(s)) + "%"
}

func dnsItem(r dnssrv.Row) LogItem {
	it := LogItem{Key: "d" + strconv.FormatInt(r.ID, 10), TS: r.TS, Kind: "dns", Who: r.ClientName, Addr: r.Client, What: r.QName, Detail: r.QType + " lookup", Verdict: "allowed"}
	if it.Who == "" {
		it.Who = r.Client
	}
	switch r.Result {
	case "blocked":
		it.Verdict = "blocked"
		it.Why = "Blocked by the DNS list"
		if r.Rule != "" {
			it.Why = fmt.Sprintf("Blocked by %q on your DNS lists.", r.Rule)
		}
	case "cached":
		it.Why = "Answered from memory. Not on any block list."
	case "local":
		it.Why = "One of your own names, answered by Gatehouse."
	case "rate_limited":
		it.Verdict = "warning"
		it.Why = "Slowed down: this device asked too many times a second."
	case "failed":
		it.Verdict = "warning"
		it.Why = "No answer came back from the DNS servers."
	default:
		it.Why = "Not on any block list. Answered by " + orWord(r.Upstream, "an upstream DNS server") + "."
	}
	return it
}

func orWord(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (a *App) searchFW(text, verdict string, since, before int64, limit int) []LogItem {
	return a.searchFWf(fwFilter{Text: text, Verdict: verdict, Since: since, Before: before, Limit: limit})
}

func (a *App) searchEvents(text string, since, before int64, limit int) []LogItem {
	where := []string{"1=1"}
	var args []any
	if since > 0 {
		where, args = append(where, "ts>=?"), append(args, since)
	}
	if before > 0 {
		where, args = append(where, "ts<=?"), append(args, before)
	}
	if strings.TrimSpace(text) != "" {
		l := like(text)
		where, args = append(where, "(title LIKE ? OR detail LIKE ? OR area LIKE ? OR actor LIKE ?)"), append(args, l, l, l, l)
	}
	args = append(args, limit)
	rows, err := a.Logs.Query(`SELECT id,ts,area,severity,actor,title,detail FROM events WHERE `+strings.Join(where, " AND ")+` ORDER BY ts DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []LogItem
	for rows.Next() {
		var id, ts int64
		var area, sev, actor, title, detail string
		if rows.Scan(&id, &ts, &area, &sev, &actor, &title, &detail) != nil {
			continue
		}
		v := "info"
		if sev == "warning" || sev == "error" || sev == "critical" {
			v = "warning"
		}
		out = append(out, LogItem{Key: "e" + strconv.FormatInt(id, 10), TS: ts, Kind: "event", Verdict: v, Who: actor, What: title, Detail: area, Why: detail})
	}
	return out
}

// logSearch merges the sources. It asks each for one page and keeps the newest of all.
func (a *App) logSearch(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	q := r.URL.Query()
	text, kind, verdict := strings.TrimSpace(q.Get("q")), q.Get("kind"), q.Get("verdict")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 300 {
		limit = 100
	}
	var since int64
	if h, err := strconv.Atoi(q.Get("hours")); err == nil && h > 0 {
		since = time.Now().Add(-time.Duration(h) * time.Hour).Unix()
	}
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	ff := logParams(r)
	if ff.Since > 0 {
		since = ff.Since
	}
	if ff.Device != "" && text == "" {
		text = ff.Device
	}
	if ff.firewallOnly() && kind == "" {
		kind = "firewall"
	}
	var items []LogItem
	if kind == "" || kind == "dns" {
		f := dnssrv.Filter{Text: text, Since: since, BeforeTS: before, Limit: limit}
		switch verdict {
		case "blocked":
			f.Result = "blocked"
		case "allowed":
			f.NotResult = "blocked"
		}
		if rows, err := a.QLog.Search(f); err == nil {
			for _, r := range rows {
				items = append(items, dnsItem(r))
			}
		}
	}
	if kind == "" || kind == "firewall" {
		ff.Text, ff.Verdict, ff.Since, ff.Before, ff.Limit = text, verdict, since, before, limit
		if ff.Device != "" && ff.Text == ff.Device {
			ff.Text = ""
		}
		items = append(items, a.searchFWf(ff)...)
	}
	if (kind == "" || kind == "event") && verdict == "" {
		items = append(items, a.searchEvents(text, since, before, limit)...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].TS > items[j].TS })
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	if items == nil {
		items = []LogItem{}
	}
	api.JSON(w, 200, map[string]any{"items": items, "more": more,
		"record_allowed": a.Config.Setting("fw.log_allowed", "1") == "1", "record_blocked": a.Config.Setting("fw.log_drops", "1") == "1"})
}

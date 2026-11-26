package app

import (
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
	"nopewarden/internal/flowexp"
)

// Find traffic: pick a source and a destination (a device, an address, a network, a name or a port)
// and see what became of the connections between them: allowed or blocked, which rule decided, and
// whether the other side answered. It is built on the firewall log and the connection tracker
// (connstats.go), which keeps how each connection ended and the bytes each way.

const findMax = 200

// endSpec is what a person typed for one end of a connection.
type endSpec struct {
	raw    string
	addr   string
	prefix netip.Prefix
	zone   string
	name   string
	port   int
}

func (e endSpec) empty() bool { return e.raw == "" }

// parseEnd works out what was typed: an address, a range, a network name, or part of a device or
// site name. A trailing :port or "tcp/443" is split off for the destination.
func (a *App) parseEnd(v string, withPort bool) endSpec {
	v = strings.TrimSpace(v)
	e := endSpec{raw: v}
	if v == "" {
		return e
	}
	if withPort {
		if i := strings.LastIndex(v, ":"); i > 0 && strings.Count(v, ":") == 1 {
			if n, err := strconv.Atoi(strings.TrimSpace(v[i+1:])); err == nil && n > 0 && n < 65536 {
				e.port, v = n, strings.TrimSpace(v[:i])
			}
		}
	}
	if ad, err := netip.ParseAddr(v); err == nil {
		e.addr = ad.Unmap().String()
		return e
	}
	if pf, err := netip.ParsePrefix(v); err == nil {
		e.prefix = pf.Masked()
		return e
	}
	zones := a.wanZoneNames()
	zones["Router"] = true
	if rows, err := a.Config.Query(`SELECT name FROM zones`); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			zones[n] = true
		}
		rows.Close()
	}
	for z := range zones {
		if strings.EqualFold(z, v) {
			e.zone = z
			return e
		}
	}
	e.name = v
	return e
}

// findFilter turns the form into a log filter.
func (a *App) findFilter(src, dst endSpec, port int, result string, since, until int64) fwFilter {
	f := fwFilter{Since: since, Until: until, Port: port}
	if dst.port > 0 && f.Port == 0 {
		f.Port = dst.port
	}
	f.SrcAddr, f.SrcZone, f.DstAddr, f.DstZone = src.addr, src.zone, dst.addr, dst.zone
	f.Device, f.Dest = src.name, dst.name
	switch result {
	case "allowed":
		f.Verdict = "allowed"
	case "blocked":
		f.Verdict = "blocked"
	case "noreply":
		f.Verdict, f.Outcome = "allowed", outTimedOut
	case "reset":
		f.Verdict, f.Outcome = "allowed", outReset
	case "open":
		f.Verdict, f.Outcome = "allowed", outOpen
	}
	if src.prefix.IsValid() || dst.prefix.IsValid() {
		f.Match = func(from, to string) bool {
			if src.prefix.IsValid() {
				ad, err := netip.ParseAddr(from)
				if err != nil || !src.prefix.Contains(ad.Unmap()) {
					return false
				}
			}
			if dst.prefix.IsValid() {
				ad, err := netip.ParseAddr(to)
				if err != nil || !dst.prefix.Contains(ad.Unmap()) {
					return false
				}
			}
			return true
		}
	}
	return f
}

func findWindow(r *http.Request) (since int64, words string) {
	q := r.URL.Query()
	if v, err := strconv.ParseInt(q.Get("since"), 10, 64); err == nil && v > 0 {
		return v, "since you started watching"
	}
	m, _ := strconv.Atoi(q.Get("minutes"))
	switch {
	case m <= 0 || m > 24*60:
		m = 60
	}
	switch m {
	case 15:
		words = "the last 15 minutes"
	case 60:
		words = "the last hour"
	case 1440:
		words = "the last 24 hours"
	default:
		words = fmt.Sprintf("the last %d minutes", m)
	}
	return time.Now().Add(-time.Duration(m) * time.Minute).Unix(), words
}

func endWords(e endSpec, any string) string {
	if e.empty() {
		return any
	}
	return e.raw
}

// findSentence says in plain words what the search turned up.
func findSentence(items []LogItem, src, dst endSpec, port int, when string, recAllowed, recBlocked bool) string {
	from := endWords(src, "anything")
	to := endWords(dst, "anywhere")
	if port > 0 {
		to += " on port " + strconv.Itoa(port)
	}
	if len(items) == 0 {
		s := fmt.Sprintf("No connections from %s to %s in %s.", from, to, when)
		switch {
		case !recAllowed && !recBlocked:
			s += " Neither allowed nor blocked connections are being recorded, so nothing can show up. Turn recording on in Logs, Settings."
		case !recAllowed:
			s += " Allowed connections are not being recorded, so only blocked ones can show up. Turn that on in Logs, Settings to see the rest."
		default:
			s += " Either nothing tried, or it never reached this firewall (a device on the same switch talks directly)."
		}
		return s
	}
	var allowed, blocked, done, open, none, reset int
	rules := map[string]int{}
	for _, it := range items {
		if it.Verdict == "blocked" {
			blocked++
		} else {
			allowed++
			switch it.Outcome {
			case outCompleted:
				done++
			case outOpen:
				open++
			case outTimedOut:
				none++
			case outReset:
				reset++
			}
		}
		if it.RuleName != "" {
			rules[it.RuleName]++
		}
	}
	n := len(items)
	noun := "connections"
	if n == 1 {
		noun = "connection"
	}
	more := ""
	if n >= findMax {
		more = fmt.Sprintf("the newest %d", findMax)
	} else {
		more = strconv.Itoa(n)
	}
	s := fmt.Sprintf("Found %s %s from %s to %s in %s.", more, noun, from, to, when)
	var parts []string
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", blocked))
	}
	if allowed > 0 {
		parts = append(parts, fmt.Sprintf("%d allowed", allowed))
	}
	s += " " + strings.Join(parts, ", ") + "."
	var ends []string
	if done > 0 {
		ends = append(ends, fmt.Sprintf("%d completed", done))
	}
	if open > 0 {
		ends = append(ends, fmt.Sprintf("%d still open", open))
	}
	if none > 0 {
		ends = append(ends, fmt.Sprintf("%d got no reply", none))
	}
	if reset > 0 {
		ends = append(ends, fmt.Sprintf("%d were reset", reset))
	}
	if len(ends) > 0 {
		s += " Of the allowed ones, " + strings.Join(ends, ", ") + "."
	}
	if none > 0 {
		s += " \"No reply\" means the connection was let through but the other side never answered, so look at the far end, not the firewall."
	}
	if len(rules) == 1 {
		for r := range rules {
			s += fmt.Sprintf(" One rule decided them all: %s.", r)
		}
	} else if len(rules) > 1 {
		type kv struct {
			k string
			v int
		}
		var l []kv
		for k, v := range rules {
			l = append(l, kv{k, v})
		}
		sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v || (l[i].v == l[j].v && l[i].k < l[j].k) })
		s += fmt.Sprintf(" Most were decided by %s.", l[0].k)
	}
	return s
}

func (a *App) findTraffic(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	q := r.URL.Query()
	src := a.parseEnd(q.Get("src"), false)
	dst := a.parseEnd(q.Get("dst"), true)
	port, _ := strconv.Atoi(q.Get("port"))
	result := q.Get("result")
	since, when := findWindow(r)
	f := a.findFilter(src, dst, port, result, since, 0)
	f.Limit = findMax
	if svc := strings.TrimSpace(q.Get("service")); svc != "" {
		f.Service = svc
	}
	items := a.searchFWf(f)
	if items == nil {
		items = []LogItem{}
	}
	loaded := a.fwLoaded.Load()
	for i := range items {
		if loaded > 0 && items[i].TS < loaded && items[i].Verdict == "allowed" && (items[i].Outcome == outOpen || items[i].Outcome == "") {
			items[i].BeforeChange = true
		}
	}
	recA := a.Config.Setting("fw.log_allowed", "1") == "1"
	recB := a.Config.Setting("fw.log_drops", "1") == "1"
	var notes []string
	blocked := 0
	before := 0
	for _, it := range items {
		if it.Verdict == "blocked" {
			blocked++
		}
		if it.BeforeChange {
			before++
		}
	}
	if blocked > 0 {
		notes = append(notes, "Blocked connections are recorded at up to about 20 a second. In a flood some are skipped, so the count of blocked ones is a floor, not an exact number.")
	}
	if before > 0 {
		notes = append(notes, fmt.Sprintf("%d of these were already open when the rules last changed. A connection that is already open keeps working under the old rules. Start it again, or end it from Open connections, to test the new rules.", before))
	}
	notes = append(notes, "How a connection ended is only tracked for IPv4 TCP and UDP connections. Others show no outcome.")
	api.JSON(w, 200, map[string]any{
		"items": items, "sentence": findSentence(items, src, dst, port, when, recA, recB), "notes": notes,
		"now": time.Now().Unix(), "since": since, "full": len(items) >= findMax, "max": findMax,
		"record_allowed": recA, "record_blocked": recB,
	})
}

// openConn is one connection the kernel is following now.
type openConn struct {
	Proto     string `json:"proto"`
	Src       string `json:"src"`
	SrcName   string `json:"src_name,omitempty"`
	SPort     int    `json:"sport"`
	Dst       string `json:"dst"`
	DstName   string `json:"dst_name,omitempty"`
	DPort     int    `json:"dport"`
	Service   string `json:"service,omitempty"`
	State     string `json:"state"`
	Unreplied bool   `json:"unreplied,omitempty"`
	BytesOut  int64  `json:"bytes_out"`
	BytesIn   int64  `json:"bytes_in"`
}

func (a *App) openConnections(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	if a.Net == nil {
		api.JSON(w, 200, map[string]any{"items": []openConn{}, "error": "This machine cannot read its connection list."})
		return
	}
	q := r.URL.Query()
	src := a.parseEnd(q.Get("src"), false)
	dst := a.parseEnd(q.Get("dst"), true)
	port, _ := strconv.Atoi(q.Get("port"))
	if port == 0 {
		port = dst.port
	}
	a.Net.FlowAcct(true)
	text, err := a.Net.ConntrackDump()
	if err != nil {
		api.JSON(w, 200, map[string]any{"items": []openConn{}, "error": err.Error()})
		return
	}
	ok := func(e endSpec, addr netip.Addr, name string) bool {
		switch {
		case e.addr != "":
			return addr.String() == e.addr
		case e.prefix.IsValid():
			return e.prefix.Contains(addr)
		case e.zone != "":
			return a.deviceZone(addr.String()) == e.zone
		case e.name != "":
			return strings.Contains(strings.ToLower(name+" "+addr.String()), strings.ToLower(e.name))
		}
		return true
	}
	out := []openConn{}
	for _, cn := range flowexp.ParseConntrack(text) {
		p := "tcp"
		switch cn.Proto {
		case 17:
			p = "udp"
		case 6:
		default:
			continue
		}
		sn, dn := a.deviceName(cn.Src.String()), a.names.get(cn.Dst.String())
		if !ok(src, cn.Src, sn) || !ok(dst, cn.Dst, dn) {
			continue
		}
		if port > 0 && int(cn.DPort) != port {
			continue
		}
		oc := openConn{Proto: p, Src: cn.Src.String(), SrcName: sn, SPort: int(cn.SPort), Dst: cn.Dst.String(), DstName: dn, DPort: int(cn.DPort),
			Service: serviceName(p, int(cn.DPort)), State: cn.State, Unreplied: cn.Unreplied, BytesOut: -1, BytesIn: -1}
		if cn.OBytes > 0 || cn.RBytes > 0 {
			oc.BytesOut, oc.BytesIn = int64(cn.OBytes), int64(cn.RBytes)
		}
		out = append(out, oc)
		if len(out) >= 500 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BytesOut+out[i].BytesIn > out[j].BytesOut+out[j].BytesIn })
	api.JSON(w, 200, map[string]any{"items": out, "full": len(out) >= 500, "counting": a.Config.Setting("fw.log_allowed", "1") == "1"})
}

func (a *App) endConnection(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Proto string `json:"proto"`
		Src   string `json:"src"`
		SPort int    `json:"sport"`
		Dst   string `json:"dst"`
		DPort int    `json:"dport"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	if a.Net == nil {
		api.Error(w, 400, "This machine cannot end connections.")
		return
	}
	if err := a.Net.ConntrackEnd(strings.ToLower(in.Proto), in.Src, in.SPort, in.Dst, in.DPort); err != nil {
		api.Error(w, 400, err.Error())
		return
	}
	a.auditEvent(c.User.Username, "info", "Ended a connection", fmt.Sprintf("%s %s:%d to %s:%d", strings.ToUpper(in.Proto), in.Src, in.SPort, in.Dst, in.DPort))
	api.JSON(w, 200, map[string]any{"ok": true})
}

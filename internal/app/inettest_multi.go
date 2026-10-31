package app

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// "Test my internet" with more than one connection: one block per connection, each with its own
// cable, modem, provider, names and speed rows, and a line above them saying how the connections
// share the work. A backup that waits on purpose is not a fault.

type multiInput struct {
	Links []testLink
	NAT   []natLink
	Mode  string // failover or balance
}

type connView struct {
	Name     string     `json:"name"`
	Port     string     `json:"port"`
	Role     string     `json:"role"` // main, backup or shared
	InUse    bool       `json:"in_use"`
	Idle     bool       `json:"idle"` // a backup waiting its turn, in good order
	State    string     `json:"state"`
	Headline string     `json:"headline"`
	Rows     []testStep `json:"rows"`
	Next     string     `json:"next,omitempty"` // for the step that has the problem, this connection only
}

type multiView struct {
	OK      bool       `json:"ok"`
	State   string     `json:"state"` // ok, warn or bad
	Mode    string     `json:"mode"`
	Summary string     `json:"summary"`
	Note    string     `json:"note,omitempty"`
	Conns   []connView `json:"conns"`
}

func worse(a, b string) string {
	rank := map[string]int{"": 0, "skip": 0, "ok": 1, "warn": 2, "bad": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func mbit(bps float64) string {
	m := bps / 1e6
	if m >= 100 {
		return strconv.Itoa(int(m + 0.5))
	}
	return strconv.FormatFloat(float64(int(m*10+0.5))/10, 'f', -1, 64)
}

func agoWords(sec int64) string {
	switch {
	case sec < 90:
		return "just now"
	case sec < 5400:
		return fmt.Sprintf("%d minutes ago", (sec+30)/60)
	case sec < 129600:
		return fmt.Sprintf("%d hours ago", (sec+1800)/3600)
	}
	return fmt.Sprintf("%d days ago", (sec+43200)/86400)
}

func buildMultiTest(in multiInput) multiView {
	mode := in.Mode
	if mode != "balance" {
		mode = "failover"
	}
	v := multiView{Mode: mode, Conns: []connView{}}
	var main string
	for _, l := range in.Links {
		if !l.Backup && main == "" {
			main = l.Name
		}
	}
	if main == "" && len(in.Links) > 0 {
		main = in.Links[0].Name
	}
	var inUse, good, broken []string
	anyActive := false
	for _, l := range in.Links {
		if l.State.Active {
			anyActive = true
		}
	}
	for _, l := range in.Links {
		var nat []natLink
		for _, n := range in.NAT {
			if n.Name == l.Name {
				nat = append(nat, n)
			}
		}
		rows := buildTest(testInput{Links: []testLink{l}, NAT: nat, DNSOK: l.ViaOK, DNSMs: l.ViaMs, DNSErr: l.ViaErr, Via: true})
		for i := range rows {
			if rows[i].ID == "dns" && !l.ViaTried && rows[i].State != "skip" {
				rows[i] = testStep{ID: "dns", Title: "Names (DNS)", State: "skip", Detail: "Not tested through this connection."}
			}
		}
		sp := testStep{ID: "speed", Title: "Internet speed", State: "skip", Detail: "No speed test yet. Run one on the Speed test page."}
		if l.SpeedKnown {
			sp = testStep{ID: "speed", Title: "Internet speed", State: "ok",
				Detail: fmt.Sprintf("%s Mbit/s down, %s up, measured %s.", mbit(l.SpeedDown), mbit(l.SpeedUp), agoWords(time.Now().Unix()-l.SpeedAt))}
		}
		rows = append(rows, sp)

		cv := connView{Name: l.Name, Port: l.Port, Rows: rows, InUse: l.State.Active}
		for _, r := range rows {
			cv.State = worse(cv.State, r.State)
			if cv.Next == "" && (r.State == "bad" || r.State == "warn") && r.Next != "" {
				cv.Next = r.Next
			}
		}
		if cv.State == "" {
			cv.State = "ok"
		}
		switch {
		case mode == "balance":
			cv.Role = "shared"
		case l.Name == main:
			cv.Role = "main"
		default:
			cv.Role = "backup"
		}
		// A backup that cannot take over is worth a look, but it is not an outage while
		// something else is carrying the traffic.
		if cv.Role == "backup" && !cv.InUse && cv.State == "bad" && anyActive {
			cv.State = "warn"
		}
		switch {
		case cv.InUse && cv.State == "ok":
			cv.Headline = "Carrying your traffic."
		case cv.InUse:
			cv.Headline = "Carrying your traffic, with a problem to look at."
		case cv.Role == "backup" && cv.State == "ok":
			cv.Idle, cv.Headline = true, "In good order. Waiting as the backup, so no traffic goes through it now."
		case cv.Role == "backup":
			cv.Headline = "Has a problem, so it could not take over right now."
		case cv.State == "ok":
			cv.Headline = "In good order."
		default:
			cv.Headline = "Not working."
		}
		if l.State.Active {
			inUse = append(inUse, l.Name)
		}
		if cv.State == "ok" {
			good = append(good, l.Name)
		} else if cv.State == "bad" || (cv.State == "warn" && cv.Role != "backup") {
			broken = append(broken, l.Name)
		}
		v.Conns = append(v.Conns, cv)
	}

	var backups []string
	for _, c := range v.Conns {
		if c.Role == "backup" {
			backups = append(backups, c.Name)
		}
	}
	allBad := true
	for _, c := range v.Conns {
		if c.State != "bad" {
			allBad = false
		}
	}
	switch {
	case len(inUse) == 0:
		v.Summary = "No connection is carrying traffic right now."
	case mode == "balance":
		v.Summary = "Load balancing: traffic is shared between " + joinNames(inUse) + "."
	case len(inUse) == 1 && inUse[0] == main:
		v.Summary = "Failover: " + main + " is carrying your traffic."
		if len(backups) > 0 {
			v.Summary += " " + joinNames(backups) + " waits as the backup."
		}
	default:
		v.Summary = "Failover: " + joinNames(inUse) + " is carrying your traffic because " + main + " is not working."
	}
	switch {
	case allBad:
		v.State = "bad"
	default:
		v.State = "ok"
		for _, c := range v.Conns {
			if c.State == "bad" || c.State == "warn" {
				v.State = "warn"
			}
		}
	}
	v.OK = v.State != "bad"
	if v.State == "warn" && len(good) > 0 && len(broken) > 0 {
		v.Note = "Your internet still works through " + joinNames(good) + ". " + joinNames(broken) + " needs a look."
	}
	return v
}

// fillPerConnection sends a name lookup out through each connection's own device and reads each
// connection's last speed test. It runs only when there is more than one connection.
func (a *App) fillPerConnection(in *testInput) {
	var wg sync.WaitGroup
	for i := range in.Links {
		l := &in.Links[i]
		if a.Config != nil {
			var down, up float64
			var ts int64
			if err := a.Config.QueryRow(`SELECT down_bps,up_bps,ts FROM speedtests WHERE link=? AND error='' AND down_bps>0 ORDER BY ts DESC LIMIT 1`, l.Name).Scan(&down, &up, &ts); err == nil {
				l.SpeedKnown, l.SpeedDown, l.SpeedUp, l.SpeedAt = true, down, up, ts
			}
		}
		if a.Net == nil || l.Dev == "" || !l.Have || !l.State.Checked || l.State.Health == "no-link" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := a.Net.Diag("lookup:"+l.Dev, "example.com")
			l.ViaTried = true
			switch {
			case err != nil:
				l.ViaErr = err.Error()
			case strings.HasPrefix(out, "ok|"):
				p := strings.Split(out, "|")
				l.ViaOK = true
				if len(p) > 1 {
					l.ViaMs, _ = strconv.Atoi(p[1])
				}
			case strings.HasPrefix(out, "fail|"):
				l.ViaErr = strings.TrimPrefix(out, "fail|")
			default:
				l.ViaTried = false
			}
		}()
	}
	wg.Wait()
}

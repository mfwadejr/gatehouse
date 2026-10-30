package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/miekg/dns"

	"gatehouse/internal/api"
	"gatehouse/internal/netd"
	"gatehouse/internal/wanmon"
)

// "Test my internet": the cable, the modem or gateway, the provider connection and DNS, in turn,
// stopping at the first thing that is wrong and saying what to try.

type testStep struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"` // ok, warn, bad, skip
	Detail string `json:"detail"`
	Next   string `json:"next,omitempty"`
}

type testLink struct {
	Name      string
	Port      string
	PortKnown bool
	Cable     bool
	Have      bool // the monitor has a state for it
	State     wanmon.State
}

type testInput struct {
	Links      []testLink
	NAT        []natLink
	DNSOK      bool
	DNSMs      int
	DNSErr     string
	UpstreamOK bool
	UpstreamMs int
	UpstreamBy string
	UpstreamEr string
}

func ms(d time.Duration) int {
	v := int(d / time.Millisecond)
	if v < 1 && d > 0 {
		v = 1
	}
	return v
}

// buildTest decides each step. Later steps are skipped once an earlier one fails, because their
// result would only be a side effect of the first problem.
func buildTest(in testInput) []testStep {
	steps := []testStep{}
	add := func(s testStep) { steps = append(steps, s) }
	failed := false
	skip := func(id, title string) {
		add(testStep{ID: id, Title: title, State: "skip", Detail: "Not checked, because an earlier step needs fixing first."})
	}

	if len(in.Links) == 0 {
		add(testStep{ID: "cable", Title: "Cable to your modem", State: "bad", Detail: "No internet connection is set up yet.", Next: "Open the Internet page and add a connection, choosing the port your modem is plugged into."})
		skip("gateway", "Modem or gateway")
		skip("provider", "Your provider")
		skip("dns", "Names (DNS)")
		return steps
	}

	// Step 1: the cable. Only the connections that have no cable at all count as failed here.
	var noCable []string
	for _, l := range in.Links {
		if l.PortKnown && !l.Cable {
			noCable = append(noCable, fmt.Sprintf("%s (port %s)", l.Name, l.Port))
		}
	}
	if len(noCable) == len(in.Links) {
		failed = true
		add(testStep{ID: "cable", Title: "Cable to your modem", State: "bad", Detail: "No cable signal on " + strings.Join(noCable, ", ") + ".",
			Next: "Check the cable is firmly in the port on Gatehouse and in the modem, that the modem has power, and try another cable."})
	} else if len(noCable) > 0 {
		add(testStep{ID: "cable", Title: "Cable to your modem", State: "warn", Detail: "No cable signal on " + strings.Join(noCable, ", ") + ". The other connection has one.",
			Next: "If you expect this one to work, check its cable and the modem's power."})
	} else {
		add(testStep{ID: "cable", Title: "Cable to your modem", State: "ok", Detail: "A cable is connected."})
	}

	// Step 2: the next device along.
	if failed {
		skip("gateway", "Modem or gateway")
	} else {
		var bad, good []string
		unknown := 0
		for _, l := range in.Links {
			switch {
			case !l.Have || !l.State.Checked:
				unknown++
			case l.State.Health == "no-link":
				bad = append(bad, l.Name)
			default:
				t := "answers"
				if l.State.GwRTT > 0 {
					t = fmt.Sprintf("answers in %d ms", ms(l.State.GwRTT))
				}
				good = append(good, fmt.Sprintf("%s %s", l.Name, t))
			}
		}
		switch {
		case len(good) == 0 && len(bad) > 0:
			failed = true
			add(testStep{ID: "gateway", Title: "Modem or gateway", State: "bad", Detail: "Nothing answers on " + strings.Join(bad, ", ") + " although the cable is connected.",
				Next: "Restart the modem: unplug its power for 30 seconds, plug it back in and wait two minutes. If your provider gives a login (PPPoE), check it on the Internet page."})
		case len(good) == 0:
			add(testStep{ID: "gateway", Title: "Modem or gateway", State: "warn", Detail: "Gatehouse has not finished checking yet.", Next: "Run the test again in a minute."})
		default:
			d := "The next device answers: " + strings.Join(good, "; ") + "."
			st := "ok"
			if len(bad) > 0 {
				st, d = "warn", d+" No answer on "+strings.Join(bad, ", ")+"."
			}
			add(testStep{ID: "gateway", Title: "Modem or gateway", State: st, Detail: d})
		}
	}

	// Step 3: the provider.
	if failed {
		skip("provider", "Your provider")
	} else {
		var good, bad []string
		for _, l := range in.Links {
			if !l.Have || !l.State.Checked || l.State.Health == "no-link" {
				continue
			}
			if l.State.Health == "no-internet" {
				bad = append(bad, l.Name)
			} else if l.State.Health == "working" {
				d := l.Name
				if l.State.RTT > 0 {
					d += fmt.Sprintf(" reaches the internet in %d ms", ms(l.State.RTT))
				}
				if l.State.Loss > 0 {
					d += fmt.Sprintf(", %d %% of tests lost", l.State.Loss)
				}
				good = append(good, d)
			}
		}
		switch {
		case len(good) == 0 && len(bad) > 0:
			failed = true
			add(testStep{ID: "provider", Title: "Your provider", State: "bad", Detail: "The modem answers, but nothing beyond it does on " + strings.Join(bad, ", ") + ".",
				Next: "This is usually your provider or the modem's own status lights. Look at the modem's lights, restart it, and check your provider's outage page from your phone on mobile data."})
		case len(good) == 0:
			add(testStep{ID: "provider", Title: "Your provider", State: "warn", Detail: "Not known yet.", Next: "Run the test again in a minute."})
		default:
			st, d, next := "ok", strings.Join(good, "; ")+".", ""
			if len(bad) > 0 {
				st, d = "warn", d+" No internet on "+strings.Join(bad, ", ")+"."
			}
			for _, n := range in.NAT {
				if n.Blocked {
					st = "warn"
					d += " " + n.Title + " on " + n.Name + ": connections from outside cannot reach you."
					next = "See the Internet page for what to do about it."
				}
			}
			add(testStep{ID: "provider", Title: "Your provider", State: st, Detail: d, Next: next})
		}
	}

	// Step 4: names.
	if failed {
		skip("dns", "Names (DNS)")
	} else {
		switch {
		case in.DNSOK:
			d := fmt.Sprintf("Gatehouse looked up a website name in %d ms.", in.DNSMs)
			add(testStep{ID: "dns", Title: "Names (DNS)", State: "ok", Detail: d})
		case in.UpstreamOK:
			add(testStep{ID: "dns", Title: "Names (DNS)", State: "bad", Detail: "The upstream DNS server " + in.UpstreamBy + " answers, but Gatehouse's own lookup failed: " + in.DNSErr + ".",
				Next: "Open the DNS page and check that DNS is on and that nothing blocks this name."})
		default:
			add(testStep{ID: "dns", Title: "Names (DNS)", State: "bad", Detail: "Website names do not resolve: " + orWord(in.DNSErr, "no answer") + ".",
				Next: "Open the DNS page and press Test beside each upstream server. If none work, choose a different provider such as Cloudflare or Quad9."})
		}
	}
	return steps
}

func (a *App) lookupName(name string, qtype uint16) (*dns.Msg, time.Duration, error) {
	if a.DNS == nil {
		return nil, 0, fmt.Errorf("DNS is not running")
	}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = true
	t0 := time.Now()
	resp := a.DNS.Resolve("127.0.0.1", m)
	if resp == nil {
		return nil, time.Since(t0), fmt.Errorf("no answer")
	}
	return resp, time.Since(t0), nil
}

func (a *App) gatherTest() testInput {
	in := testInput{NAT: a.natReport()}
	var st map[string]wanmon.State
	if a.Net != nil {
		st, _ = a.Net.WanStatus()
	}
	pl := map[string]bool{}
	for _, p := range ports() {
		pl[p["name"].(string)], _ = p["link"].(bool)
	}
	if rows, err := a.Config.Query(`SELECT name,iface FROM wan_links WHERE enabled=1 ORDER BY priority,id`); err == nil {
		for rows.Next() {
			var name, iface string
			rows.Scan(&name, &iface)
			l := testLink{Name: name, Port: iface}
			l.Cable, l.PortKnown = pl[iface]
			l.State, l.Have = st[name]
			in.Links = append(in.Links, l)
		}
		rows.Close()
	}
	resp, d, err := a.lookupName("example.com", dns.TypeA)
	switch {
	case err != nil:
		in.DNSErr = err.Error()
	case resp.Rcode != dns.RcodeSuccess || len(resp.Answer) == 0:
		in.DNSErr = dns.RcodeToString[resp.Rcode]
	default:
		in.DNSOK, in.DNSMs = true, ms(d)
	}
	if !in.DNSOK && a.DNS != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		q := new(dns.Msg)
		q.SetQuestion("example.com.", dns.TypeA)
		r, u, rt, e := a.DNS.Pool().Exchange(ctx, q)
		cancel()
		if e == nil && r != nil && len(r.Answer) > 0 {
			in.UpstreamOK, in.UpstreamMs = true, ms(rt)
			if u != nil {
				in.UpstreamBy = u.Name
			}
		} else if e != nil {
			in.UpstreamEr = e.Error()
		}
	}
	return in
}

func (a *App) registerDiagRoutes() {
	s := a.API
	s.HandleAdmin("POST /api/diag/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		steps := buildTest(a.gatherTest())
		ok := true
		for _, st := range steps {
			if st.State == "bad" {
				ok = false
			}
		}
		api.JSON(w, 200, map[string]any{"ok": ok, "steps": steps, "at": time.Now().Unix()})
	})
	// ping and trace run in the privileged helper, which refuses anything but a plain name or address.
	s.HandleAdmin("POST /api/diag/run", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Kind   string `json:"kind"`
			Target string `json:"target"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Target = strings.TrimSpace(in.Target)
		if !netd.DiagTargetOK(in.Target) {
			fail(w, fmt.Errorf("enter a website name or an address, such as example.com or 8.8.8.8"))
			return
		}
		if a.Net == nil {
			fail(w, fmt.Errorf("the network helper is not running"))
			return
		}
		out, err := a.Net.Diag(in.Kind, in.Target)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]string{"output": out})
	})
	s.HandleAdmin("POST /api/diag/dns", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if !netd.DiagTargetOK(in.Name) {
			fail(w, fmt.Errorf("enter a website name such as example.com"))
			return
		}
		qt, good := map[string]uint16{"A": dns.TypeA, "AAAA": dns.TypeAAAA, "MX": dns.TypeMX, "TXT": dns.TypeTXT, "NS": dns.TypeNS, "CNAME": dns.TypeCNAME}[strings.ToUpper(orWord(in.Type, "A"))]
		if !good {
			fail(w, fmt.Errorf("choose A, AAAA, MX, TXT, NS or CNAME"))
			return
		}
		resp, d, err := a.lookupName(in.Name, qt)
		if err != nil {
			fail(w, err)
			return
		}
		ans := []string{}
		for _, rr := range resp.Answer {
			ans = append(ans, rr.String())
		}
		api.JSON(w, 200, map[string]any{"status": dns.RcodeToString[resp.Rcode], "ms": ms(d), "answers": ans})
	})
}

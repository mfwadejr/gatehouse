package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/miekg/dns"

	"nopewarden/internal/api"
	dnssrv "nopewarden/internal/dns"
)

// Names that use a different server. The list is kept as one setting and goes live with the other DNS
// changes (it waits for Review like them). The test button acts at once.

const settingDNSForwards = "dns.forwards"

func (a *App) dnsForwards() []dnssrv.FwdRule {
	var rules []dnssrv.FwdRule
	json.Unmarshal([]byte(a.setting(settingDNSForwards, "")), &rules)
	out := rules[:0]
	for _, r := range rules {
		if c, err := dnssrv.CleanFwdRule(r); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func (a *App) loadForwards() { a.DNS.SetForwards(a.dnsForwards()) }

func (a *App) registerDNSForwardRoutes() {
	s := a.API
	s.Handle("GET /api/dns/forwards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		rules := a.dnsForwards()
		if rules == nil {
			rules = []dnssrv.FwdRule{}
		}
		api.JSON(w, 200, map[string]any{"rules": rules})
	})
	s.HandleAdmin("PUT /api/dns/forwards", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Rules []dnssrv.FwdRule `json:"rules"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if len(in.Rules) > 50 {
			fail(w, errors.New("that is more rules than NopeWarden will keep (50)"))
			return
		}
		clean := []dnssrv.FwdRule{}
		seen := map[string]bool{}
		for _, r := range in.Rules {
			c, err := dnssrv.CleanFwdRule(r)
			if err != nil {
				fail(w, err)
				return
			}
			if seen[c.Domain] {
				fail(w, errors.New(c.Domain+" is in the list twice"))
				return
			}
			seen[c.Domain] = true
			clean = append(clean, c)
		}
		b, _ := json.Marshal(clean)
		if err := a.Config.SetSetting(settingDNSForwards, string(b)); err != nil {
			fail(w, err)
			return
		}
		if err := a.applyDNSHeld(); err != nil {
			fail(w, err)
			return
		}
		ok(w)
	})
	// Test asks one server about the domain and says in words what came back.
	s.HandleAdmin("POST /api/dns/forwards/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in dnssrv.FwdRule
		if !api.Decode(w, r, &in) {
			return
		}
		if len(in.Servers) > 1 {
			in.Servers = in.Servers[:1]
		}
		rule, err := dnssrv.CleanFwdRule(in)
		if err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		q := new(dns.Msg)
		q.SetQuestion(dns.Fqdn(rule.Domain), dns.TypeSOA)
		cl := &dns.Client{Net: "udp", Timeout: 4 * time.Second}
		t0 := time.Now()
		resp, _, err := cl.ExchangeContext(ctx, q, rule.Servers[0])
		took := time.Since(t0).Milliseconds()
		res := map[string]any{"ms": took}
		switch {
		case err != nil:
			msg := err.Error()
			if strings.Contains(msg, "timeout") || strings.Contains(msg, "i/o") {
				res["verdict"] = "That server did not answer. Check its address, that it is switched on, and that this box can reach it."
			} else {
				res["verdict"] = "That server could not be reached: " + msg + "."
			}
		case resp.Rcode == dns.RcodeSuccess:
			res["ok"] = true
			res["verdict"] = "The server answered and knows this domain."
		case resp.Rcode == dns.RcodeNameError:
			res["ok"] = true
			res["verdict"] = "The server answered but says this domain does not exist. It is reachable; check that the domain is spelled the way that server knows it."
		case resp.Rcode == dns.RcodeRefused:
			res["verdict"] = "The server answered but refuses questions from this box. Its settings may only allow certain addresses."
		default:
			res["verdict"] = "The server answered with an error (" + dns.RcodeToString[resp.Rcode] + ")."
		}
		api.JSON(w, 200, res)
	})
}

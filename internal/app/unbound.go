package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"nopewarden/internal/api"
	dnssrv "nopewarden/internal/dns"
	"nopewarden/internal/netd"
)

// "Resolve directly" (Unbound). With it on, NopeWarden's DNS server asks Unbound, which looks names
// up from the root servers and checks DNSSEC signatures, instead of asking encrypted forwarders.
// If Unbound stops answering, NopeWarden switches to the encrypted forwarders by itself, says so in
// an alert, and switches back once Unbound has answered well for a while.

const (
	resolveModeSetting = "dns.resolve_mode" // forward (default) or direct
	dnssecSetting      = "dns.dnssec"       // 1 (default) or 0
	ubFailsToFallback  = 3                  // probes in a row that fail before falling back
	ubOKsToReturn      = 3                  // good probes in a row before switching back
)

type unboundCtl struct {
	mu       sync.Mutex
	fallback bool
	since    time.Time
	reason   string
	fails    int
	oks      int
	last     time.Time
	lastErr  string
	step     string // what a change in progress is doing now; empty when none
}

// setUbStep records, for the console, what a Resolve directly change is doing right now.
func (a *App) setUbStep(s string) {
	a.ub.mu.Lock()
	a.ub.step = s
	a.ub.mu.Unlock()
}

// ubExchange asks Unbound a question; tests replace it.
var ubExchange = func(ctx context.Context, m *dns.Msg) (*dns.Msg, error) {
	c := &dns.Client{Timeout: 4 * time.Second}
	r, _, err := c.ExchangeContext(ctx, m, "127.0.0.1:5335")
	return r, err
}

func (a *App) resolveMode() string {
	if a.setting(resolveModeSetting, "forward") == "direct" {
		return "direct"
	}
	return "forward"
}

func (a *App) dnssecOn() bool { return a.setting(dnssecSetting, "1") != "0" }

// directActive is true while lookups really go to Unbound.
func (a *App) directActive() bool {
	if a.resolveMode() != "direct" {
		return false
	}
	a.ub.mu.Lock()
	defer a.ub.mu.Unlock()
	return !a.ub.fallback
}

// unboundUpstream is the one "server" the pool uses in direct mode.
func unboundUpstream() *dnssrv.Upstream {
	return &dnssrv.Upstream{ID: -100, Name: "Resolve directly (Unbound)", Addr: "127.0.0.1:5335", Proto: "udp", Position: 1}
}

func (a *App) unboundApplyPlan() error {
	if a.Net == nil {
		return errNoHelper
	}
	return a.Net.UnboundApply(netd.UnboundPlan{Enabled: a.resolveMode() == "direct", DNSSEC: a.dnssecOn()})
}

// ubProbe asks Unbound a question it can only answer if it works: who runs the root.
func (a *App) ubProbe(ctx context.Context) error {
	m := new(dns.Msg)
	m.SetQuestion(".", dns.TypeNS)
	m.RecursionDesired = true
	r, err := ubExchange(ctx, m)
	if err != nil {
		return err
	}
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) == 0 {
		return errors.New("Unbound answered, but not with the root servers")
	}
	return nil
}

// ubObserve records one probe and moves between "using Unbound" and "using the forwarders".
func (a *App) ubObserve(err error) {
	a.ub.mu.Lock()
	changed, to := false, ""
	a.ub.last = time.Now()
	if err != nil {
		a.ub.lastErr = err.Error()
		a.ub.oks = 0
		a.ub.fails++
		if !a.ub.fallback && a.ub.fails >= ubFailsToFallback {
			a.ub.fallback, a.ub.since, a.ub.reason = true, time.Now(), plainUnboundError(err)
			changed, to = true, "forwarders"
		}
	} else {
		a.ub.lastErr = ""
		a.ub.fails = 0
		a.ub.oks++
		if a.ub.fallback && a.ub.oks >= ubOKsToReturn {
			a.ub.fallback, a.ub.reason = false, ""
			changed, to = true, "direct"
		}
	}
	reason := a.ub.reason
	a.ub.mu.Unlock()
	if !changed {
		return
	}
	a.loadUpstreams()
	if to == "forwarders" {
		a.event("system", "warning", "Name lookups switched to your encrypted forwarders",
			"Resolve directly (Unbound) stopped answering: "+reason+" Names are being looked up through the forwarders in your list until it works again, so nothing is lost. NopeWarden switches back by itself.")
	} else {
		a.event("system", "info", "Resolve directly is working again", "Name lookups are going to Unbound once more.")
	}
}

func plainUnboundError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "refused"):
		return "Unbound is not running."
	case strings.Contains(s, "timeout"), strings.Contains(s, "i/o"):
		return "Unbound did not answer in time. It may be starting, or it cannot reach the internet's root servers."
	}
	return s
}

func (a *App) unboundLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(40 * time.Second):
	}
	if a.resolveMode() == "direct" {
		if err := a.unboundApplyPlan(); err != nil {
			a.ubObserve(err)
		}
		a.loadUpstreams()
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if a.resolveMode() != "direct" {
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		a.ubObserve(a.ubProbe(pctx))
		cancel()
	}
}

// explainFailure says, in plain words, why a lookup failed when the reason is a DNSSEC check. It asks
// again with signature checking switched off: if that works, the signatures were the problem.
func (a *App) explainFailure(r *dns.Msg) string {
	if !a.directActive() || !a.dnssecOn() {
		return ""
	}
	cd := r.Copy()
	cd.CheckingDisabled = true
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	resp, err := ubExchange(ctx, cd)
	if err == nil && resp.Rcode == dns.RcodeSuccess && len(resp.Answer) > 0 {
		return "DNSSEC check failed: this name's security signatures do not match, so the answer was refused. The site's own setup may be broken, or someone may be tampering with it."
	}
	return ""
}

type resolverView struct {
	Mode      string `json:"mode"`   // forward or direct
	DNSSEC    bool   `json:"dnssec"` // checks signatures (direct only)
	Active    string `json:"active"` // direct, forwarders
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version,omitempty"`
	Why       string `json:"why,omitempty"`
	Fallback  bool   `json:"fallback"`
	Since     int64  `json:"since,omitempty"`
	Reason    string `json:"reason,omitempty"`
	LastProbe int64  `json:"last_probe,omitempty"`
	LastError string `json:"last_error,omitempty"`
	Step      string `json:"step,omitempty"` // set while a change is in progress
}

func (a *App) resolverView() resolverView {
	v := resolverView{Mode: a.resolveMode(), DNSSEC: a.dnssecOn(), Active: "forwarders"}
	if a.Net != nil {
		i := a.Net.UnboundStatus()
		v.Installed, v.Running, v.Version, v.Why = i.Installed, i.Running, i.Version, i.Why
	} else {
		v.Why = "The network helper is not running."
	}
	a.ub.mu.Lock()
	v.Fallback, v.Reason, v.LastError = a.ub.fallback, a.ub.reason, a.ub.lastErr
	v.Step = a.ub.step
	if !a.ub.since.IsZero() && a.ub.fallback {
		v.Since = a.ub.since.Unix()
	}
	if !a.ub.last.IsZero() {
		v.LastProbe = a.ub.last.Unix()
	}
	a.ub.mu.Unlock()
	if v.Mode == "direct" && !v.Fallback {
		v.Active = "direct"
	}
	return v
}

type resolverTest struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// runResolverTests tries Unbound with four questions and reports each in plain words.
func (a *App) runResolverTests(ctx context.Context) []resolverTest {
	ask := func(name string, typ uint16, cd bool) (*dns.Msg, error) {
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(name), typ)
		m.RecursionDesired = true
		m.CheckingDisabled = cd
		c, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		return ubExchange(c, m)
	}
	var out []resolverTest
	add := func(n string, ok bool, d string) { out = append(out, resolverTest{n, ok, d}) }
	if err := a.ubProbe(ctx); err != nil {
		add("Unbound answers", false, plainUnboundError(err))
		return out // nothing else can work
	}
	add("Unbound answers", true, "It found the root servers.")
	if r, err := ask("example.com", dns.TypeA, false); err == nil && r.Rcode == dns.RcodeSuccess && len(r.Answer) > 0 {
		add("Looks up an ordinary name", true, "example.com was found.")
	} else {
		add("Looks up an ordinary name", false, "example.com could not be found through Unbound.")
	}
	if !a.dnssecOn() {
		add("Checks DNSSEC signatures", true, "Switched off, so signatures are not checked.")
		return out
	}
	// a name whose signatures are good must be found, and one whose are broken must be refused
	if r, err := ask("sigok.verteiltesysteme.net", dns.TypeA, false); err == nil && r.Rcode == dns.RcodeSuccess && len(r.Answer) > 0 {
		add("Accepts a name with good signatures", true, "It was found and its signatures checked.")
	} else {
		add("Accepts a name with good signatures", false, "A name with correct signatures was refused. DNSSEC checking may be misconfigured, or the clock is wrong.")
	}
	if r, err := ask("sigfail.verteiltesysteme.net", dns.TypeA, false); err == nil && r.Rcode == dns.RcodeServerFailure {
		add("Refuses a name with broken signatures", true, "It was refused, as it should be.")
	} else {
		add("Refuses a name with broken signatures", false, "A name with broken signatures was accepted, so DNSSEC is not protecting you.")
	}
	return out
}

func (a *App) registerResolverRoutes() {
	s := a.API
	s.Handle("GET /api/dns/resolver", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.resolverView())
	})
	s.HandleAdmin("PUT /api/dns/resolver", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Mode   string `json:"mode"`
			DNSSEC *bool  `json:"dnssec"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.Mode != "forward" && in.Mode != "direct" {
			fail(w, errors.New("choose your encrypted forwarders, or resolve directly"))
			return
		}
		defer a.setUbStep("")
		if in.Mode == "direct" {
			a.setUbStep("Setting up Unbound: checking its settings and DNSSEC key, then enabling and starting it")
		} else {
			a.setUbStep("Switching back to your encrypted forwarders")
		}
		prevMode, prevSec := a.resolveMode(), a.setting(dnssecSetting, "1")
		a.Config.SetSetting(resolveModeSetting, in.Mode)
		if in.DNSSEC != nil {
			v := "0"
			if *in.DNSSEC {
				v = "1"
			}
			a.Config.SetSetting(dnssecSetting, v)
		}
		if err := a.unboundApplyPlan(); err != nil {
			// nothing is changed if Unbound cannot be set up
			a.Config.SetSetting(resolveModeSetting, prevMode)
			a.Config.SetSetting(dnssecSetting, prevSec)
			a.unboundApplyPlan()
			fail(w, err)
			return
		}
		a.ub.mu.Lock()
		a.ub.fallback, a.ub.fails, a.ub.oks, a.ub.reason = false, 0, 0, ""
		a.ub.mu.Unlock()
		if in.Mode == "direct" {
			// switch only when Unbound really answers; give it a few seconds to start
			var perr error
			for i := 0; i < 6; i++ {
				a.setUbStep("Waiting for Unbound to answer its first question (try " + strconv.Itoa(i+1) + " of 6)")
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				perr = a.ubProbe(ctx)
				cancel()
				if perr == nil {
					break
				}
				time.Sleep(time.Second)
			}
			if perr != nil {
				a.Config.SetSetting(resolveModeSetting, "forward")
				a.unboundApplyPlan()
				a.loadUpstreams()
				fail(w, errors.New("Unbound started but did not answer ("+plainUnboundError(perr)+"). Your encrypted forwarders are still in use"))
				return
			}
		}
		a.setUbStep("Switching your devices over")
		a.DNS.Cache().Flush()
		a.loadUpstreams()
		a.auditEvent(c.User.Username, "info", "Name lookups changed", map[string]string{"direct": "Now resolving directly with Unbound.", "forward": "Now using the encrypted forwarders."}[in.Mode])
		api.JSON(w, 200, a.resolverView())
	})
	s.HandleAdmin("POST /api/dns/resolver/test", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		if a.resolveMode() != "direct" {
			fail(w, errors.New("turn on Resolve directly first"))
			return
		}
		api.JSON(w, 200, map[string]any{"tests": a.runResolverTests(r.Context())})
	})
}

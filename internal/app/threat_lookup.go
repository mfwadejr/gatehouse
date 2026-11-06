package app

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"nopewarden/internal/api"
)

// Names for the addresses in threat alerts. The first lookup happens when someone opens Details; a
// lookup that fails or finds nothing is tried again after 10 seconds, 30 seconds, 1 minute and 5
// minutes, then every 30 minutes for as long as the address is still being looked at. Look up again
// starts over at once.

type ptrEntry struct {
	Name    string
	State   string // looking, found, none, failed
	Tries   int
	Next    time.Time
	Seen    time.Time // when someone last asked about it
	Reason  string
	Looking bool
}

var (
	ptrMu      sync.Mutex
	ptrs       = map[string]*ptrEntry{}
	ptrWatch   sync.Once
	ptrNow     = time.Now
	ptrResolve func(a *App, addr string) (name string, nodata bool, err error)
)

func ptrDelay(tries int) time.Duration {
	steps := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}
	if tries < len(steps) {
		return steps[tries]
	}
	return 30 * time.Minute
}

func ptrDoLookup(a *App, addr string) (string, bool, error) {
	if ptrResolve != nil {
		return ptrResolve(a, addr)
	}
	rev, err := dns.ReverseAddr(addr)
	if err != nil {
		return "", true, nil
	}
	resp, _, err := a.lookupName(rev, dns.TypePTR)
	if err != nil {
		return "", false, err
	}
	if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
		return "", false, errServerAnswer(resp.Rcode)
	}
	for _, rr := range resp.Answer {
		if p, ok := rr.(*dns.PTR); ok {
			return strings.TrimSuffix(p.Ptr, "."), false, nil
		}
	}
	return "", true, nil
}

type lookupErr string

func (e lookupErr) Error() string { return string(e) }
func errServerAnswer(rc int) error {
	return lookupErr("the DNS server answered " + dns.RcodeToString[rc])
}

// ptrName is the name known so far for an address ("" when none). It never waits.
func (a *App) ptrName(addr string) string {
	ptrMu.Lock()
	defer ptrMu.Unlock()
	if e := ptrs[addr]; e != nil {
		return e.Name
	}
	return ""
}

// ptrAsk makes sure the address is being looked up. force starts over (the Look up again button).
func (a *App) ptrAsk(addr string, force bool) {
	now := ptrNow()
	ptrMu.Lock()
	e := ptrs[addr]
	if e == nil {
		e = &ptrEntry{State: "looking", Next: now}
		ptrs[addr] = e
	}
	e.Seen = now
	if force && e.State != "found" {
		e.Tries, e.Next, e.State = 0, now, "looking"
	}
	ptrMu.Unlock()
	ptrWatch.Do(func() { go a.ptrLoop() })
	a.ptrRunDue()
}

// ptrRunDue starts every lookup whose time has come.
func (a *App) ptrRunDue() {
	now := ptrNow()
	ptrMu.Lock()
	var run []string
	for addr, e := range ptrs {
		if now.Sub(e.Seen) > 24*time.Hour {
			delete(ptrs, addr)
			continue
		}
		if e.State != "found" && !e.Looking && !e.Next.After(now) {
			e.Looking = true
			run = append(run, addr)
		}
	}
	ptrMu.Unlock()
	for _, addr := range run {
		go func(addr string) {
			name, nodata, err := ptrDoLookup(a, addr)
			ptrMu.Lock()
			defer ptrMu.Unlock()
			e := ptrs[addr]
			if e == nil {
				return
			}
			e.Looking = false
			switch {
			case err != nil:
				e.State, e.Reason = "failed", "NopeWarden could not ask the DNS server ("+err.Error()+")."
			case name != "":
				e.State, e.Name, e.Reason = "found", name, ""
				return
			case nodata:
				e.State, e.Reason = "none", "The owner of this address has not published a name for it."
			}
			e.Next = ptrNow().Add(ptrDelay(e.Tries))
			e.Tries++
		}(addr)
	}
}

func (a *App) ptrLoop() {
	for {
		time.Sleep(5 * time.Second)
		a.ptrRunDue()
	}
}

type ptrView struct {
	Addr   string `json:"addr"`
	Name   string `json:"name,omitempty"`
	State  string `json:"state"`           // looking, found, none, failed
	Status string `json:"status"`          // plain words
	Retry  int    `json:"retry,omitempty"` // seconds until the next try
}

func (a *App) ptrViews(addrs []string) []ptrView {
	now := ptrNow()
	ptrMu.Lock()
	defer ptrMu.Unlock()
	out := []ptrView{}
	for _, addr := range addrs {
		e := ptrs[addr]
		if e == nil {
			continue
		}
		v := ptrView{Addr: addr, Name: e.Name, State: e.State}
		if e.Looking || (e.State == "looking") {
			v.State, v.Status = "looking", "Looking up the name…"
		} else if e.State == "found" {
			v.Status = "Name found."
		} else {
			v.Status = e.Reason
			if d := int(e.Next.Sub(now).Seconds()); d > 0 {
				v.Retry = d
				v.Status += " Trying again in " + plainWait(d) + "."
			}
		}
		out = append(out, v)
	}
	return out
}

func plainWait(s int) string {
	switch {
	case s < 90:
		return strconv.Itoa(s) + " seconds"
	case s < 90*60:
		return strconv.Itoa((s+30)/60) + " minutes"
	}
	return strconv.Itoa((s+1800)/3600) + " hours"
}

func (a *App) registerThreatLookupRoutes() {
	s := a.API
	// Asking starts the lookups (first time: when Details opens). Reading it again shows how they went.
	s.Handle("POST /api/threat/lookup", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Addrs []string `json:"addrs"`
			Force bool     `json:"force"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if len(in.Addrs) > 50 {
			in.Addrs = in.Addrs[:50]
		}
		for _, addr := range in.Addrs {
			if addr != "" && !privateAddr(addr) {
				a.ptrAsk(addr, in.Force)
			}
		}
		api.JSON(w, 200, a.ptrViews(in.Addrs))
	})
}

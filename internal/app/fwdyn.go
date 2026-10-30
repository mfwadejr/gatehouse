package app

import (
	"context"
	"log"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"gatehouse/internal/fw"
)

// The live part of the firewall: the addresses behind devices, groups, domains and countries.
// Rules name these things; this keeps the matching address sets up to date, so a device that gets
// a new address from DHCP is covered within a couple of seconds and no rule has to be applied.

type fwDyn struct {
	mu         sync.Mutex
	dom        map[string]map[string]time.Time // domain -> address -> when it is forgotten
	pushQ      map[string]map[string]time.Time // set name -> address -> expiry, waiting to be sent
	watch      []string                        // domains named by enabled rules
	counts     map[string]fw.Counter
	kick       chan struct{}
	geoBusy    atomic.Bool
	geoTried   atomic.Int64
	geoErr     atomic.Value // string
	lastPaused atomic.Value // string: the paused devices as last pushed
}

func newFWDyn() *fwDyn {
	d := &fwDyn{dom: map[string]map[string]time.Time{}, pushQ: map[string]map[string]time.Time{}, counts: map[string]fw.Counter{}, kick: make(chan struct{}, 1)}
	d.geoErr.Store("")
	return d
}

// fwSyncAll asks for every dynamic set to be filled again, including the country lists.
func (a *App) fwSyncAll() { a.dyn.kickSync(true) }

// fwSync asks for the device, group and domain sets to be refreshed soon.
func (a *App) fwSync() { a.dyn.kickSync(false) }

var fullSyncWanted atomic.Bool

func (d *fwDyn) kickSync(full bool) {
	if full {
		fullSyncWanted.Store(true)
	}
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// addrIndex reads every device's current addresses: active leases and reservations.
func (a *App) addrIndex() (leases, reserved map[string][]string) {
	leases, reserved = map[string][]string{}, map[string][]string{}
	if rows, err := a.Config.Query(`SELECT lower(mac),ip FROM dhcp_leases WHERE state='active' AND expires>?`, time.Now().Unix()); err == nil {
		for rows.Next() {
			var mac, ip string
			rows.Scan(&mac, &ip)
			leases[mac] = append(leases[mac], ip)
		}
		rows.Close()
	}
	if rows, err := a.Config.Query(`SELECT lower(mac),ip FROM dhcp_reservations`); err == nil {
		for rows.Next() {
			var mac, ip string
			rows.Scan(&mac, &ip)
			reserved[mac] = append(reserved[mac], ip)
		}
		rows.Close()
	}
	return
}

func uniqStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// deviceAddrs are the addresses a set of devices has right now. With reservedOnly, only the
// address reserved for each device counts.
func deviceAddrs(macs []string, reservedOnly bool, leases, reserved map[string][]string) []string {
	var out []string
	for _, m := range macs {
		out = append(out, reserved[m]...)
		if !reservedOnly {
			out = append(out, leases[m]...)
		}
	}
	return uniqStrings(out)
}

// domainMatches reports whether a looked-up name falls under a rule's domain.
func domainMatches(pattern, name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	return name == pattern || strings.HasSuffix(name, "."+pattern)
}

// setContents works out what every dynamic set of the model should hold. Country lists are left
// out unless withGeo is set, because they are large and change only weekly.
func (a *App) setContents(m *fw.Model, withGeo bool) map[string]fw.SetFill {
	out := map[string]fw.SetFill{}
	leases, reserved := a.addrIndex()
	wantGeo := map[string]bool{}
	now := time.Now()
	a.dyn.mu.Lock()
	defer a.dyn.mu.Unlock()
	for _, ds := range m.DynamicSets() {
		fill := fw.SetFill{Replace: true}
		switch ds.Ref.Kind {
		case fw.RefDevice:
			if ds.Family == 0 {
				fill.Elems = []string{ds.Ref.Val}
			} else {
				fill.Elems = deviceAddrs([]string{ds.Ref.Val}, false, leases, reserved)
			}
		case fw.RefGroup:
			g := m.DevGroups[ds.Ref.Val]
			if ds.Family == 0 {
				fill.Elems = append([]string(nil), g.MACs...)
			} else {
				fill.Elems = deviceAddrs(g.MACs, g.ReservedOnly, leases, reserved)
			}
		case fw.RefDomain:
			for ip, exp := range a.dyn.dom[ds.Ref.Val] {
				left := int(exp.Sub(now).Seconds())
				if left < 1 {
					continue
				}
				if addr, err := netip.ParseAddr(ip); err == nil && (ds.Family == 4) == addr.Is4() {
					fill.Elems = append(fill.Elems, ip+" timeout "+istr(left)+"s")
				}
			}
		case fw.RefGeo:
			if !withGeo {
				continue
			}
			wantGeo[strings.ToLower(ds.Ref.Val)+"_"+istr(ds.Family)] = true
		}
		// Only IPv4 addresses belong in an IPv4 set.
		if ds.Family == 4 && (ds.Ref.Kind == fw.RefDevice || ds.Ref.Kind == fw.RefGroup) {
			fill.Elems = onlyFamily(fill.Elems, 4)
		}
		if ds.Ref.Kind != fw.RefGeo {
			out[ds.Name] = fill
		}
	}
	if len(m.PortalNets()) > 0 {
		out[fw.PortalSet] = a.portalFill(leases)
	}
	if withGeo && len(wantGeo) > 0 {
		pref := a.geoPrefixes(wantGeo)
		for _, ds := range m.DynamicSets() {
			if ds.Ref.Kind == fw.RefGeo {
				out[ds.Name] = fw.SetFill{Replace: true, Elems: pref[strings.ToLower(ds.Ref.Val)+"_"+istr(ds.Family)]}
			}
		}
	}
	return out
}

func istr(n int) string { return strconv.Itoa(n) }

func onlyFamily(in []string, fam int) []string {
	var out []string
	for _, s := range in {
		if a, err := netip.ParseAddr(s); err == nil && (fam == 4) == a.Is4() {
			out = append(out, s)
		}
	}
	return out
}

// fullScript is the ruleset with the contents of its sets, which loads in one step so no rule
// ever runs against an empty set.
func (a *App) fullScript(m *fw.Model, script string) string {
	body, err := fw.BuildSetScript(a.setContents(m, true), nil)
	if err != nil {
		log.Printf("firewall sets: %v", err)
		return script
	}
	return script + "\n" + body
}

// geoMissing says whether the rules use countries whose address lists have not been downloaded.
func (a *App) geoMissing(m *fw.Model) bool {
	for _, ds := range m.DynamicSets() {
		if ds.Ref.Kind == fw.RefGeo {
			_, ok := a.geoAge()
			return !ok
		}
	}
	return false
}

func (a *App) usesGeo(m *fw.Model) bool {
	for _, ds := range m.DynamicSets() {
		if ds.Ref.Kind == fw.RefGeo {
			return true
		}
	}
	return false
}

// syncSets pushes current contents into the loaded ruleset.
func (a *App) syncSets(full bool) {
	if a.FW == nil {
		return
	}
	m, err := fw.Load(a.Config)
	if err != nil {
		return
	}
	a.dyn.mu.Lock()
	a.dyn.watch = a.dyn.watch[:0]
	for _, ds := range m.DynamicSets() {
		if ds.Ref.Kind == fw.RefDomain && ds.Family == 4 {
			a.dyn.watch = append(a.dyn.watch, ds.Ref.Val)
		}
	}
	a.dyn.mu.Unlock()
	for name, fill := range a.setContents(m, full) {
		if err := a.FW.LoadSets(map[string]fw.SetFill{name: fill}); err != nil {
			log.Printf("firewall sets: %s: %v", name, err)
		}
	}
}

// onDNSAnswer remembers the addresses behind the domains that rules name.
func (a *App) onDNSAnswer(qname string, answers []dns.RR) {
	a.names.remember(qname, answers)
	a.dyn.mu.Lock()
	defer a.dyn.mu.Unlock()
	if len(a.dyn.watch) == 0 {
		return
	}
	var hit []string
	for _, d := range a.dyn.watch {
		if domainMatches(d, qname) {
			hit = append(hit, d)
		}
	}
	if len(hit) == 0 {
		return
	}
	now := time.Now()
	for _, rr := range answers {
		var ip netip.Addr
		switch v := rr.(type) {
		case *dns.A:
			ip, _ = netip.AddrFromSlice(v.A.To4())
		case *dns.AAAA:
			ip, _ = netip.AddrFromSlice(v.AAAA)
		default:
			continue
		}
		if !ip.IsValid() {
			continue
		}
		keep := time.Duration(rr.Header().Ttl)*2*time.Second + 5*time.Minute
		if keep > 24*time.Hour {
			keep = 24 * time.Hour
		}
		fam := 4
		if ip.Is6() {
			fam = 6
		}
		for _, d := range hit {
			if a.dyn.dom[d] == nil {
				a.dyn.dom[d] = map[string]time.Time{}
			}
			a.dyn.dom[d][ip.String()] = now.Add(keep)
			name := fw.SetNameFor(fw.Ref{Kind: fw.RefDomain, Val: d}, fam)
			if a.dyn.pushQ[name] == nil {
				a.dyn.pushQ[name] = map[string]time.Time{}
			}
			a.dyn.pushQ[name][ip.String()] = now.Add(keep)
		}
	}
}

// flushDomains sends newly learned addresses into the ruleset.
func (a *App) flushDomains() {
	a.dyn.mu.Lock()
	q := a.dyn.pushQ
	a.dyn.pushQ = map[string]map[string]time.Time{}
	a.dyn.mu.Unlock()
	if a.FW == nil || len(q) == 0 {
		return
	}
	now := time.Now()
	fills := map[string]fw.SetFill{}
	for name, ips := range q {
		var f fw.SetFill
		for ip, exp := range ips {
			if left := int(exp.Sub(now).Seconds()); left > 0 {
				f.Elems = append(f.Elems, ip+" timeout "+istr(left)+"s")
			}
		}
		fills[name] = f
	}
	if err := a.FW.LoadSets(fills); err != nil {
		log.Printf("firewall sets: %v", err)
	}
	// Forget addresses that have expired.
	a.dyn.mu.Lock()
	for d, ips := range a.dyn.dom {
		for ip, exp := range ips {
			if exp.Before(now) {
				delete(ips, ip)
			}
		}
		if len(ips) == 0 {
			delete(a.dyn.dom, d)
		}
	}
	a.dyn.mu.Unlock()
}

// fwTag is the label a rule's counters carry in the ruleset.
func fwTag(r fw.RuleRow) string { return "rule:" + r.From + ":" + r.To + ":" + r.Name }

// sampleHits adds what the counters have counted since the last look to the totals, which
// survive a firewall reload.
func (a *App) sampleHits() {
	if a.FW == nil {
		return
	}
	cur, err := a.FW.Counters()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	a.sampleDevices(cur, time.Now())
	a.sampleRemotes(cur, time.Now())
	for tag, c := range cur {
		if strings.HasPrefix(tag, fw.AcctPrefix) || strings.HasPrefix(tag, fw.RemotePrefix) {
			continue
		}
		prev := a.dyn.counts[tag]
		dp, db := c.Packets-prev.Packets, c.Bytes-prev.Bytes
		if c.Packets < prev.Packets { // the ruleset was loaded again, so counting restarted
			dp, db = c.Packets, c.Bytes
		}
		if dp == 0 {
			continue
		}
		a.Config.Exec(`INSERT INTO fw_hits(key,hits,bytes,last_hit) VALUES(?,?,?,?)
			ON CONFLICT(key) DO UPDATE SET hits=hits+excluded.hits,bytes=bytes+excluded.bytes,last_hit=excluded.last_hit`, tag, dp, db, now)
	}
	a.dyn.counts = cur
}

type hitInfo struct {
	Hits    int64 `json:"hits"`
	Bytes   int64 `json:"bytes"`
	LastHit int64 `json:"last_hit"`
}

func (a *App) hitTable() map[string]hitInfo {
	out := map[string]hitInfo{}
	if rows, err := a.Config.Query(`SELECT key,hits,bytes,last_hit FROM fw_hits`); err == nil {
		for rows.Next() {
			var k string
			var h hitInfo
			rows.Scan(&k, &h.Hits, &h.Bytes, &h.LastHit)
			out[k] = h
		}
		rows.Close()
	}
	return out
}

// utcOffset is the machine's offset from UTC at a moment, in seconds.
func (a *App) utcOffset(at time.Time) int {
	loc := time.UTC
	if name := strings.TrimSpace(a.Config.Setting("system.timezone", "")); name != "" {
		if l, err := time.LoadLocation(name); err == nil {
			loc = l
		}
	}
	_, off := at.In(loc).Zone()
	return off
}

// retimeIfNeeded re-applies the firewall when summer time starts or ends, so that scheduled rules
// stay at the hours people chose. It only does so when nothing else about the settings has
// changed, so it never puts someone's unfinished edits live.
func (a *App) retimeIfNeeded() {
	if a.FW == nil {
		return
	}
	if _, _, pending := a.FW.Pending(); pending {
		return
	}
	var at int64
	var cur string
	if a.Config.QueryRow(`SELECT created_at,ruleset FROM fw_revisions WHERE status='confirmed' ORDER BY version DESC LIMIT 1`).Scan(&at, &cur) != nil {
		return
	}
	if a.utcOffset(time.Unix(at, 0)) == a.utcOffset(time.Now()) {
		return
	}
	m, err := fw.Load(a.Config)
	if err != nil {
		return
	}
	scheduled := false
	for _, r := range m.Rules {
		if r.Enabled && strings.TrimSpace(r.Sched) != "" {
			scheduled = true
		}
	}
	if !scheduled {
		return
	}
	// The settings as they were when the live ruleset was made must still give that ruleset.
	m.Now = time.Unix(at, 0)
	was, err := fw.Compile(m)
	if err != nil || was != cur {
		return
	}
	m.Now = time.Now()
	script, err := fw.Compile(m)
	if err != nil {
		return
	}
	tok, _, err := a.FW.Apply(a.fullScript(m, script), confirmWindow, "schedule")
	if err != nil {
		return
	}
	if err := a.FW.Confirm(tok); err != nil {
		return
	}
	a.Config.Exec(`UPDATE fw_revisions SET status='superseded' WHERE status='confirmed'`)
	a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status) VALUES(?,?,?,?,'confirmed')`,
		time.Now().Unix(), "schedule", "Scheduled rules moved with the change between summer and winter time.", script)
	a.event("system", "info", "Scheduled firewall rules were adjusted for the time change", "")
	a.publishFW()
}

// dynLoop runs the live firewall work.
func (a *App) dynLoop(ctx context.Context) {
	a.DNS.OnAnswer = a.onDNSAnswer
	fast := time.NewTicker(2 * time.Second)
	hits := time.NewTicker(20 * time.Second)
	slow := time.NewTicker(time.Hour)
	defer fast.Stop()
	defer hits.Stop()
	defer slow.Stop()
	a.syncSets(true)
	a.geoMaybeRefreshIfNeeded(ctx)
	var due time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.dyn.kick:
			due = time.Now().Add(1500 * time.Millisecond)
		case <-fast.C:
			if !due.IsZero() && time.Now().After(due) {
				due = time.Time{}
				a.syncSets(fullSyncWanted.Swap(false))
				a.geoMaybeRefreshIfNeeded(ctx)
			}
			a.flushDomains()
		case <-hits.C:
			a.sampleHits()
			if sig := a.pausedSig(); sig != a.dyn.lastPaused.Load() {
				a.dyn.lastPaused.Store(sig)
				a.fwSync()
			}
		case <-slow.C:
			a.retimeIfNeeded()
			a.geoMaybeRefreshIfNeeded(ctx)
		}
	}
}

func (a *App) geoMaybeRefreshIfNeeded(ctx context.Context) {
	m, err := fw.Load(a.Config)
	if err != nil {
		return
	}
	if a.usesGeo(m) {
		go a.geoMaybeRefresh(ctx, true)
	}
}

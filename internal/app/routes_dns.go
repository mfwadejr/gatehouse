package app

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
	dnssrv "gatehouse/internal/dns"
)

var (
	errBadID        = errors.New("invalid request")
	errLastUpstream = errors.New("at least one upstream resolver must remain")
)

func (a *App) dnsSummary(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	snap := a.DNS.Stats().Snapshot()
	snap.CacheSize = a.DNS.Cache().Len()
	lists, _ := a.Lists.Lists()
	entries, enabled, loaded := 0, 0, 0
	for _, l := range lists {
		if l.Enabled {
			enabled++
			entries += l.Entries
			if l.Entries+l.Allowed > 0 {
				loaded++
			}
		}
	}
	api.JSON(w, 200, map[string]any{
		"stats": snap, "upstreams": a.DNS.Pool().Status(), "list_count": len(lists), "blocked_entries": entries,
		"unique_blocked": a.Lists.UniqueBlocked(),
		// Blocking only works once at least one switched-on list has been downloaded.
		"lists_enabled": enabled, "lists_loaded": loaded, "blocking_active": enabled == 0 || loaded > 0,
		"log_dropped": a.QLog.Dropped(),
	})
}

func (a *App) dnsLog(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	q := r.URL.Query()
	f := dnssrv.Filter{Text: q.Get("q"), Result: q.Get("result")}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	if h, err := strconv.Atoi(q.Get("hours")); err == nil && h > 0 {
		f.Since = time.Now().Add(-time.Duration(h) * time.Hour).Unix()
	}
	rows, err := a.QLog.Search(f)
	if err != nil {
		fail(w, err)
		return
	}
	if rows == nil {
		rows = []dnssrv.Row{}
	}
	api.JSON(w, 200, rows)
}

func (a *App) dnsTop(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	hours := 24
	if h, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && h > 0 && h <= 24*30 {
		hours = h
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	raw, _ := a.QLog.TopClients(since, 200, r.URL.Query().Get("self") == "1")
	clients := a.namedClients(raw, 10)
	blocked, _ := a.QLog.TopBlocked(since, 10)
	domains, _ := a.QLog.TopDomains(since, 10)
	api.JSON(w, 200, map[string]any{"clients": clients, "blocked": nz(blocked), "domains": nz(domains), "total": a.QLog.Count(since), "hours": hours})
}

func nz(t []dnssrv.Top) []dnssrv.Top {
	if t == nil {
		return []dnssrv.Top{}
	}
	return t
}

func (a *App) dnsAddList(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Text     string `json:"text"`
		Format   string `json:"format"`
		Interval int    `json:"interval_hours"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) != "" {
		id, err := a.Lists.AddLocal(in.Name, in.Text, in.Format)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]int64{"id": id})
		return
	}
	id, err := a.Lists.Add(in.Name, in.URL, in.Format, in.Interval)
	if err != nil {
		fail(w, err)
		return
	}
	// Fetch in the background so the console responds at once; progress shows in the list status.
	go func() {
		ctx, cancel := contextTimeout(2 * time.Minute)
		defer cancel()
		if a.Lists.Refresh(ctx, id) == nil {
			a.Lists.Reload()
		}
	}()
	api.JSON(w, 200, map[string]int64{"id": id})
}

func (a *App) dnsAddRecord(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Value string `json:"value"`
		TTL   int    `json:"ttl"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Value) == "" {
		fail(w, errors.New("name and value are required"))
		return
	}
	// Check the record the way the resolver will read it, so a bad value is refused here
	// instead of being saved and then silently ignored.
	name, value, err := dnssrv.CleanRecord(in.Name, in.Type, in.Value)
	if err != nil {
		fail(w, err)
		return
	}
	in.Name, in.Value = name, value
	if in.TTL <= 0 {
		in.TTL = 300
	}
	if _, err := a.Config.Exec(`INSERT OR IGNORE INTO dns_records(name,type,value,ttl) VALUES(?,?,?,?)`, in.Name, in.Type, in.Value, in.TTL); err != nil {
		fail(w, err)
		return
	}
	a.DNS.SetLocalRecords(a.localRecords())
	ok(w)
}

func (a *App) dnsAddUpstream(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct{ Name, Address, Proto, TLSName string }
	if !api.Decode(w, r, &in) {
		return
	}
	in.Proto = strings.ToLower(in.Proto)
	if err := dnssrv.ValidateUpstream(in.Proto, in.Address, in.TLSName); err != nil {
		fail(w, err)
		return
	}
	if in.Name == "" {
		in.Name = in.Address
	}
	if _, err := a.Config.Exec(`INSERT INTO dns_upstreams(name,address,proto,tls_name,position) VALUES(?,?,?,?,(SELECT COALESCE(MAX(position),0)+1 FROM dns_upstreams))`, in.Name, in.Address, in.Proto, in.TLSName); err != nil {
		fail(w, errors.New("that resolver is already in the list"))
		return
	}
	a.loadUpstreams()
	ok(w)
}

func (a *App) dnsSaveSettings(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		BlockMode   string  `json:"block_mode"`
		LocalDomain string  `json:"local_domain"`
		RateLimit   float64 `json:"rate_limit"`
		RateBurst   float64 `json:"rate_burst"`
		CacheSize   int     `json:"cache_size"`
		Canary      bool    `json:"canary"`
		Retention   int     `json:"log_retention_days"`
		Anonymize   bool    `json:"anonymize"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	switch in.BlockMode {
	case "null", "nxdomain", "refused":
	default:
		fail(w, errors.New("block mode must be null, nxdomain or refused"))
		return
	}
	dom := strings.Trim(strings.ToLower(strings.TrimSpace(in.LocalDomain)), ".")
	if dom == "" || strings.ContainsAny(dom, " /") {
		fail(w, errors.New("local domain is not valid"))
		return
	}
	if in.RateLimit < 0 || in.RateBurst < 0 || in.CacheSize < 100 || in.CacheSize > 1000000 || in.Retention < 0 || in.Retention > 3650 {
		fail(w, errors.New("a value is out of range"))
		return
	}
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	for k, v := range map[string]string{
		"dns.block_mode": in.BlockMode, "dns.local_domain": dom,
		"dns.rate_limit": strconv.FormatFloat(in.RateLimit, 'f', -1, 64), "dns.rate_burst": strconv.FormatFloat(in.RateBurst, 'f', -1, 64),
		"dns.cache_size": strconv.Itoa(in.CacheSize), "dns.canary": b(in.Canary),
		"dns.log_retention_days": strconv.Itoa(in.Retention), "dns.anonymize": b(in.Anonymize),
	} {
		if err := a.Config.SetSetting(k, v); err != nil {
			fail(w, err)
			return
		}
	}
	if err := a.applyDNS(); err != nil {
		fail(w, err)
		return
	}
	ok(w)
}

// dnsOrderUpstreams saves the order the person arranged the DNS servers in. The list must name
// every server exactly once, so a stale page cannot drop or duplicate one.
func (a *App) dnsOrderUpstreams(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	have := map[int64]bool{}
	rows, err := a.Config.Query(`SELECT id FROM dns_upstreams`)
	if err != nil {
		fail(w, err)
		return
	}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		have[id] = true
	}
	rows.Close()
	seen := map[int64]bool{}
	for _, id := range in.IDs {
		if !have[id] || seen[id] {
			fail(w, errors.New("the list changed; reload the page and try again"))
			return
		}
		seen[id] = true
	}
	if len(seen) != len(have) {
		fail(w, errors.New("the list changed; reload the page and try again"))
		return
	}
	tx, err := a.Config.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	for i, id := range in.IDs {
		if _, err := tx.Exec(`UPDATE dns_upstreams SET position=? WHERE id=?`, i+1, id); err != nil {
			tx.Rollback()
			fail(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	a.loadUpstreams()
	ok(w)
}

// topClient is one row of the busiest-devices list: a device, which can have more than one address.
type topClient struct {
	Key   string   `json:"key"`
	Label string   `json:"label,omitempty"`
	N     int64    `json:"n"`
	Addrs []string `json:"addrs"`
}

// namedClients turns the per-address ranking into a per-device one. A device is named the way
// the Devices page names it (the alias you chose, else its own name), and addresses that belong
// to one device (same hardware address) are added together. The label recorded at query time is
// only the fallback, so renaming a device shows at once.
func (a *App) namedClients(raw []dnssrv.Top, limit int) []topClient {
	out := []topClient{}
	byMAC := map[string]int{}
	for _, t := range raw {
		ci := a.Devices.Client(t.Key)
		label := ci.Name
		if label == "" {
			label = t.Label
		}
		if ci.MAC != "" {
			if i, ok := byMAC[ci.MAC]; ok {
				out[i].N += t.N
				out[i].Addrs = append(out[i].Addrs, t.Key)
				if out[i].Label == "" {
					out[i].Label = label
				}
				continue
			}
			byMAC[ci.MAC] = len(out)
		}
		key := t.Key
		if ci.MAC != "" {
			key = ci.MAC
		}
		out = append(out, topClient{Key: key, Label: label, N: t.N, Addrs: []string{t.Key}})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].N > out[j].N })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

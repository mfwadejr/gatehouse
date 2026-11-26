package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"nopewarden/internal/api"
)

// The DNS check: does a device use NopeWarden's DNS, or does it also talk to a DNS server on its own?
// It reads the firewall log and the DNS log for one device. A destination is named in layers, and the
// answer says which layer named it, so "confirmed" and "probably" are never mixed up:
//  1. the known list of public DNS services, 2. the name the device itself looked up for that address,
//  3. the address's reverse name, 4. and only when someone presses "Check this address", the name on
//     the server's certificate and an actual DNS question over HTTPS (443) and TLS (853).
//
// Steps 3 and 4 connect to a third party, so step 4 never runs by itself.

type dnsDest struct {
	Addr     string    `json:"addr"`
	Name     string    `json:"name,omitempty"`
	Service  string    `json:"service,omitempty"` // the DNS service it is, when known
	Level    string    `json:"level"`             // known, probably, port, confirmed, unknown
	Why      string    `json:"why"`
	Port     int       `json:"port"`
	Proto    string    `json:"proto"`
	Allowed  int       `json:"allowed"`
	Stopped  int       `json:"stopped"`
	First    int64     `json:"first"`
	Last     int64     `json:"last"`
	Every    string    `json:"every,omitempty"` // "about every 5 minutes"
	ProbeOut *dnsProbe `json:"probe,omitempty"`
}

type dnsProbe struct {
	At       int64    `json:"at"`
	DoH      bool     `json:"doh"`
	DoT      bool     `json:"dot"`
	CertName []string `json:"cert_names,omitempty"`
	Detail   string   `json:"detail"`
}

var (
	probeMu    sync.Mutex
	probeCache = map[string]*dnsProbe{}
)

// looksLikeDNSName is true for names that DNS services use for themselves: dns.google, dns.quad9.net,
// cloudflare-dns.com, doh.opendns.com, mozilla.cloudflare-dns.com.
func looksLikeDNSName(n string) bool {
	n = strings.ToLower(n)
	for _, l := range strings.Split(n, ".") {
		if l == "dns" || l == "doh" || l == "dot" || strings.HasSuffix(l, "-dns") || strings.HasPrefix(l, "dns-") || strings.HasPrefix(l, "doh-") {
			return true
		}
	}
	return strings.HasSuffix(n, "dns.google") || strings.Contains(n, "dns.nextdns") || strings.Contains(n, "dnsforge")
}

func everyWords(n int, first, last int64) string {
	if n < 3 || last <= first {
		return ""
	}
	gap := (last - first) / int64(n-1)
	switch {
	case gap < 90:
		return "about every minute"
	case gap < 3600:
		return fmt.Sprintf("about every %d minutes", (gap+30)/60)
	case gap < 86400:
		h := (gap + 1800) / 3600
		if h == 1 {
			return "about every hour"
		}
		return fmt.Sprintf("about every %d hours", h)
	}
	return "about once a day"
}

func dcPrivate(s string) bool {
	ad, err := netip.ParseAddr(s)
	if err != nil {
		return true
	}
	ad = ad.Unmap()
	return ad.IsPrivate() || ad.IsLoopback() || ad.IsLinkLocalUnicast() || ad.IsMulticast() || ad.IsUnspecified() || ad.IsInterfaceLocalMulticast()
}

func (a *App) dnsCheck(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if _, err := netip.ParseAddr(ip); err != nil {
		api.Error(w, 400, "Pick a device to check.")
		return
	}
	hours := 24
	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	res := map[string]any{"ip": ip, "name": a.deviceName(ip), "hours": hours}

	var total, blocked int
	a.Logs.QueryRow(`SELECT COUNT(*), COALESCE(SUM(result='blocked'),0) FROM dns_queries WHERE client=? AND ts>=? AND `+notHealthSQL, ip, since).Scan(&total, &blocked)
	res["lookups"], res["blocked"] = total, blocked

	// Everything this device sent to port 53, 853 or 443 that left the building.
	rows, err := a.Logs.Query(`SELECT to_addr,proto,dport,action,to_zone,dst_name,COUNT(*),MIN(ts),MAX(ts) FROM fw_events
		WHERE from_addr=? AND ts>=? AND dport IN (53,443,853) AND to_zone<>'Router' GROUP BY to_addr,proto,dport,action,to_zone,dst_name ORDER BY COUNT(*) DESC LIMIT 400`, ip, since)
	byKey := map[string]*dnsDest{}
	var order []string
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var to, proto, action, zone, dn string
			var port, n int
			var first, last int64
			if rows.Scan(&to, &proto, &port, &action, &zone, &dn, &n, &first, &last) != nil || dcPrivate(to) {
				continue
			}
			ad, _ := netip.ParseAddr(to)
			known := knownResolver(ad)
			own := a.names.getFor(ip, to)
			var level, why, svc, name string
			switch {
			case known != "":
				level, svc, why = "known", known, "On the list of well-known public DNS services."
			case port == 443 && (looksLikeDNSName(own) || looksLikeDNSName(dn)):
				name = own
				if name == "" {
					name = dn
				}
				level, why = "probably", fmt.Sprintf("This device looked up %s just before, and that name belongs to a DNS service.", name)
			case port == 853:
				level, why = "port", "Port 853 is used only for encrypted DNS (DNS over TLS)."
			case port == 53:
				level, why = "port", "Port 53 is the ordinary DNS port, and this is not NopeWarden."
			default:
				continue // an ordinary website: not a DNS server as far as can be told
			}
			if name == "" {
				name = own
			}
			k := fmt.Sprintf("%s|%d|%s", to, port, proto)
			d := byKey[k]
			if d == nil {
				d = &dnsDest{Addr: to, Name: name, Service: svc, Level: level, Why: why, Port: port, Proto: proto, First: first, Last: last}
				byKey[k] = d
				order = append(order, k)
			}
			if action == "drop" {
				d.Stopped += n
			} else {
				d.Allowed += n
			}
			if first < d.First {
				d.First = first
			}
			if last > d.Last {
				d.Last = last
			}
		}
	}
	out := make([]*dnsDest, 0, len(order))
	probeMu.Lock()
	for _, k := range order {
		d := byKey[k]
		d.Every = everyWords(d.Allowed+d.Stopped, d.First, d.Last)
		if p := probeCache[d.Addr]; p != nil && time.Since(time.Unix(p.At, 0)) < 6*time.Hour {
			d.ProbeOut = p
			if p.DoH || p.DoT {
				d.Level = "confirmed"
				d.Why = "A DNS question sent to this address was answered: " + p.Detail
				if d.Name == "" && len(p.CertName) > 0 {
					d.Name = p.CertName[0]
				}
			}
		}
		out = append(out, d)
	}
	probeMu.Unlock()
	// Reverse names for the ones still unnamed, a few at a time, never long.
	named := 0
	for _, d := range out {
		if d.Name != "" || d.Service != "" || named >= 6 {
			continue
		}
		named++
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		if names, err := net.DefaultResolver.LookupAddr(ctx, d.Addr); err == nil && len(names) > 0 {
			d.Name = strings.TrimSuffix(names[0], ".")
			if looksLikeDNSName(d.Name) && d.Level != "known" && d.Level != "confirmed" {
				d.Level, d.Why = "probably", fmt.Sprintf("Its address resolves back to %s, a name that belongs to a DNS service.", d.Name)
			}
		}
		cancel()
	}
	var allowed, stopped int
	var first, last int64
	var who []string
	for _, d := range out {
		allowed += d.Allowed
		stopped += d.Stopped
		if d.Allowed+d.Stopped > 0 && (first == 0 || d.First < first) {
			first = d.First
		}
		if d.Last > last {
			last = d.Last
		}
		if d.Allowed > 0 {
			nm := d.Service
			if nm == "" {
				nm = d.Name
			}
			if nm == "" {
				nm = d.Addr
			}
			who = append(who, nm)
		}
	}
	sort.Strings(who)
	who = dcUnique(who)
	recA := a.Config.Setting("fw.log_allowed", "1") == "1"
	verdict, tone, detail := "Uses NopeWarden's DNS", "good", fmt.Sprintf("In the last %d hours this device made %d lookups through NopeWarden and no direct connections to a DNS server were seen.", hours, total)
	switch {
	case allowed > 0:
		every := everyWords(allowed, first, last)
		verdict, tone = "Also talks to a DNS server directly", "warn"
		if len(who) > 2 {
			who = append(who[:2], fmt.Sprintf("%d others", len(who)-2))
		}
		detail = fmt.Sprintf("%d connection%s went straight to %s, past NopeWarden", allowed, dcPlural(allowed), dcJoin(who))
		if every != "" {
			detail += ", " + every
		}
		detail += ". Lookups sent that way skip your block lists."
		if stopped > 0 {
			detail += fmt.Sprintf(" Another %d %s stopped.", stopped, dcWasWere(stopped))
		}
	case stopped > 0:
		verdict, tone = "Tried to skip NopeWarden's DNS and was stopped", "good"
		detail = fmt.Sprintf("%d connection%s to a DNS server %s stopped by the firewall. The device keeps trying, so it is set to use its own DNS.", stopped, dcPlural(stopped), dcWasWere(stopped))
	case !recA:
		verdict, tone = "Not sure", "warn"
		detail = "Allowed connections are not being recorded (Logs, Settings), so a connection that went straight to a DNS server would not show up here. Only blocked ones can."
	case total == 0:
		verdict, tone = "Not sure", ""
		detail = fmt.Sprintf("No lookups from this device in the last %d hours, and no connections to a DNS server either.", hours)
	}
	res["verdict"], res["tone"], res["detail"] = verdict, tone, detail
	res["servers"] = out
	res["block_dot"] = a.Config.Setting("fw.block_dot", "1") == "1"
	res["block_doh"] = a.Config.Setting("fw.block_doh", "0") == "1"
	res["limits"] = []string{
		"A server that insists on an exact name or web path may not answer the check even though it is a DNS server.",
		"A server that ignores strangers will look unknown.",
		"Only the first connection to a website is seen, so a device that keeps one connection open for hours counts once.",
		"With Encrypted Client Hello the name on a connection is hidden, so the name has to come from the device's own lookup.",
		"Only IPv4 connections and this device's IPv4 lookups are checked.",
	}
	api.JSON(w, 200, res)
}

const notHealthSQL = `NOT (client IN ('127.0.0.1','::1') AND qname='health.nopewarden.invalid')`

func dcPlural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
func dcWasWere(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}
func dcUnique(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}
func dcJoin(s []string) string {
	switch len(s) {
	case 0:
		return "a DNS server"
	case 1:
		return s[0]
	case 2:
		return s[0] + " and " + s[1]
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

// dnsProbeAddr asks one public address whether it is a DNS server. It connects out to a third
// party, so it runs only when someone presses "Check this address", and only for a public address.
func (a *App) dnsProbeAddr(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
	var in struct {
		Addr string `json:"addr"`
	}
	if !api.Decode(w, r, &in) {
		return
	}
	ad, err := netip.ParseAddr(strings.TrimSpace(in.Addr))
	if err != nil || dcPrivate(ad.String()) {
		api.Error(w, 400, "Only an address on the internet can be checked.")
		return
	}
	p := runDNSProbe(ad.Unmap().String())
	probeMu.Lock()
	probeCache[ad.Unmap().String()] = p
	if len(probeCache) > 500 {
		probeCache = map[string]*dnsProbe{ad.Unmap().String(): p}
	}
	probeMu.Unlock()
	a.auditEvent(c.User.Username, "info", "Checked whether an address is a DNS server", ad.String())
	api.JSON(w, 200, p)
}

func runDNSProbe(addr string) *dnsProbe {
	p := &dnsProbe{At: time.Now().Unix()}
	var details []string
	// The name on the certificate, from a TLS handshake on 443.
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	conf := &tls.Config{InsecureSkipVerify: true} // only reading the name; nothing is sent
	if conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(addr, "443"), conf); err == nil {
		for _, c := range conn.ConnectionState().PeerCertificates[:min(1, len(conn.ConnectionState().PeerCertificates))] {
			p.CertName = append(p.CertName, c.DNSNames...)
			if len(c.DNSNames) == 0 && c.Subject.CommonName != "" {
				p.CertName = append(p.CertName, c.Subject.CommonName)
			}
		}
		conn.Close()
		if len(p.CertName) > 5 {
			p.CertName = p.CertName[:5]
		}
	}
	host := addr
	for _, n := range p.CertName {
		if !strings.HasPrefix(n, "*") {
			host = n
			break
		}
	}
	// A DNS question over HTTPS.
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: host, InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(addr, "443"))
		}}}
	req, _ := http.NewRequest("GET", "https://"+host+"/dns-query?name=example.com&type=A", nil)
	req.Header.Set("Accept", "application/dns-json")
	if resp, err := hc.Do(req); err == nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		var j struct {
			Status int `json:"Status"`
			Answer []struct {
				Data string `json:"data"`
			} `json:"Answer"`
		}
		if resp.StatusCode == 200 && json.Unmarshal(body, &j) == nil && len(j.Answer) > 0 {
			p.DoH = true
			details = append(details, "it answers DNS over HTTPS")
		}
	}
	// A DNS question over TLS on 853.
	m := new(dns.Msg)
	m.SetQuestion("example.com.", dns.TypeA)
	cl := &dns.Client{Net: "tcp-tls", Timeout: 5 * time.Second, Dialer: dialer, TLSConfig: &tls.Config{ServerName: host, InsecureSkipVerify: true}}
	if resp, _, err := cl.Exchange(m, net.JoinHostPort(addr, "853")); err == nil && resp != nil && len(resp.Answer) > 0 {
		p.DoT = true
		details = append(details, "it answers DNS over TLS")
	}
	switch {
	case len(details) > 0:
		p.Detail = strings.Join(details, " and ")
		if len(p.CertName) > 0 {
			p.Detail += " (certificate name " + p.CertName[0] + ")"
		}
		p.Detail += "."
	case len(p.CertName) > 0:
		p.Detail = "It did not answer a DNS question, but its certificate is for " + strings.Join(p.CertName, ", ") + ". It may still be a DNS server that wants a different name or path."
	default:
		p.Detail = "It did not answer a DNS question and gave no certificate name, so it could not be told what it is."
	}
	return p
}

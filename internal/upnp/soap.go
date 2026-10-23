package upnp

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const svcType = "urn:schemas-upnp-org:service:WANIPConnection:1"

// Gateway answers the control requests of one LAN.
type Gateway struct {
	Table  *Table
	LAN    netip.Prefix
	ExtIP  func() string // current internet address, "" when there is none
	Name   string
	UUID   string
	mu     sync.Mutex
	bucket map[netip.Addr]*tokens
}

type tokens struct {
	n    float64
	last time.Time
}

// allow is a small per-device rate limit so one noisy device cannot flood the table.
func (g *Gateway) allow(ip netip.Addr) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.bucket == nil {
		g.bucket = map[netip.Addr]*tokens{}
	}
	now := time.Now()
	b := g.bucket[ip]
	if b == nil {
		if len(g.bucket) > 1024 {
			g.bucket = map[netip.Addr]*tokens{}
		}
		b = &tokens{n: 40, last: now}
		g.bucket[ip] = b
	}
	b.n += now.Sub(b.last).Seconds() * 10
	if b.n > 40 {
		b.n = 40
	}
	b.last = now
	if b.n < 1 {
		return false
	}
	b.n--
	return true
}

// Handler serves the device description and the control endpoint.
func (g *Gateway) Handler(base string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rootDesc.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, rootDesc(g.Name, g.UUID, base))
	})
	mux.HandleFunc("/WANIPCn.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, scpd)
	})
	mux.HandleFunc("/evt/IPConn", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "events are not supported", 501) })
	mux.HandleFunc("/ctl/IPConn", g.control)
	return mux
}

type envelope struct {
	Body struct {
		Inner struct {
			XMLName  xml.Name
			Children []struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			} `xml:",any"`
		} `xml:",any"`
	} `xml:"Body"`
}

func (g *Gateway) control(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	ip := ap.Addr().Unmap()
	if err != nil || !g.LAN.Contains(ip) {
		http.Error(w, "not from this network", 403)
		return
	}
	if !g.allow(ip) {
		http.Error(w, "slow down", 503)
		return
	}
	var env envelope
	if err := xml.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&env); err != nil {
		fault(w, ErrBadArgs)
		return
	}
	act := env.Body.Inner.XMLName.Local
	args := map[string]string{}
	for _, c := range env.Body.Inner.Children {
		args[c.XMLName.Local] = strings.TrimSpace(c.Text)
	}
	svc := svcType
	if sa := strings.Trim(r.Header.Get("SOAPAction"), `"`); strings.Contains(sa, "#") {
		svc = strings.SplitN(sa, "#", 2)[0]
	}
	out, err := g.do(ip, act, args)
	if err != nil {
		fault(w, err)
		return
	}
	var b strings.Builder
	for _, kv := range out {
		fmt.Fprintf(&b, "<%s>%s</%s>", kv[0], xmlEscape(kv[1]), kv[0])
	}
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`, act, xmlEscape(svc), b.String(), act)
}

func (g *Gateway) do(ip netip.Addr, act string, a map[string]string) ([][2]string, error) {
	port := func(k string) (int, error) {
		n, err := strconv.Atoi(a[k])
		if err != nil {
			return 0, ErrBadArgs
		}
		return n, nil
	}
	switch act {
	case "GetExternalIPAddress":
		return [][2]string{{"NewExternalIPAddress", g.ExtIP()}}, nil
	case "GetStatusInfo":
		st := "Connected"
		if g.ExtIP() == "" {
			st = "Disconnected"
		}
		return [][2]string{{"NewConnectionStatus", st}, {"NewLastConnectionError", "ERROR_NONE"}, {"NewUptime", "0"}}, nil
	case "GetConnectionTypeInfo":
		return [][2]string{{"NewConnectionType", "IP_Routed"}, {"NewPossibleConnectionTypes", "IP_Routed"}}, nil
	case "GetNATRSIPStatus":
		return [][2]string{{"NewRSIPAvailable", "0"}, {"NewNATEnabled", "1"}}, nil
	case "AddPortMapping":
		if g.ExtIP() == "" {
			return nil, &Error{501, "Action Failed"}
		}
		if a["NewRemoteHost"] != "" {
			return nil, ErrNotAuthorized
		}
		ext, err := port("NewExternalPort")
		if err != nil {
			return nil, err
		}
		in, err := port("NewInternalPort")
		if err != nil {
			return nil, err
		}
		if a["NewEnabled"] == "0" {
			return nil, ErrBadArgs
		}
		target, err := netip.ParseAddr(a["NewInternalClient"])
		if err != nil {
			return nil, ErrBadArgs
		}
		lease, _ := strconv.Atoi(a["NewLeaseDuration"])
		return nil, g.Table.Add(ip, target, strings.ToLower(a["NewProtocol"]), ext, in, cleanDesc(a["NewPortMappingDescription"]), time.Duration(lease)*time.Second)
	case "DeletePortMapping":
		ext, err := port("NewExternalPort")
		if err != nil {
			return nil, err
		}
		return nil, g.Table.Delete(ip, strings.ToLower(a["NewProtocol"]), ext)
	case "GetSpecificPortMappingEntry":
		ext, err := port("NewExternalPort")
		if err != nil {
			return nil, err
		}
		m, ok := g.Table.Get(strings.ToLower(a["NewProtocol"]), ext)
		if !ok {
			return nil, ErrNoSuchEntry
		}
		return entry(m, false), nil
	case "GetGenericPortMappingEntry":
		n, err := port("NewPortMappingIndex")
		if err != nil {
			return nil, err
		}
		m, ok := g.Table.Index(n)
		if !ok {
			return nil, ErrNoSuchEntry
		}
		return entry(m, true), nil
	}
	return nil, &Error{401, "Invalid Action"}
}

func entry(m Mapping, full bool) [][2]string {
	left := int(time.Until(m.Expires).Seconds())
	if left < 1 {
		left = 1
	}
	out := [][2]string{}
	if full {
		out = append(out, [2]string{"NewRemoteHost", ""}, [2]string{"NewExternalPort", strconv.Itoa(m.ExtPort)}, [2]string{"NewProtocol", strings.ToUpper(m.Proto)})
	}
	return append(out, [2]string{"NewInternalPort", strconv.Itoa(m.IntPort)}, [2]string{"NewInternalClient", m.IntAddr}, [2]string{"NewEnabled", "1"},
		[2]string{"NewPortMappingDescription", m.Desc}, [2]string{"NewLeaseDuration", strconv.Itoa(left)})
}

func cleanDesc(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fault(w http.ResponseWriter, err error) {
	e, ok := err.(*Error)
	if !ok {
		e = &Error{501, "Action Failed"}
	}
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(500)
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, e.Code, xmlEscape(e.Text))
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Package ndpi receives application names from gh-ndpi, the separate program that inspects the
// first packets of each flow with the nDPI library. Gatehouse itself never runs that library: the
// program has almost no rights and sends back one short line per flow, which is checked here before
// anything uses it.
package ndpi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Flow is what the program reports about one flow.
type Flow struct {
	Proto string   `json:"proto"`
	Src   string   `json:"src"`
	SP    int      `json:"sp"`
	Dst   string   `json:"dst"`
	DP    int      `json:"dp"`
	App   string   `json:"app"`
	Cat   string   `json:"cat"`
	Risk  []string `json:"risk"`
	Seen  int64    `json:"seen"`
}

func printable(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// ParseLine checks one line from the program strictly and refuses anything unexpected.
func ParseLine(b []byte) (Flow, error) {
	var f Flow
	if len(b) > 2048 {
		return f, errors.New("line too long")
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return f, err
	}
	if f.Proto != "tcp" && f.Proto != "udp" {
		return f, errors.New("bad protocol")
	}
	for _, a := range []string{f.Src, f.Dst} {
		if ip, err := netip.ParseAddr(a); err != nil || !ip.Is4() {
			return f, errors.New("bad address")
		}
	}
	if f.SP < 0 || f.SP > 65535 || f.DP < 0 || f.DP > 65535 {
		return f, errors.New("bad port")
	}
	if !printable(f.App, 64) || !printable(f.Cat, 64) {
		return f, errors.New("bad name")
	}
	if len(f.Risk) > 3 {
		return f, errors.New("too many warnings")
	}
	for _, r := range f.Risk {
		if !printable(r, 96) {
			return f, errors.New("bad warning")
		}
	}
	return f, nil
}

var generic = map[string]bool{"TLS": true, "HTTP": true, "HTTPS": true, "DNS": true, "QUIC": true, "NTP": true, "DHCP": true, "SSH": true,
	"ICMP": true, "SMTP": true, "IMAP": true, "POP3": true, "FTP": true, "Unknown": true, "mDNS": true, "SSDP": true, "NetBIOS": true,
	"STUN": true, "SNMP": true, "RDP": true, "SMB": true, "NFS": true, "IGMP": true, "LLMNR": true, "DHCPV6": true, "HTTP_Proxy": true,
	"SIP": true, "Syslog": true, "RTP": true, "RTSP": true, "RTMP": true, "OpenVPN": true, "WireGuard": true, "IPsec": true}

// Label turns nDPI's name ("TLS.YouTube") into the application a person would name. Plain protocols
// ("TLS", "DNS") are not applications, so they give no label.
func Label(app string) (string, bool) {
	if i := strings.LastIndex(app, "."); i >= 0 {
		app = app[i+1:]
	}
	app = strings.ReplaceAll(app, "_", " ")
	if app == "" || generic[strings.ReplaceAll(app, " ", "_")] {
		return "", false
	}
	return app, true
}

// Warning keeps only the cautions worth showing a person; nDPI raises many that are just noise.
func Warning(r string) bool {
	l := strings.ToLower(r)
	for _, k := range []string{"malware", "suspicious", "self-signed", "expired", "blacklist", "dga", "exploit", "binary application transfer", "clear-text credentials", "known protocol on non standard port", "tor", "possible exploit", "mismatch", "unsafe protocol", "missing sni"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

type tkey struct {
	proto string
	a, b  netip.AddrPort
}

func mk(proto, src string, sp int, dst string, dp int) (tkey, bool) {
	s, e1 := netip.ParseAddr(src)
	d, e2 := netip.ParseAddr(dst)
	if e1 != nil || e2 != nil {
		return tkey{}, false
	}
	return tkey{proto, netip.AddrPortFrom(s, uint16(sp)), netip.AddrPortFrom(d, uint16(dp))}, true
}

// Table remembers recent flows, bounded in size and age.
type Table struct {
	mu    sync.Mutex
	m     map[tkey]Flow
	order []tkey
	warns []Flow
	Max   int
	TTL   time.Duration
	Now   func() time.Time

	Seen      atomic.Int64
	Rejected  atomic.Int64
	Connected atomic.Int32
}

func NewTable() *Table {
	return &Table{m: map[tkey]Flow{}, Max: 20000, TTL: 30 * time.Minute, Now: time.Now}
}

// Add stores a flow.
func (t *Table) Add(f Flow) {
	k, ok := mk(f.Proto, f.Src, f.SP, f.Dst, f.DP)
	if !ok {
		return
	}
	f.Seen = t.Now().Unix()
	t.Seen.Add(1)
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, have := t.m[k]; !have {
		t.order = append(t.order, k)
		for len(t.order) > t.Max {
			delete(t.m, t.order[0])
			t.order = t.order[1:]
		}
	}
	t.m[k] = f
	for _, r := range f.Risk {
		if Warning(r) {
			t.warns = append(t.warns, f)
			if len(t.warns) > 200 {
				t.warns = t.warns[len(t.warns)-200:]
			}
			break
		}
	}
}

// Lookup finds the flow in either direction.
func (t *Table) Lookup(proto, src string, sp int, dst string, dp int) (Flow, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range []func() (tkey, bool){func() (tkey, bool) { return mk(proto, src, sp, dst, dp) }, func() (tkey, bool) { return mk(proto, dst, dp, src, sp) }} {
		if key, ok := k(); ok {
			if f, have := t.m[key]; have {
				if t.Now().Sub(time.Unix(f.Seen, 0)) > t.TTL {
					return Flow{}, false
				}
				return f, true
			}
		}
	}
	return Flow{}, false
}

// Warnings lists the recent flows that raised a caution worth showing, newest first.
func (t *Table) Warnings() []Flow {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Flow, 0, len(t.warns))
	for i := len(t.warns) - 1; i >= 0; i-- {
		f := t.warns[i]
		var keep []string
		for _, r := range f.Risk {
			if Warning(r) {
				keep = append(keep, r)
			}
		}
		f.Risk = keep
		out = append(out, f)
	}
	return out
}

// Serve listens on a local socket for the program until ctx ends. The socket is readable only by
// the program's own group, when that group exists.
func (t *Table) Serve(ctx context.Context, path, group string) error {
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	os.Chmod(path, 0o660)
	if group != "" {
		if g, err := user.LookupGroup(group); err == nil {
			if gid, err := strconv.Atoi(g.Gid); err == nil {
				os.Chown(path, -1, gid)
			}
		}
	}
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go t.serve(ctx, c)
	}
}

func (t *Table) serve(ctx context.Context, c net.Conn) {
	defer c.Close()
	t.Connected.Add(1)
	defer t.Connected.Add(-1)
	go func() { <-ctx.Done(); c.Close() }()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 4096), 4096)
	for sc.Scan() {
		f, err := ParseLine(sc.Bytes())
		if err != nil {
			t.Rejected.Add(1)
			continue
		}
		t.Add(f)
	}
}

// Package mdns copies multicast DNS announcements (Bonjour, AirPlay, Chromecast, printers)
// between networks the person has paired, so devices on one can find devices on the other.
// It only copies. Whether the devices may then talk to each other is still the firewall's call.
package mdns

import (
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
)

// Config says which interfaces are paired and what may be copied.
type Config struct {
	// Links maps an interface to the interfaces its announcements are copied to.
	Links map[string][]string
	// Services limits what is copied, for example "_ipp._tcp". Empty means everything.
	Services []string
}

// Plan turns pairs of groups of interfaces into the Links of a Config. A pair is two networks;
// each network may have several interfaces. Copying goes both ways.
func Plan(pairs [][2][]string) map[string][]string {
	out := map[string][]string{}
	add := func(from, to string) {
		if from == to {
			return
		}
		for _, x := range out[from] {
			if x == to {
				return
			}
		}
		out[from] = append(out[from], to)
	}
	for _, p := range pairs {
		for _, a := range p[0] {
			for _, b := range p[1] {
				add(a, b)
				add(b, a)
			}
		}
	}
	return out
}

// Wanted says whether a packet should be copied. With no service list everything is. Otherwise a
// question or an answer must be about one of the listed services (or ask which services exist).
func Wanted(pkt []byte, services []string) bool {
	if len(services) == 0 {
		return true
	}
	var m dns.Msg
	if m.Unpack(pkt) != nil {
		return false
	}
	match := func(name string) bool {
		name = strings.ToLower(name)
		if strings.HasPrefix(name, "_services._dns-sd._udp.") {
			return true
		}
		for _, s := range services {
			s = strings.ToLower(strings.TrimSpace(s))
			if s != "" && (strings.Contains(name, s+".") || strings.HasSuffix(name, s)) {
				return true
			}
		}
		return false
	}
	for _, q := range m.Question {
		if match(q.Name) {
			return true
		}
	}
	for _, sec := range [][]dns.RR{m.Answer, m.Ns, m.Extra} {
		for _, rr := range sec {
			if match(rr.Header().Name) {
				return true
			}
			if ptr, ok := rr.(*dns.PTR); ok && match(ptr.Ptr) {
				return true
			}
		}
	}
	return false
}

// Reflector runs the copying. SetConfig can be called at any time.
type Reflector struct {
	mu   sync.Mutex
	cfg  Config
	conn *ipv4.PacketConn
	raw  *net.UDPConn
	quit chan struct{}
	Logf func(string, ...any)
}

func (r *Reflector) logf(f string, a ...any) {
	if r.Logf != nil {
		r.Logf(f, a...)
		return
	}
	log.Printf(f, a...)
}

var group = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// SetConfig starts, changes or stops the reflector. An empty config stops it and frees the port.
func (r *Reflector) SetConfig(c Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sameConfig(r.cfg, c) && (r.conn != nil || len(c.Links) == 0) {
		return
	}
	r.stopLocked()
	r.cfg = c
	if len(c.Links) == 0 {
		return
	}
	if err := r.startLocked(); err != nil {
		r.logf("mdns: %v", err)
	}
}

func sameConfig(a, b Config) bool {
	if len(a.Links) != len(b.Links) || strings.Join(a.Services, ",") != strings.Join(b.Services, ",") {
		return false
	}
	for k, v := range a.Links {
		w := b.Links[k]
		if strings.Join(v, ",") != strings.Join(w, ",") {
			return false
		}
	}
	return true
}

func (r *Reflector) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *Reflector) stopLocked() {
	if r.quit != nil {
		close(r.quit)
		r.quit = nil
	}
	if r.raw != nil {
		r.raw.Close()
		r.raw, r.conn = nil, nil
	}
}

func (r *Reflector) startLocked() error {
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 5353})
	if err != nil {
		return errors.New("cannot listen for discovery announcements: " + err.Error())
	}
	pc := ipv4.NewPacketConn(ln)
	if err := pc.SetControlMessage(ipv4.FlagInterface|ipv4.FlagSrc|ipv4.FlagDst, true); err != nil {
		ln.Close()
		return err
	}
	pc.SetMulticastLoopback(false)
	joined := 0
	for name := range r.cfg.Links {
		ifi, err := net.InterfaceByName(name)
		if err != nil {
			continue // not there yet; it is tried again when the choice is saved or on restart
		}
		if err := pc.JoinGroup(ifi, group); err != nil {
			r.logf("mdns: cannot listen on %s: %v", name, err)
			continue
		}
		joined++
	}
	if joined == 0 {
		ln.Close()
		return errors.New("none of the paired networks has an address yet")
	}
	r.raw, r.conn, r.quit = ln, pc, make(chan struct{})
	go r.loop(pc, r.cfg, r.quit)
	return nil
}

func localAddrs() map[string]bool {
	out := map[string]bool{}
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		if ipn, ok := a.(*net.IPNet); ok {
			out[ipn.IP.String()] = true
		}
	}
	return out
}

func onLink(ifi *net.Interface, ip net.IP) bool {
	as, _ := ifi.Addrs()
	for _, a := range as {
		if ipn, ok := a.(*net.IPNet); ok && ipn.Contains(ip) {
			return true
		}
	}
	return false
}

func (r *Reflector) loop(pc *ipv4.PacketConn, cfg Config, quit chan struct{}) {
	buf := make([]byte, 9000)
	local, refreshed := localAddrs(), time.Now()
	for {
		n, cm, src, err := pc.ReadFrom(buf)
		if err != nil {
			select {
			case <-quit:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if cm == nil || cm.IfIndex == 0 {
			continue
		}
		ifi, err := net.InterfaceByIndex(cm.IfIndex)
		if err != nil {
			continue
		}
		targets := cfg.Links[ifi.Name]
		if len(targets) == 0 {
			continue
		}
		ua, ok := src.(*net.UDPAddr)
		if !ok {
			continue
		}
		if time.Since(refreshed) > 2*time.Second {
			local, refreshed = localAddrs(), time.Now()
		}
		// Our own copies come back to us; and only devices actually on the link count.
		if local[ua.IP.String()] || !onLink(ifi, ua.IP) {
			continue
		}
		if cm.Dst != nil && !cm.Dst.Equal(group.IP) {
			continue
		}
		pkt := append([]byte(nil), buf[:n]...)
		if !Wanted(pkt, cfg.Services) {
			continue
		}
		for _, t := range targets {
			to, err := net.InterfaceByName(t)
			if err != nil {
				continue
			}
			pc.WriteTo(pkt, &ipv4.ControlMessage{IfIndex: to.Index}, group)
		}
	}
}

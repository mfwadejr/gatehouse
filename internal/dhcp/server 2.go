package dhcp

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// Server binds the DHCP port and feeds requests to the Engine.
type Server struct {
	eng  *Engine
	mu   sync.Mutex
	conn *ipv4.PacketConn
	raw  *net.UDPConn
	// Addr is where to listen; ":67" in production.
	Addr string
	// ifaceIP finds our IPv4 address on an interface; replaceable for tests.
	ifaceIP func(ifi *net.Interface) net.IP
	Logf    func(format string, args ...any)
}

func NewServer(e *Engine, addr string) *Server {
	return &Server{eng: e, Addr: addr, ifaceIP: firstIPv4, Logf: log.Printf}
}

func firstIPv4(ifi *net.Interface) net.IP {
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.To4()
		}
	}
	return nil
}

// ifaceIPIn returns the interface address that falls inside any of the scopes' subnets, so
// multi-address interfaces answer from the right one.
func (s *Server) serverIP(ifi *net.Interface) net.IP {
	addrs, err := ifi.Addrs()
	if err == nil {
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			for _, sc := range s.eng.Scopes() {
				if sc.Iface == ifi.Name && sc.Net.Contains(ipn.IP) {
					return ipn.IP.To4()
				}
			}
		}
	}
	return s.ifaceIP(ifi)
}

// Start opens the socket. It returns once listening.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return errors.New("dhcp: already started")
	}
	ua, err := net.ResolveUDPAddr("udp4", s.Addr)
	if err != nil {
		return err
	}
	raw, err := net.ListenUDP("udp4", ua)
	if err != nil {
		return err
	}
	pc := ipv4.NewPacketConn(raw)
	if err := pc.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true); err != nil {
		raw.Close()
		return err
	}
	s.raw, s.conn = raw, pc
	go s.loop(pc)
	return nil
}

// Addr returns the bound address (for tests that listen on port 0).
func (s *Server) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.raw == nil {
		return nil
	}
	return s.raw.LocalAddr()
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.raw != nil {
		s.raw.Close()
		s.raw, s.conn = nil, nil
	}
}

func (s *Server) loop(pc *ipv4.PacketConn) {
	buf := make([]byte, 2048)
	for {
		n, cm, src, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.logf("dhcp: read: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		req, err := Parse(buf[:n])
		if err != nil {
			continue
		}
		in := Ingress{}
		var ifi *net.Interface
		if cm != nil && cm.IfIndex != 0 {
			if ifi, err = net.InterfaceByIndex(cm.IfIndex); err == nil {
				in.Iface = ifi.Name
				in.ServerIP = s.serverIP(ifi)
			}
		}
		if ifi == nil && s.Addr != "" && cm == nil {
			continue
		}
		resp, err := s.eng.Handle(req, in)
		if err != nil {
			s.logf("dhcp: %s: %v", req.CHAddr, err)
			continue
		}
		if resp == nil {
			continue
		}
		dst := destination(req, resp, src)
		out := &ipv4.ControlMessage{}
		if ifi != nil {
			out.IfIndex = ifi.Index
		}
		if in.ServerIP != nil {
			out.Src = in.ServerIP
		}
		if _, err := pc.WriteTo(resp.Marshal(), out, dst); err != nil {
			s.logf("dhcp: write: %v", err)
		}
	}
}

// destination follows RFC 2131 section 4.1: relay, renewing unicast, or broadcast.
func destination(req, resp *Packet, src net.Addr) *net.UDPAddr {
	if req.GIAddr != nil && !req.GIAddr.Equal(net.IPv4zero) {
		return &net.UDPAddr{IP: req.GIAddr, Port: 67}
	}
	port := 68
	if ua, ok := src.(*net.UDPAddr); ok && ua.Port != 68 && ua.Port != 0 && !ua.IP.IsUnspecified() {
		port = ua.Port // tests and unusual clients
	}
	if resp.Type() == Nak {
		return &net.UDPAddr{IP: net.IPv4bcast, Port: port}
	}
	if req.CIAddr != nil && !req.CIAddr.Equal(net.IPv4zero) {
		return &net.UDPAddr{IP: req.CIAddr, Port: port}
	}
	return &net.UDPAddr{IP: net.IPv4bcast, Port: port}
}

func (s *Server) logf(f string, a ...any) {
	if s.Logf != nil {
		s.Logf(f, a...)
	}
}

// Run expires leases and reloads on a timer until ctx ends.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.eng.Expire()
		}
	}
}

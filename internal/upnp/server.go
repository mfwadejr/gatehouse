package upnp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// LAN is one network that may use UPnP.
type LAN struct {
	Iface  string
	IP     netip.Addr
	Prefix netip.Prefix
}

// Config is what the server should be doing right now. The daemon recomputes it every few
// seconds from the settings and the state of the internet connections.
type Config struct {
	LANs   []LAN
	WanDev string // internet device mappings attach to; "" means no UPnP-enabled connection is up
	ExtIP  string
}

const ControlPort = 5000

// Server runs the listeners for every LAN in its config and keeps them current.
type Server struct {
	Table  *Table
	Name   string
	Config func() Config

	mu      sync.Mutex
	running map[string]context.CancelFunc
	wan     string
	ext     string
}

func NewServer(t *Table, name string, cfg func() Config) *Server {
	return &Server{Table: t, Name: name, Config: cfg, running: map[string]context.CancelFunc{}}
}

func (s *Server) extIP() string { s.mu.Lock(); defer s.mu.Unlock(); return s.ext }

// WanDev is the connection mappings currently attach to.
func (s *Server) WanDev() string { s.mu.Lock(); defer s.mu.Unlock(); return s.wan }

func uuidFor(name string) string {
	h := sha256.Sum256([]byte("gatehouse-upnp:" + name))
	x := hex.EncodeToString(h[:16])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

// Run keeps the listeners in step with the config until ctx ends.
func (s *Server) Run(ctx context.Context) {
	tk := time.NewTicker(5 * time.Second)
	defer tk.Stop()
	for {
		s.reconcile(ctx)
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for _, c := range s.running {
				c()
			}
			s.mu.Unlock()
			return
		case <-tk.C:
			s.Table.Expire()
		}
	}
}

func (s *Server) reconcile(ctx context.Context) {
	cfg := s.Config()
	s.mu.Lock()
	if cfg.WanDev != s.wan {
		s.wan = cfg.WanDev
		// Ports opened on one connection mean nothing on another.
		defer s.Table.Clear("the internet connection changed")
	}
	s.ext = cfg.ExtIP
	want := map[string]LAN{}
	if cfg.WanDev != "" {
		for _, l := range cfg.LANs {
			want[l.Iface+"|"+l.IP.String()] = l
		}
	}
	for k, c := range s.running {
		if _, ok := want[k]; !ok {
			c()
			delete(s.running, k)
		}
	}
	for k, l := range want {
		if _, ok := s.running[k]; ok {
			continue
		}
		lctx, cancel := context.WithCancel(ctx)
		if err := s.start(lctx, l); err != nil {
			cancel()
			s.Table.event("error", "UPnP on "+l.Iface+": "+err.Error())
			continue
		}
		s.running[k] = cancel
	}
	s.mu.Unlock()
}

func (s *Server) start(ctx context.Context, l LAN) error {
	ln, err := net.Listen("tcp4", net.JoinHostPort(l.IP.String(), strconv.Itoa(ControlPort)))
	if err != nil {
		return err
	}
	uuid := uuidFor(s.Name)
	base := "http://" + net.JoinHostPort(l.IP.String(), strconv.Itoa(ControlPort)) + "/"
	g := &Gateway{Table: s.Table, LAN: l.Prefix, ExtIP: s.extIP, Name: s.Name, UUID: uuid}
	srv := &http.Server{Handler: g.Handler(base), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10}
	go srv.Serve(ln)
	go func() { <-ctx.Done(); srv.Close() }()
	ifi, err := net.InterfaceByName(l.Iface)
	if err != nil {
		srv.Close()
		return err
	}
	go func() {
		if err := serveSSDP(ctx, ifi, l.Prefix, base+"rootDesc.xml", uuid); err != nil {
			s.Table.event("error", "UPnP discovery on "+l.Iface+": "+err.Error())
		}
	}()
	return nil
}

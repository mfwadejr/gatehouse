// Package netd is the privileged network service: it installs generated network
// configuration with a confirmation window and runs the multi-WAN monitor.
package netd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gatehouse/internal/netcfg"
	"gatehouse/internal/wanmon"
)

// Plan is the request the daemon sends: what to configure and how to watch the links.
type Plan struct {
	Config  netcfg.Config `json:"config"`
	Mode    string        `json:"mode"` // failover or balance
	Links   []LinkSpec    `json:"links"`
	Seconds int           `json:"seconds"`
}

// LinkSpec is the serialisable form of wanmon.Link.
type LinkSpec struct {
	Name     string `json:"name"`
	Dev      string `json:"dev"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Table    int    `json:"table"`
	Mark     uint32 `json:"mark"`
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Interval int    `json:"interval"`
	Failures int    `json:"failures"`
	Recover  int    `json:"recover"`
}

func (s LinkSpec) link() wanmon.Link {
	return wanmon.Link{Name: s.Name, Dev: s.Dev, Priority: s.Priority, Weight: s.Weight, Table: s.Table,
		Mark: s.Mark, Kind: s.Kind, Target: s.Target, Interval: time.Duration(s.Interval) * time.Second,
		Failures: s.Failures, Recover: s.Recover}
}

type monitorFile struct {
	Mode  string     `json:"mode"`
	Links []LinkSpec `json:"links"`
}

// Service ties the installer and the monitor together.
type Service struct {
	Inst     *netcfg.Installer
	StateDir string
	NewMon   func(links []wanmon.Link, mode wanmon.Mode) *wanmon.Monitor

	mu      sync.Mutex
	cancel  context.CancelFunc
	mon     *wanmon.Monitor
	staged  *monitorFile // monitor settings waiting for confirmation
	root    context.Context
	running sync.WaitGroup
}

func New(inst *netcfg.Installer, stateDir string) *Service {
	return &Service{Inst: inst, StateDir: stateDir, NewMon: func(l []wanmon.Link, m wanmon.Mode) *wanmon.Monitor {
		return wanmon.New(l, m, wanmon.NetChecker{}, &wanmon.IPRouter{Metric: 50})
	}}
}

func (s *Service) path(n string) string { return filepath.Join(s.StateDir, n) }

// Start launches the monitor from the saved settings, if any.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root = ctx
	if err := os.MkdirAll(s.StateDir, 0o700); err != nil {
		return err
	}
	// A change that was never confirmed is discarded on boot, as for the firewall.
	if _, _, ok := s.Inst.Pending(); ok {
		s.Inst.Rollback()
	}
	b, err := os.ReadFile(s.path("monitor.json"))
	if err != nil {
		return nil
	}
	var mf monitorFile
	if json.Unmarshal(b, &mf) != nil {
		return nil
	}
	s.runLocked(&mf)
	return nil
}

func (s *Service) runLocked(mf *monitorFile) {
	if s.cancel != nil {
		s.cancel()
		s.running.Wait()
		s.cancel, s.mon = nil, nil
	}
	if mf == nil || len(mf.Links) < 2 || s.root == nil {
		return // one connection needs no monitor; systemd-networkd owns its route
	}
	links := make([]wanmon.Link, len(mf.Links))
	for i, l := range mf.Links {
		links[i] = l.link()
	}
	mode := wanmon.Failover
	if mf.Mode == "balance" {
		mode = wanmon.Balance
	}
	m := s.NewMon(links, mode)
	ctx, cancel := context.WithCancel(s.root)
	s.cancel, s.mon = cancel, m
	s.running.Add(1)
	go func() { defer s.running.Done(); m.Run(ctx) }()
}

// Stop ends the monitor.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.running.Wait()
		s.cancel, s.mon = nil, nil
	}
}

func validateLinks(ls []LinkSpec) error {
	seen := map[string]bool{}
	for _, l := range ls {
		if l.Name == "" || l.Dev == "" || seen[l.Name] {
			return errors.New("every internet connection needs a unique name and a device")
		}
		seen[l.Name] = true
		if l.Table < 100 || l.Table > 250 || l.Mark == 0 {
			return errors.New("invalid routing table or mark")
		}
		if l.Kind != "icmp" && l.Kind != "tcp" {
			return errors.New("connection check must be icmp or tcp")
		}
		if l.Interval < 0 || l.Interval > 3600 || l.Failures < 0 || l.Failures > 100 || l.Recover < 0 || l.Recover > 100 {
			return errors.New("connection check timing is out of range")
		}
	}
	return nil
}

// Apply validates the plan, installs it and starts the confirmation window.
func (s *Service) Apply(p Plan, window time.Duration) (string, time.Time, error) {
	if p.Mode != "failover" && p.Mode != "balance" {
		return "", time.Time{}, errors.New("mode must be failover or balance")
	}
	if err := validateLinks(p.Links); err != nil {
		return "", time.Time{}, err
	}
	if err := p.Config.Validate(); err != nil {
		return "", time.Time{}, err
	}
	out, err := netcfg.Generate(&p.Config)
	if err != nil {
		return "", time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, dl, err := s.Inst.Apply(out, window)
	if err != nil {
		return "", time.Time{}, err
	}
	s.staged = &monitorFile{Mode: p.Mode, Links: p.Links}
	// Run the monitor against the new links right away so failures show up during the window.
	s.runLocked(s.staged)
	return tok, dl, nil
}

// Confirm keeps the change and saves the monitor settings.
func (s *Service) Confirm(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Inst.Confirm(token); err != nil {
		return err
	}
	if s.staged != nil {
		b, _ := json.Marshal(s.staged)
		if err := os.WriteFile(s.path("monitor.json"), b, 0o600); err != nil {
			return err
		}
		s.staged = nil
	}
	return nil
}

// Rollback undoes an unconfirmed change and returns the monitor to its saved settings.
func (s *Service) Rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.Inst.Rollback()
	s.staged = nil
	var mf *monitorFile
	if b, e := os.ReadFile(s.path("monitor.json")); e == nil {
		var x monitorFile
		if json.Unmarshal(b, &x) == nil {
			mf = &x
		}
	}
	s.runLocked(mf)
	return err
}

func (s *Service) Pending() (string, time.Time, bool) { return s.Inst.Pending() }

// Status reports each connection's health.
func (s *Service) Status() map[string]wanmon.State {
	s.mu.Lock()
	m := s.mon
	s.mu.Unlock()
	if m == nil {
		return map[string]wanmon.State{}
	}
	return m.Snapshot()
}

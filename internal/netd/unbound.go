package netd

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// "Resolve directly": NopeWarden's DNS server asks Unbound, which looks names up itself starting at
// the root servers (and checks DNSSEC signatures) instead of asking a forwarder such as Cloudflare.
// Unbound only listens on this machine, on port 5335; devices never talk to it.

const (
	unboundConf   = "etc/unbound/unbound.conf.d/nopewarden.conf"
	unboundBinary = "usr/sbin/unbound"
	unboundUnit   = "unbound.service"
	// Debian's Unbound package points DNSSEC at this file. The service creates it when it first starts,
	// but ours stays off until the console turns it on, so it is made here before Unbound's settings are
	// checked (a check against a missing file fails).
	unboundAnchor = "var/lib/unbound/root.key"
	// The root trust anchor Debian ships with the dns-root-data package: the same key, no network needed.
	unboundAnchorShipped = "usr/share/dns/root.key"
	// UnboundPort is where Unbound listens (this machine only).
	UnboundPort = 5335
)

// UnboundPlan says whether Unbound runs and how.
type UnboundPlan struct {
	Enabled bool `json:"enabled"`
	DNSSEC  bool `json:"dnssec"`
	IPv6    bool `json:"ipv6"` // look names up over IPv6 as well
}

// UnboundInfo is what the console shows about Unbound.
type UnboundInfo struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version,omitempty"`
	Why       string `json:"why,omitempty"` // plain reason it is not running, when it should be
}

// UnboundConfig is the settings file for a plan (a pure function, so it can be tested).
func UnboundConfig(p UnboundPlan) string {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	modules := "iterator"
	if p.DNSSEC {
		modules = "validator iterator"
	}
	return fmt.Sprintf(`# Written by NopeWarden. Changes made here are overwritten.
server:
    port: %d
    interface: 127.0.0.1
    interface: ::1
    access-control: 0.0.0.0/0 refuse
    access-control: ::/0 refuse
    access-control: 127.0.0.0/8 allow
    access-control: ::1/128 allow
    do-ip6: %s
    do-udp: yes
    do-tcp: yes
    hide-identity: yes
    hide-version: yes
    qname-minimisation: yes
    harden-glue: yes
    harden-dnssec-stripped: yes
    prefetch: yes
    prefetch-key: yes
    num-threads: 2
    msg-cache-size: 16m
    rrset-cache-size: 32m
    val-log-level: 2
    module-config: "%s"
`, UnboundPort, yn(p.IPv6), modules)
}

// UnboundStatus says whether Unbound is installed and running.
func (s *Service) UnboundStatus() UnboundInfo {
	info := UnboundInfo{}
	if _, err := os.Stat(s.rootPath(unboundBinary)); err != nil {
		info.Why = "Unbound is not installed on this machine yet. It arrives with the next update."
		return info
	}
	info.Installed = true
	if out, err := sysRun(10*time.Second, "unbound", "-V"); err == nil {
		if f := strings.Fields(out); len(f) >= 2 && strings.EqualFold(f[0], "Version") {
			info.Version = f[1]
		}
	}
	out, _ := sysRun(10*time.Second, "systemctl", "is-active", unboundUnit)
	info.Running = strings.TrimSpace(out) == "active"
	if !info.Running {
		info.Why = "Unbound is installed but not running."
	}
	return info
}

// ensureUnboundAnchor makes sure Unbound's DNSSEC trust anchor file exists, so a fresh install can turn
// "Resolve directly" on. It copies the key Debian ships (no network needed), and falls back to the package's
// own helper and then to unbound-anchor. Unbound keeps the file up to date itself once it runs.
func (s *Service) ensureUnboundAnchor() error {
	dst := s.rootPath(unboundAnchor)
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if b, err := os.ReadFile(s.rootPath(unboundAnchorShipped)); err == nil && len(b) > 0 {
		if err := writeFileAtomic(dst, b, 0o644); err != nil {
			return err
		}
	} else {
		// Not shipped on this machine: ask the package's own helper, then unbound-anchor itself.
		sysRun(60*time.Second, "/usr/libexec/unbound-helper", "root_trust_anchor_update")
		if fi, err := os.Stat(dst); err != nil || fi.Size() == 0 {
			sysRun(60*time.Second, "unbound-anchor", "-a", dst)
		}
		if fi, err := os.Stat(dst); err != nil || fi.Size() == 0 {
			return errors.New("Unbound's DNSSEC key file is missing and could not be created")
		}
	}
	// Unbound runs as its own user and must be able to update the file.
	if u, err := user.Lookup("unbound"); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		os.Chown(dst, uid, gid)
	}
	return nil
}

// UnboundApply writes Unbound's settings and starts it, or stops it.
func (s *Service) UnboundApply(p UnboundPlan) error {
	conf := s.rootPath(unboundConf)
	if !p.Enabled {
		sysRun(30*time.Second, "systemctl", "disable", "--now", unboundUnit)
		os.Remove(conf)
		return nil
	}
	if _, err := os.Stat(s.rootPath(unboundBinary)); err != nil {
		return errors.New("Unbound is not installed on this machine yet. It arrives with the next update")
	}
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		return err
	}
	if err := s.ensureUnboundAnchor(); err != nil {
		return fmt.Errorf("%v. Your name lookups are unchanged", err)
	}
	old, hadOld := os.ReadFile(conf)
	if err := writeFileAtomic(conf, []byte(UnboundConfig(p)), 0o644); err != nil {
		return err
	}
	// A settings file Unbound rejects must never stay in place: check it first, and put the old one back.
	if out, err := sysRun(15*time.Second, "unbound-checkconf"); err != nil {
		if hadOld == nil {
			writeFileAtomic(conf, old, 0o644)
		} else {
			os.Remove(conf)
		}
		return fmt.Errorf("Unbound rejected its settings: %s", strings.TrimSpace(out))
	}
	if out, err := sysRun(30*time.Second, "systemctl", "enable", unboundUnit); err != nil {
		return fmt.Errorf("could not turn Unbound on: %s", strings.TrimSpace(out))
	}
	if out, err := sysRun(60*time.Second, "systemctl", "restart", unboundUnit); err != nil {
		return fmt.Errorf("could not start Unbound: %s", strings.TrimSpace(out))
	}
	return nil
}

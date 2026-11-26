package netd

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	flowAcctProc   = "proc/sys/net/netfilter/nf_conntrack_acct"
	flowAcctSysctl = "etc/sysctl.d/60-nopewarden-flows.conf"
	conntrackProc  = "proc/net/nf_conntrack"
	maxConntrack   = 32 << 20
)

// FlowAcct turns packet and byte counting on connections on or off, and keeps it that way across
// restarts. Flow export needs the counts; nothing else does.
func (s *Service) FlowAcct(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	conf := s.rootPath(flowAcctSysctl)
	if on {
		if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(conf, []byte("# Written by NopeWarden (flow export)\nnet.netfilter.nf_conntrack_acct = 1\n"), 0o644); err != nil {
			return err
		}
	} else {
		os.Remove(conf)
	}
	if err := os.WriteFile(s.rootPath(flowAcctProc), []byte(v), 0o644); err != nil {
		return fmt.Errorf("this system cannot count traffic on connections: %v", err)
	}
	return nil
}

// ConntrackDump returns the kernel's list of live connections as text.
func (s *Service) ConntrackDump() (string, error) {
	f, err := os.Open(s.rootPath(conntrackProc))
	if err != nil {
		return "", fmt.Errorf("cannot read the connection list: %v", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxConntrack))
	return string(b), err
}

// ConntrackEnd ends one connection by removing it from the kernel's list, so the next packet is treated
// as a new connection and meets the current rules. Needs the conntrack tool.
func (s *Service) ConntrackEnd(proto, src string, sport int, dst string, dport int) error {
	if proto != "tcp" && proto != "udp" {
		return fmt.Errorf("only TCP and UDP connections can be ended")
	}
	a, err1 := netip.ParseAddr(src)
	b, err2 := netip.ParseAddr(dst)
	if err1 != nil || err2 != nil || !a.Is4() || !b.Is4() || sport <= 0 || sport > 65535 || dport <= 0 || dport > 65535 {
		return fmt.Errorf("that is not a connection NopeWarden can end")
	}
	if _, err := lookPath("conntrack"); err != nil {
		return fmt.Errorf("NopeWarden needs the conntrack tool to end a connection; it is added to the next system image, or run: apt install conntrack")
	}
	out, err := sysRun(10*time.Second, "conntrack", "-D", "-p", proto, "-s", src, "-d", dst, "--sport", strconv.Itoa(sport), "--dport", strconv.Itoa(dport))
	if err != nil && !containsAny(out, "0 flow entries", "1 flow entries") {
		return fmt.Errorf("the connection could not be ended: %s", smartLastLine(out))
	}
	return nil
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				return true
			}
		}
	}
	return false
}

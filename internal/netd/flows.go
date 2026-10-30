package netd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	flowAcctProc   = "proc/sys/net/netfilter/nf_conntrack_acct"
	flowAcctSysctl = "etc/sysctl.d/60-gatehouse-flows.conf"
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
		if err := writeFileAtomic(conf, []byte("# Written by Gatehouse (flow export)\nnet.netfilter.nf_conntrack_acct = 1\n"), 0o644); err != nil {
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

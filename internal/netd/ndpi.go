package netd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NDPIPlan says whether the traffic-labelling program runs, and on which ports. It watches copies of
// the packets on the inside ports and reports a label per connection; it never changes traffic.
type NDPIPlan struct {
	Enabled bool     `json:"enabled"`
	Ifaces  []string `json:"ifaces"`
}

const (
	ndpiConf   = "etc/gatehouse/ndpi.conf"
	ndpiBinary = "usr/lib/gatehouse/gh-ndpi"
	ndpiUnit   = "gatehouse-ndpi.service"
	// NDPISocketDir is where the program and Gatehouse meet; tmpfiles.d creates it at boot.
	NDPISocketDir = "/run/gatehouse/ndpi"
)

// NDPIInstalled reports whether this system has the program (only ISO builds that could compile it do).
func (s *Service) NDPIInstalled() bool {
	st, err := os.Stat(s.rootPath(ndpiBinary))
	return err == nil && st.Mode().IsRegular()
}

// NDPIApply starts or stops the program. With nothing to watch it is stopped.
func (s *Service) NDPIApply(p NDPIPlan) error {
	conf := s.rootPath(ndpiConf)
	if !p.Enabled || len(p.Ifaces) == 0 {
		wifiRun(30*time.Second, "systemctl", "disable", "--now", ndpiUnit)
		os.Remove(conf)
		return nil
	}
	if !s.NDPIInstalled() {
		return fmt.Errorf("this system does not have the traffic-labelling program (it comes with a fresh install image)")
	}
	var b strings.Builder
	for _, d := range p.Ifaces {
		if !reHADev.MatchString(d) {
			return fmt.Errorf("%q is not a usable port name", d)
		}
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(d)
	}
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		return err
	}
	old, _ := os.ReadFile(conf)
	nw := "# Written by Gatehouse\nIFACES=" + b.String() + "\n"
	if err := writeFileAtomic(conf, []byte(nw), 0o644); err != nil {
		return err
	}
	if string(old) == nw {
		wifiRun(30*time.Second, "systemctl", "start", ndpiUnit)
		return nil
	}
	wifiRun(30*time.Second, "systemctl", "enable", ndpiUnit)
	if out, err := wifiRun(60*time.Second, "systemctl", "restart", ndpiUnit); err != nil {
		return fmt.Errorf("the labelling program did not start: %s", strings.TrimSpace(out))
	}
	return nil
}

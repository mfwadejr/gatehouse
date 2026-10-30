package netd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gatehouse/internal/ha"
)

// HAPlan is what the helper puts in place for a pair of units: the settings for the program that
// moves the shared addresses (keepalived) and the one that copies the connection table
// (conntrackd). Disabled stops both and removes their files, leaving a single ordinary unit.
type HAPlan struct {
	Enabled    bool   `json:"enabled"`
	Keepalived string `json:"keepalived"`
	Conntrackd string `json:"conntrackd"`
	// WanDevs are the internet ports that stay down while this unit is the standby.
	WanDevs []string `json:"wan_devs"`
}

const (
	haKeepalived = "etc/keepalived/keepalived.conf"
	haConntrackd = "etc/conntrackd/conntrackd.conf"
	haSysctl     = "etc/sysctl.d/90-gatehouse-ha.conf"
	haWan        = "etc/gatehouse/ha-wan"
)

var reHADev = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

func haText(what, s string) error {
	if len(s) == 0 || len(s) > 16<<10 || strings.ContainsRune(s, 0) || !strings.HasPrefix(s, "# Written by Gatehouse") {
		return fmt.Errorf("the %s settings are not in the form Gatehouse writes", what)
	}
	return nil
}

// HAApply installs or removes the high-availability services.
func (s *Service) HAApply(p HAPlan) error {
	ka, ct := s.rootPath(haKeepalived), s.rootPath(haConntrackd)
	notify, sc := s.rootPath(strings.TrimPrefix(ha.NotifyPath, "/")), s.rootPath(haSysctl)
	if !p.Enabled {
		wifiRun(30*time.Second, "systemctl", "disable", "--now", "keepalived.service")
		wifiRun(30*time.Second, "systemctl", "disable", "--now", "conntrackd.service")
		for _, f := range []string{ka, ct, notify, sc, s.rootPath(haWan)} {
			os.Remove(f)
		}
		return nil
	}
	if err := haText("keepalived", p.Keepalived); err != nil {
		return err
	}
	if err := haText("conntrackd", p.Conntrackd); err != nil {
		return err
	}
	var wans strings.Builder
	for _, d := range p.WanDevs {
		if !reHADev.MatchString(d) {
			return fmt.Errorf("%q is not a usable port name", d)
		}
		wans.WriteString(d + "\n")
	}
	for _, d := range []string{filepath.Dir(s.rootPath(haWan)), filepath.Dir(ka), filepath.Dir(ct), filepath.Dir(notify), filepath.Dir(sc)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := writeFileAtomic(s.rootPath(haWan), []byte(wans.String()), 0o644); err != nil {
		return err
	}
	// Remember what was there, so a configuration the program rejects never stays.
	oldKA, _ := os.ReadFile(ka)
	if err := writeFileAtomic(notify, []byte(ha.NotifyScript), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(ct, []byte(p.Conntrackd), 0o600); err != nil {
		return err
	}
	// Services may bind to the shared address before this unit holds it.
	if err := writeFileAtomic(sc, []byte("net.ipv4.ip_nonlocal_bind = 1\n"), 0o644); err != nil {
		return err
	}
	wifiRun(10*time.Second, "sysctl", "-w", "net.ipv4.ip_nonlocal_bind=1")
	if err := writeFileAtomic(ka, []byte(p.Keepalived), 0o600); err != nil {
		return err
	}
	if out, err := wifiRun(15*time.Second, "keepalived", "--config-test", "-f", ka); err != nil {
		if len(oldKA) > 0 {
			writeFileAtomic(ka, oldKA, 0o600)
		} else {
			os.Remove(ka)
		}
		return errors.New("the high-availability program rejected its settings: " + strings.TrimSpace(out))
	}
	for _, unit := range []string{"conntrackd.service", "keepalived.service"} {
		if out, err := wifiRun(30*time.Second, "systemctl", "enable", unit); err != nil {
			return fmt.Errorf("could not enable %s: %s", unit, strings.TrimSpace(out))
		}
		if out, err := wifiRun(30*time.Second, "systemctl", "restart", unit); err != nil {
			return fmt.Errorf("could not start %s: %s", unit, strings.TrimSpace(out))
		}
	}
	return nil
}

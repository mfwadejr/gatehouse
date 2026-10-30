package netd

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
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
	// HeartDev and HeartCIDR describe the optional cable to the partner: the port, and this unit's
	// address on it. Empty means no cable.
	// WarmDevs are internet ports kept connected but silent while this unit is standby (instead of
	// being taken down). WanStatic lists "port address" for fixed addresses to announce on takeover,
	// and WanMACs "port hardware-address" to copy so the provider sees the same device.
	WarmDevs  []string `json:"warm_devs,omitempty"`
	WanStatic []string `json:"wan_static,omitempty"`
	WanMACs   []string `json:"wan_macs,omitempty"`
	HeartDev  string   `json:"heart_dev,omitempty"`
	HeartCIDR string   `json:"heart_cidr,omitempty"`
}

const haHeartNet = "etc/systemd/network/05-gatehouse-heart.network"

// heartApply writes or removes the network file for the cable. The cable port belongs to this file
// alone; the name sorts first so nothing else claims the port.
func (s *Service) heartApply(dev, cidr string) error {
	f := s.rootPath(haHeartNet)
	if dev == "" {
		if _, err := os.Stat(f); err == nil {
			os.Remove(f)
			wifiRun(15*time.Second, "networkctl", "reload")
		}
		return nil
	}
	if !reHADev.MatchString(dev) {
		return fmt.Errorf("%q is not a usable port name", dev)
	}
	if _, err := netip.ParsePrefix(cidr); err != nil {
		return fmt.Errorf("%q is not an address", cidr)
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	body := "# Written by Gatehouse (cable to the partner unit)\n[Match]\nName=" + dev + "\n\n[Network]\nAddress=" + cidr +
		"\nLinkLocalAddressing=no\nIPv6AcceptRA=no\nLLDP=no\nEmitLLDP=no\n\n[Link]\nRequiredForOnline=no\n"
	old, _ := os.ReadFile(f)
	if string(old) == body {
		return nil
	}
	if err := writeFileAtomic(f, []byte(body), 0o644); err != nil {
		return err
	}
	wifiRun(15*time.Second, "networkctl", "reload")
	wifiRun(15*time.Second, "networkctl", "reconfigure", dev)
	return nil
}

const (
	haKeepalived = "etc/keepalived/keepalived.conf"
	haConntrackd = "etc/conntrackd/conntrackd.conf"
	haSysctl     = "etc/sysctl.d/90-gatehouse-ha.conf"
	haWan        = "etc/gatehouse/ha-wan"
	haWanWarm    = "etc/gatehouse/ha-wan-warm"
	haWanStatic  = "etc/gatehouse/ha-wan-static"
	haWanMACs    = "etc/gatehouse/ha-wan-macs"
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
		for _, f := range []string{ka, ct, notify, sc, s.rootPath(haWan), s.rootPath(haWanWarm), s.rootPath(haWanStatic), s.rootPath(haWanMACs)} {
			os.Remove(f)
		}
		return s.heartApply("", "")
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
	var warm, stat, macs strings.Builder
	for _, d := range p.WarmDevs {
		if !reHADev.MatchString(d) {
			return fmt.Errorf("%q is not a usable port name", d)
		}
		warm.WriteString(d + "\n")
	}
	for _, l := range p.WanStatic {
		d, a, _ := strings.Cut(l, " ")
		if _, err := netip.ParseAddr(a); err != nil || !reHADev.MatchString(d) {
			return fmt.Errorf("%q is not a port and an address", l)
		}
		stat.WriteString(d + " " + a + "\n")
	}
	for _, l := range p.WanMACs {
		d, m, _ := strings.Cut(l, " ")
		if _, err := net.ParseMAC(m); err != nil || !reHADev.MatchString(d) {
			return fmt.Errorf("%q is not a port and a hardware address", l)
		}
		macs.WriteString(d + " " + m + "\n")
	}
	for _, d := range []string{filepath.Dir(s.rootPath(haWan)), filepath.Dir(ka), filepath.Dir(ct), filepath.Dir(notify), filepath.Dir(sc)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := writeFileAtomic(s.rootPath(haWan), []byte(wans.String()), 0o644); err != nil {
		return err
	}
	for f, body := range map[string]string{haWanWarm: warm.String(), haWanStatic: stat.String(), haWanMACs: macs.String()} {
		if err := writeFileAtomic(s.rootPath(f), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := s.heartApply(p.HeartDev, p.HeartCIDR); err != nil {
		return err
	}
	// Remember what was there, so a configuration the program rejects never stays.
	oldKA, _ := os.ReadFile(ka)
	if err := writeFileAtomic(notify, []byte(ha.NotifyScriptFor(len(p.WarmDevs) > 0)), 0o755); err != nil {
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

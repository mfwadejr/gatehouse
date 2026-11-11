package netd

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The management port: one port that carries only a management address. It is not a network the
// device serves: no routing through it, no DHCP server, no address translation. The address comes
// from a DHCP server or is typed in. A gateway, when there is one, is used only to answer people
// on other subnets, through a small routing table of its own, so it can never become this device's
// route to the internet.

const (
	mgmtNetFile = "etc/systemd/network/06-nopewarden-mgmt.network"
	// MgmtTable is the routing table used for replies from the management port's own address.
	MgmtTable = 195
)

// MgmtPlan says how to set the management port up. An empty Dev removes it.
type MgmtPlan struct {
	Dev     string `json:"dev"`
	Mode    string `json:"mode"`    // dhcp or static
	CIDR    string `json:"cidr"`    // static only: address and prefix
	Gateway string `json:"gateway"` // static only, optional
}

// MgmtNetwork returns the network file for a plan, without writing anything, so it can be tested
// anywhere. The file is named so it sorts before the files for the other ports.
func MgmtNetwork(p MgmtPlan) (string, error) {
	if !reHADev.MatchString(p.Dev) {
		return "", fmt.Errorf("%q is not a usable port name", p.Dev)
	}
	var b strings.Builder
	b.WriteString("# Written by NopeWarden (management port)\n[Match]\nName=" + p.Dev + "\n\n[Network]\n")
	switch p.Mode {
	case "dhcp":
		b.WriteString("DHCP=ipv4\n")
	case "static":
		pf, err := netip.ParsePrefix(p.CIDR)
		if err != nil || !pf.Addr().Is4() {
			return "", fmt.Errorf("%q is not an IPv4 address with a prefix", p.CIDR)
		}
		b.WriteString("Address=" + pf.String() + "\n")
	default:
		return "", fmt.Errorf("choose DHCP or a fixed address")
	}
	b.WriteString("LinkLocalAddressing=no\nIPv6AcceptRA=no\nLLDP=no\nEmitLLDP=no\nConfigureWithoutCarrier=no\n")
	if p.Mode == "dhcp" {
		b.WriteString("\n[DHCPv4]\nUseRoutes=no\nUseGateway=no\nUseDNS=no\nUseNTP=no\nUseHostname=no\nUseDomains=no\nSendHostname=no\n")
	}
	if p.Mode == "static" && p.Gateway != "" {
		pf, _ := netip.ParsePrefix(p.CIDR)
		gw, err := netip.ParseAddr(p.Gateway)
		if err != nil || !gw.Is4() || !pf.Masked().Contains(gw) {
			return "", fmt.Errorf("the gateway %q is not an address on the same subnet", p.Gateway)
		}
		fmt.Fprintf(&b, "\n[RoutingPolicyRule]\nFrom=%s/32\nTable=%d\nPriority=100\n", pf.Addr(), MgmtTable)
		fmt.Fprintf(&b, "\n[Route]\nDestination=%s\nScope=link\nTable=%d\n", pf.Masked(), MgmtTable)
		fmt.Fprintf(&b, "\n[Route]\nGateway=%s\nTable=%d\n", gw, MgmtTable)
	}
	b.WriteString("\n[Link]\nRequiredForOnline=no\n")
	return b.String(), nil
}

// MgmtApply writes the network file for the management port and has the network service read it,
// or removes it. Nothing is changed when the plan is refused.
func (s *Service) MgmtApply(p MgmtPlan) error {
	f := s.rootPath(mgmtNetFile)
	if p.Dev == "" {
		if _, err := os.Stat(f); err == nil {
			old, _ := os.ReadFile(f)
			os.Remove(f)
			sysRun(15*time.Second, "networkctl", "reload")
			if dev := mgmtDevOf(string(old)); dev != "" && reHADev.MatchString(dev) {
				sysRun(15*time.Second, "networkctl", "reconfigure", dev)
				sysRun(10*time.Second, "ip", "-4", "addr", "flush", "dev", dev)
			}
		}
		return nil
	}
	body, err := MgmtNetwork(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	old, _ := os.ReadFile(f)
	if string(old) == body {
		return nil
	}
	if err := writeFileAtomic(f, []byte(body), 0o644); err != nil {
		return err
	}
	sysRun(15*time.Second, "networkctl", "reload")
	sysRun(15*time.Second, "networkctl", "reconfigure", p.Dev)
	// A port that moved: the old one stops carrying the address.
	if od := mgmtDevOf(string(old)); od != "" && od != p.Dev && reHADev.MatchString(od) {
		sysRun(15*time.Second, "networkctl", "reconfigure", od)
		sysRun(10*time.Second, "ip", "-4", "addr", "flush", "dev", od)
	}
	return nil
}

func mgmtDevOf(file string) string {
	for _, ln := range strings.Split(file, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "Name="); ok {
			return v
		}
	}
	return ""
}

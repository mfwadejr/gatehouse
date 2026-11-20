package netd

import (
	"context"
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
		// The router the DHCP server names goes into the management table only (RouteTable), so it
		// can answer people on other subnets and can never become the route to the internet.
		fmt.Fprintf(&b, "\n[DHCPv4]\nRouteTable=%d\nUseRoutes=yes\nUseGateway=yes\nUseDNS=no\nUseNTP=no\nUseHostname=no\nUseDomains=no\nSendHostname=no\n", MgmtTable)
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
			mgmtClean()
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
	// Rules and routes that were made for the old settings go; the new ones are made from the new file.
	mgmtClean()
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

// ---- Answering people on other subnets when the address comes from DHCP ----
//
// With a fixed address the network file carries the policy rule (replies from the port's address use
// table 195). With DHCP the address is not known when the file is written, so a watcher makes the
// rule from the address the port really has, and keeps it right when the lease changes. The router
// from the lease is kept in table 195 by systemd-networkd (RouteTable above); the watcher also puts
// the route back if it is missing, so the answer never depends on one tool alone.

const mgmtRulePref = "100"

// mgmtClean removes the rules and routes this feature made. It is safe to call at any time.
func mgmtClean() {
	for _, r := range mgmtRules(mgmtRulesOut()) {
		sysRun(10*time.Second, "ip", "-4", "rule", "del", "pref", r.pref, "from", r.from, "lookup", fmt.Sprint(MgmtTable))
	}
	sysRun(10*time.Second, "ip", "-4", "route", "flush", "table", fmt.Sprint(MgmtTable))
}

func mgmtRulesOut() string {
	out, _ := sysRun(10*time.Second, "ip", "-4", "rule", "show")
	return out
}

type mgmtRule struct{ pref, from string }

// mgmtRules reads `ip rule show` and returns the rules that use the management table.
func mgmtRules(out string) []mgmtRule {
	var rs []mgmtRule
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 || !strings.HasSuffix(f[0], ":") || f[len(f)-2] != "lookup" || f[len(f)-1] != fmt.Sprint(MgmtTable) {
			continue
		}
		from := ""
		for i := 1; i+1 < len(f); i++ {
			if f[i] == "from" {
				from = strings.TrimSuffix(f[i+1], "/32") // `ip rule show` prints a single address without /32
			}
		}
		if from != "" {
			rs = append(rs, mgmtRule{pref: strings.TrimSuffix(f[0], ":"), from: from})
		}
	}
	return rs
}

// firstGlobalAddr returns the first IPv4 address and prefix in `ip -4 -o addr show` output.
func firstGlobalAddr(out string) (netip.Prefix, bool) {
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "inet" {
				if p, err := netip.ParsePrefix(f[i+1]); err == nil && p.Addr().Is4() {
					return p, true
				}
			}
		}
	}
	return netip.Prefix{}, false
}

// routerOf reads the router from a systemd-networkd lease file (ROUTER=a.b.c.d), or from the
// default route already in the management table ("default via a.b.c.d").
func routerOf(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if v, ok := strings.CutPrefix(ln, "ROUTER="); ok {
			if a, err := netip.ParseAddr(strings.Fields(v + " ")[0]); err == nil && a.Is4() {
				return a.String()
			}
		}
		f := strings.Fields(ln)
		if len(f) >= 3 && f[0] == "default" && f[1] == "via" {
			if a, err := netip.ParseAddr(f[2]); err == nil && a.Is4() {
				return a.String()
			}
		}
	}
	return ""
}

// mgmtSync makes the management table match the port's DHCP address and router. It does nothing for
// a fixed address, whose rule and routes come from the network file.
func (s *Service) mgmtSync() {
	file, err := os.ReadFile(s.rootPath(mgmtNetFile))
	if err != nil || !strings.Contains(string(file), "DHCP=ipv4") {
		return
	}
	dev := mgmtDevOf(string(file))
	if !reHADev.MatchString(dev) {
		return
	}
	addrOut, _ := sysRun(10*time.Second, "ip", "-4", "-o", "addr", "show", "dev", dev, "scope", "global")
	pf, ok := firstGlobalAddr(addrOut)
	have := mgmtRules(mgmtRulesOut())
	if !ok { // no address yet: nothing may point at an old one
		for _, r := range have {
			sysRun(10*time.Second, "ip", "-4", "rule", "del", "pref", r.pref, "from", r.from, "lookup", fmt.Sprint(MgmtTable))
		}
		return
	}
	want := pf.Addr().String()
	has := false
	for _, r := range have {
		if r.from == want {
			has = true
			continue
		}
		sysRun(10*time.Second, "ip", "-4", "rule", "del", "pref", r.pref, "from", r.from, "lookup", fmt.Sprint(MgmtTable))
	}
	if !has {
		sysRun(10*time.Second, "ip", "-4", "rule", "add", "pref", mgmtRulePref, "from", want+"/32", "lookup", fmt.Sprint(MgmtTable))
	}
	t := fmt.Sprint(MgmtTable)
	sysRun(10*time.Second, "ip", "-4", "route", "replace", pf.Masked().String(), "dev", dev, "scope", "link", "table", t)
	routes, _ := sysRun(10*time.Second, "ip", "-4", "route", "show", "table", t, "default")
	gw := routerOf(routes)
	if gw == "" {
		if idx, err := os.ReadFile(filepath.Join(s.sys(), "class", "net", dev, "ifindex")); err == nil {
			if lease, err := os.ReadFile(s.rootPath("run/systemd/netif/leases/" + strings.TrimSpace(string(idx)))); err == nil {
				gw = routerOf(string(lease))
			}
		}
	}
	if gw != "" {
		sysRun(10*time.Second, "ip", "-4", "route", "replace", "default", "via", gw, "dev", dev, "table", t)
	}
}

// mgmtWatch keeps the management table right while the port uses DHCP.
func (s *Service) mgmtWatch(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mgmtSync()
		}
	}
}

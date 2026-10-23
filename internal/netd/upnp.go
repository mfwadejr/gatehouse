package netd

import (
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"strings"
)

// UPnPMapping is one port opened on request by a device on the inside. The daemon decides whether to
// allow a request; this side re-checks everything, because it writes firewall rules as root.
type UPnPMapping struct {
	ID      int64  `json:"id"`
	Proto   string `json:"proto"` // tcp or udp
	ExtPort int    `json:"ext_port"`
	IntPort int    `json:"int_port"`
	IntAddr string `json:"int_addr"`
	WanDev  string `json:"wan_dev"`
}

var reDev = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

// UPnPScript builds the nftables script for a set of mappings. They live in their own table, so they
// never touch the main ruleset; the main ruleset lets their connections through when UPnP is on.
func UPnPScript(ms []UPnPMapping) (string, error) {
	var b strings.Builder
	b.WriteString("add table ip gatehouse_upnp\ndelete table ip gatehouse_upnp\n")
	if len(ms) == 0 {
		return b.String(), nil
	}
	if len(ms) > 512 {
		return "", errors.New("too many port mappings")
	}
	b.WriteString("table ip gatehouse_upnp {\n\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
	for _, m := range ms {
		if m.Proto != "tcp" && m.Proto != "udp" {
			return "", fmt.Errorf("mapping %d: protocol must be tcp or udp", m.ID)
		}
		if m.ExtPort < 1024 || m.ExtPort > 65535 || m.IntPort < 1 || m.IntPort > 65535 {
			return "", fmt.Errorf("mapping %d: port out of range", m.ID)
		}
		a, err := netip.ParseAddr(m.IntAddr)
		if err != nil || !a.Is4() || !a.IsPrivate() {
			return "", fmt.Errorf("mapping %d: the target must be a private IPv4 address", m.ID)
		}
		if !reDev.MatchString(m.WanDev) {
			return "", fmt.Errorf("mapping %d: bad interface", m.ID)
		}
		fmt.Fprintf(&b, "\t\tiifname \"%s\" %s dport %d counter dnat to %s:%d comment \"upnp:%d\"\n", m.WanDev, m.Proto, m.ExtPort, a, m.IntPort, m.ID)
	}
	b.WriteString("\t}\n}\n")
	return b.String(), nil
}

// UPnPApply replaces the whole set of mappings.
func (s *Service) UPnPApply(ms []UPnPMapping) error {
	script, err := UPnPScript(ms)
	if err != nil {
		return err
	}
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

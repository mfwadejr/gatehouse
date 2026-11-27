package threatcfg

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// Inline mode ("Block as it happens"). The firewall's own rules decide first. Packets they accept,
// between a checked inside port and an internet port, then pass through a second table whose only
// job is to hand them to the engine and wait for its answer. The queue is set to fail open: if the
// engine is not listening, or is so far behind that the queue is full, packets go through unchecked
// instead of being held or dropped. The box's own services (the console, SSH) never enter it.

const (
	// QueueNum is the queue the engine listens on.
	QueueNum = 100
	// IPSTable is the firewall table that holds the queue rules. It is separate from the main one so
	// a firewall change never touches it, and it is loaded and removed with the engine's service.
	IPSTable = "nopewarden_ips"
	// IPSFile is where the table is written; the engine's service loads it before it starts.
	IPSFile = "etc/nopewarden/ips.nft"
	// LocalRules holds the harmless test rule.
	LocalRules = "etc/nopewarden/local.rules"
	DropConf   = "etc/suricata/drop.conf"
	ModifyConf = "etc/suricata/modify.conf"
	// TestSID is the harmless test rule's number.
	TestSID = 9900001
	// TestPath is the web address path that trips the test rule.
	TestPath = "/nopewarden-inline-test"
)

// parseNet reads an address or a network.
func parseNet(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func quoteIfaces(l []string) string {
	q := make([]string, len(l))
	for i, s := range l {
		q[i] = `"` + s + `"`
	}
	sort.Strings(q)
	return "{ " + strings.Join(q, ", ") + " }"
}

// NftScript is the firewall table that queues the traffic to check.
func NftScript(p Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\ntable inet %s {\n", IPSTable, IPSTable, IPSTable)
	b.WriteString("\tchain forward {\n\t\ttype filter hook forward priority 10; policy accept;\n")
	var v4, v6 []string
	for _, a := range p.NeverCheck {
		if n, err := parseNet(a); err == nil {
			if n.Addr().Is4() {
				v4 = append(v4, n.String())
			} else {
				v6 = append(v6, n.String())
			}
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)
	if len(v4) > 0 {
		fmt.Fprintf(&b, "\t\tip saddr { %s } return\n\t\tip daddr { %s } return\n", strings.Join(v4, ", "), strings.Join(v4, ", "))
	}
	if len(v6) > 0 {
		fmt.Fprintf(&b, "\t\tip6 saddr { %s } return\n\t\tip6 daddr { %s } return\n", strings.Join(v6, ", "), strings.Join(v6, ", "))
	}
	in, wan := quoteIfaces(p.InlineIfaces), quoteIfaces(p.WANIfaces)
	fmt.Fprintf(&b, "\t\tiifname %s oifname %s queue flags bypass to %d\n", in, wan, QueueNum)
	fmt.Fprintf(&b, "\t\tiifname %s oifname %s queue flags bypass to %d\n", wan, in, QueueNum)
	b.WriteString("\t}\n}\n")
	return b.String()
}

func inlineUnit(p Plan) string {
	var pre strings.Builder
	seen := map[string]bool{}
	for _, i := range append(append([]string{}, p.InlineIfaces...), p.WANIfaces...) {
		if !seen[i] {
			seen[i] = true
			// The engine needs to see packets as they were on the wire, not glued together by the card.
			fmt.Fprintf(&pre, "ExecStartPre=-/usr/sbin/ethtool -K %s gro off lro off\n", i)
		}
	}
	return fmt.Sprintf(`[Unit]
Description=NopeWarden threat watching (Suricata, blocking as it happens)
After=network.target

[Service]
%sExecStartPre=-/usr/sbin/nft -f /etc/nopewarden/ips.nft
ExecStart=/usr/bin/suricata -c /etc/suricata/suricata.yaml --include /etc/nopewarden/suricata.yaml -q %d
ExecStopPost=-/usr/sbin/nft delete table inet %s
Restart=on-failure
RestartSec=5
Nice=10
CPUAccounting=yes
MemoryAccounting=yes
CPUQuota=%d%%
MemoryMax=%dM

[Install]
WantedBy=multi-user.target
`, pre.String(), QueueNum, IPSTable, p.CPUPercent, p.MemoryMB)
}

// IncludeFor is the engine's settings for this plan. In inline mode it adds the queue settings; the
// engine is told to let packets through when its queue is full.
func IncludeFor(p Plan) string {
	s := Include()
	if p.Inline {
		s += `nfq:
  mode: accept
  fail-open: yes
`
	}
	return s
}

// DropConfFor is the drop.conf the rule updater reads: groups set to Block have their rules turned
// from alert into drop. Empty unless inline mode is on.
func DropConfFor(p Plan) string {
	var b strings.Builder
	b.WriteString("# Written by NopeWarden\n")
	if p.Inline {
		g := append([]string{}, p.BlockGroups...)
		sort.Strings(g)
		for _, x := range g {
			b.WriteString("group:emerging-" + x + ".rules\n")
		}
		// The harmless test rule is a drop rule on its own.
	}
	return b.String()
}

// ModifyConfFor puts single rules back to alert only. The rule updater applies this after the drop
// conversion, so it wins over the group's setting.
func ModifyConfFor(p Plan) string {
	var b strings.Builder
	b.WriteString("# Written by NopeWarden\n")
	if p.Inline {
		s := append([]int{}, p.AlertOnlySIDs...)
		sort.Ints(s)
		for _, id := range s {
			fmt.Fprintf(&b, "%d \"^drop\" \"alert\"\n", id)
		}
	}
	return b.String()
}

// LocalRulesFor holds one harmless rule that drops a web request for a made-up address, so the
// person can prove that blocking works from any device on a checked network.
func LocalRulesFor(p Plan) string {
	if !p.Inline {
		return ""
	}
	return fmt.Sprintf(`# Written by NopeWarden. Harmless: it only matches a web address nobody uses.
drop http $HOME_NET any -> $EXTERNAL_NET any (msg:"NopeWarden inline test (harmless)"; flow:established,to_server; http.uri; content:"%s"; classtype:not-suspicious; sid:%d; rev:1;)
`, TestPath, TestSID)
}

package netd

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Smart queue management (the cure for "bufferbloat"). Each chosen internet connection is held
// just under its real speed, in both directions, so the queue lives here, where it is managed,
// instead of in the modem, where it is not. Upload is shaped on the port itself; download is
// steered through a helper device (ifb) and shaped there. CAKE is used when the kernel has it,
// otherwise fq_codel under a rate limit.

// SQMLink is one connection and the speeds to hold it to, in kilobits a second.
type SQMLink struct {
	Iface    string `json:"iface"`
	DownKbit int    `json:"down_kbit"`
	UpKbit   int    `json:"up_kbit"`
	// Prio asks for the queue to tell traffic classes apart (Give priority entries exist).
	Prio bool `json:"prio,omitempty"`
}

// SQMPlan is the whole set. Applying it replaces what was applied before.
type SQMPlan struct {
	Links []SQMLink `json:"links"`
	// Known are ports that may carry shaping from earlier plans, so a removed one is cleaned up.
	Known []string `json:"known"`
}

const sqmPrefix = "ghifb"

func sqmIfb(i int) string { return fmt.Sprintf("%s%d", sqmPrefix, i) }

func sqmClear(devs []string) [][]string {
	var cmds [][]string
	for _, d := range devs {
		cmds = append(cmds,
			[]string{"tc", "qdisc", "del", "dev", d, "root"},
			[]string{"tc", "qdisc", "del", "dev", d, "ingress"})
	}
	return cmds
}

// sqmShape returns the commands that shape one device with the discipline chosen.
func sqmShape(dev string, kbit int, disc string, ingress, prio bool) [][]string {
	rate := fmt.Sprintf("%dkbit", kbit)
	if disc == "cake" {
		classes := "besteffort"
		if prio {
			classes = "diffserv3"
		}
		args := []string{"tc", "qdisc", "replace", "dev", dev, "root", "cake", "bandwidth", rate, classes, "triple-isolate", "nat"}
		if ingress {
			args = append(args, "wash", "ingress")
		}
		return [][]string{args}
	}
	return [][]string{
		{"tc", "qdisc", "replace", "dev", dev, "root", "handle", "1:", "htb", "default", "10"},
		{"tc", "class", "replace", "dev", dev, "parent", "1:", "classid", "1:10", "htb", "rate", rate, "ceil", rate},
		{"tc", "qdisc", "replace", "dev", dev, "parent", "1:10", "fq_codel"},
	}
}

// SQMCommands returns the commands for a plan with one queue type, without running anything, so
// it can be tested anywhere. Clearing comes first. The types are "cake", "fq_codel", and for
// links that want priority "cake+priority" (both directions) and "cake+priority-up" (upload only,
// used when the kernel cannot restore the priority of download traffic).
func SQMCommands(p SQMPlan, disc string) ([][]string, error) {
	base, up, down := disc, false, false
	switch disc {
	case "cake+priority":
		base, up, down = "cake", true, true
	case "cake+priority-up":
		base, up = "cake", true
	case "cake", "fq_codel":
	default:
		return nil, fmt.Errorf("unknown queue type %q", disc)
	}
	disc = base
	seen := map[string]bool{}
	var devs []string
	add := func(d string) error {
		if !reHADev.MatchString(d) {
			return fmt.Errorf("%q is not a usable port name", d)
		}
		if !seen[d] {
			seen[d] = true
			devs = append(devs, d)
		}
		return nil
	}
	for _, d := range p.Known {
		if err := add(d); err != nil {
			return nil, err
		}
	}
	links := append([]SQMLink(nil), p.Links...)
	sort.Slice(links, func(i, j int) bool { return links[i].Iface < links[j].Iface })
	for _, l := range links {
		if err := add(l.Iface); err != nil {
			return nil, err
		}
		if l.DownKbit < 100 || l.UpKbit < 100 || l.DownKbit > 100000000 || l.UpKbit > 100000000 {
			return nil, fmt.Errorf("the speeds for %s are not sensible", l.Iface)
		}
	}
	sort.Strings(devs)
	cmds := sqmClear(devs)
	for i, l := range links {
		ifb := sqmIfb(i)
		pu, pd := l.Prio && up, l.Prio && down
		cmds = append(cmds, sqmShape(l.Iface, l.UpKbit, disc, false, pu)...)
		cmds = append(cmds,
			[]string{"ip", "link", "add", ifb, "type", "ifb"},
			[]string{"ip", "link", "set", ifb, "up"})
		cmds = append(cmds, sqmShape(ifb, l.DownKbit, disc, true, pd)...)
		cmds = append(cmds, []string{"tc", "qdisc", "replace", "dev", l.Iface, "handle", "ffff:", "ingress"})
		if pd {
			// Put back the priority the firewall noted on the connection, then hand over to the queue.
			cmds = append(cmds, []string{"tc", "filter", "replace", "dev", l.Iface, "parent", "ffff:", "protocol", "all", "matchall",
				"action", "ctinfo", "dscp", "0xfc000000", "0x01000000", "pipe", "action", "mirred", "egress", "redirect", "dev", ifb})
		} else {
			cmds = append(cmds, []string{"tc", "filter", "replace", "dev", l.Iface, "parent", "ffff:", "protocol", "all", "u32", "match", "u32", "0", "0",
				"action", "mirred", "egress", "redirect", "dev", ifb})
		}
	}
	return cmds, nil
}

// sqmRemoveIfbs deletes the helper devices an earlier plan made.
func sqmRemoveIfbs() {
	out, _ := sysRun(10*time.Second, "ip", "-o", "link", "show", "type", "ifb")
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		name := strings.TrimSuffix(strings.SplitN(f[1], "@", 2)[0], ":")
		if strings.HasPrefix(name, sqmPrefix) {
			sysRun(10*time.Second, "ip", "link", "del", name)
		}
	}
}

func sqmClearAll(p SQMPlan) {
	devs := append([]string{}, p.Known...)
	for _, l := range p.Links {
		devs = append(devs, l.Iface)
	}
	for _, c := range sqmClear(devs) {
		sysRun(10*time.Second, c[0], c[1:]...)
	}
	sqmRemoveIfbs()
}

// SQMApply puts the plan in place and reports which queue type it used ("cake", "cake+priority", "cake+priority-up" or "fq_codel").
// With no links it only removes earlier shaping. If anything fails, everything this feature added
// is taken away again, so the connection is left exactly as it was.
func (s *Service) SQMApply(p SQMPlan) (string, error) {
	if _, err := SQMCommands(p, "cake"); err != nil {
		return "", err
	}
	if len(p.Links) == 0 {
		sqmClearAll(p)
		return "", nil
	}
	var lastMsg string
	variants := []string{"cake", "fq_codel"}
	for _, l := range p.Links {
		if l.Prio {
			variants = []string{"cake+priority", "cake+priority-up", "cake", "fq_codel"}
			break
		}
	}
	for _, disc := range variants {
		sqmClearAll(p)
		cmds, _ := SQMCommands(p, disc)
		failed := false
		for _, c := range cmds {
			out, err := sysRun(15*time.Second, c[0], c[1:]...)
			if err == nil {
				continue
			}
			isDel := c[0] == "tc" && len(c) > 2 && (c[1] == "qdisc" && c[2] == "del")
			if isDel {
				continue
			}
			lastMsg = strings.TrimSpace(out)
			failed = true
			break
		}
		if !failed {
			return disc, nil
		}
		unknown := strings.Contains(lastMsg, "Unknown qdisc") || strings.Contains(lastMsg, "kind is unknown") || strings.Contains(lastMsg, "cake") ||
			strings.Contains(lastMsg, "ctinfo") || strings.Contains(lastMsg, "Unknown action") || strings.Contains(lastMsg, "diffserv3")
		if strings.HasPrefix(disc, "cake") && unknown {
			continue
		}
		break
	}
	sqmClearAll(p)
	if strings.Contains(lastMsg, "ifb") || strings.Contains(lastMsg, "Operation not supported") || strings.Contains(lastMsg, "RTNETLINK answers: Not supported") {
		lastMsg = "this system's kernel cannot shape incoming traffic"
	}
	return "", fmt.Errorf("could not set up the speed limit: %s", lastMsg)
}

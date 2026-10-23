package installer

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Port is a wired network port as the installer's own screen reports it.
type Port struct {
	Name  string
	Link  bool // a cable is connected
	USB   bool
	Speed string
}

// WiredPorts lists real Ethernet ports, USB adapters included.
func WiredPorts() []Port {
	var out []Port
	ents, _ := os.ReadDir("/sys/class/net")
	for _, e := range ents {
		n := e.Name()
		base := "/sys/class/net/" + n
		real, err := filepath.EvalSymlinks(base)
		if n == "lo" || err != nil || strings.Contains(real, "/devices/virtual/") {
			continue
		}
		if _, err := os.Stat(base + "/wireless"); err == nil {
			continue
		}
		if t, _ := os.ReadFile(base + "/type"); strings.TrimSpace(string(t)) != "1" {
			continue
		}
		carrier, _ := os.ReadFile(base + "/carrier")
		speed, _ := os.ReadFile(base + "/speed")
		sp := strings.TrimSpace(string(speed))
		if sp == "-1" {
			sp = ""
		}
		out = append(out, Port{Name: n, Link: strings.TrimSpace(string(carrier)) == "1", USB: strings.Contains(real, "/usb"), Speed: sp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Colours from the design tokens, loaded into the console palette.
const palette = "\x1b]P0121211\x1b]P1e5484d\x1b]P24cb06a\x1b]P3e5b43a\x1b]P43987e5\x1b]P7c3c2b7\x1b]P8898781\x1b]PFf0efec"

const (
	reset = "\x1b[0m"
	dim   = "\x1b[0;90m" // grey
	fg    = "\x1b[0;37m"
	bold  = "\x1b[1;97m"
	blue  = "\x1b[1;34m"
	green = "\x1b[0;32m"
	amber = "\x1b[0;33m"
	red   = "\x1b[0;31m"
)

// RenderScreen builds what the machine's own display shows while it waits to be installed.
// It is a pure function of the machine's state so it can be tested.
func RenderScreen(ports []Port, urls []string, code string) string {
	var b strings.Builder
	line := func(s string) { b.WriteString("    " + s + reset + "\n") }
	b.WriteString("\x1b[?25l\x1b[2J\x1b[H\n\n")
	line(blue + "GATEHOUSE")
	line(dim + "Installer")
	b.WriteString("\n\n")

	connected := 0
	for _, p := range ports {
		if p.Link {
			connected++
		}
	}
	switch {
	case len(ports) == 0:
		line(amber + "No network port found yet.")
		b.WriteString("\n")
		line(fg + "Gatehouse needs at least one wired network port. If you use a USB")
		line(fg + "network adapter, plug it in now. It is detected automatically.")
	case connected == 0:
		line(bold + "Connect your LAN cable to continue.")
		b.WriteString("\n")
		line(fg + "Plug the cable to your own network (a computer or switch) into any")
		line(fg + "port below. You finish setup from a computer on that network.")
		b.WriteString("\n")
		line(dim + "The internet cable (from your modem or provider) is optional.")
		line(dim + "Connect it now, or add it later from the console.")
		b.WriteString("\n")
		line(dim + "This screen updates by itself when a cable is detected.")
	case len(urls) == 0:
		line(bold + "Cable detected. Getting a network address...")
		b.WriteString("\n")
		line(dim + "This normally takes a few seconds.")
	default:
		line(bold + "Ready to install.")
		b.WriteString("\n")
		if len(urls) > 1 {
			line(fg + "On a computer connected to this network, open the address")
			line(fg + "for the port its cable is plugged into:")
		} else {
			line(fg + "On a computer connected to this network, open:")
		}
		b.WriteString("\n")
		for _, u := range urls {
			line(blue + "  " + u)
		}
		b.WriteString("\n")
		line(fg + "Installer code   " + bold + code)
		b.WriteString("\n")
		line(dim + "The internet cable is optional. You can connect it now or later.")
		line(dim + "Your browser will warn about the certificate. That is expected.")
		line(dim + "Nothing on this machine's disks changes until you confirm.")
	}

	if len(ports) > 0 {
		b.WriteString("\n\n")
		line(dim + "Network ports")
		for _, p := range ports {
			state, col := "no cable", dim
			if p.Link {
				state, col = "cable connected", green
				if p.Speed != "" {
					state += ", " + p.Speed + " Mb/s"
				}
			}
			name := p.Name
			if p.USB {
				name += " (USB)"
			}
			line(fmt.Sprintf("%s%-18s%s%s", fg, name, col, state))
		}
	}
	return b.String()
}

// usableAddresses are the web addresses a person can really use: one per port that has a
// cable in it, on a private network, so the address of an internet-facing port or a stray
// link-local address never shows up as a second, confusing choice. With a single address
// it is shown bare; with several, each says which port it belongs to.
func usableAddresses(port string) []string {
	wired := map[string]bool{}
	for _, p := range WiredPorts() {
		if p.Link {
			wired[p.Name] = true
		}
	}
	type entry struct{ port, url string }
	var found []entry
	seen := map[string]bool{}
	hasPort := map[string]bool{} // one address per port is enough; a second would only confuse
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		if !wired[ifi.Name] || ifi.Flags&net.FlagUp == 0 {
			continue
		}
		as, _ := ifi.Addrs()
		for _, a := range as {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || n.IP.IsLinkLocalUnicast() || !privateAddr(n.IP) {
				continue
			}
			u := "https://" + n.IP.String()
			if port != "" && port != "443" {
				u += ":" + port
			}
			if !seen[u] && !hasPort[ifi.Name] {
				seen[u], hasPort[ifi.Name] = true, true
				found = append(found, entry{ifi.Name, u})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].port < found[j].port })
	var out []string
	for _, e := range found {
		if len(found) > 1 {
			out = append(out, fmt.Sprintf("%-26s on port %s", e.url, e.port))
		} else {
			out = append(out, e.url)
		}
	}
	return out
}

// RunScreen keeps the machine's display current until the process ends.
func RunScreen(code, port string) {
	var last string
	first := true
	for {
		s := RenderScreen(WiredPorts(), usableAddresses(port), code)
		if s != last || first {
			for _, tty := range []string{"/dev/tty1", "/dev/console"} {
				if f, err := os.OpenFile(tty, os.O_WRONLY, 0); err == nil {
					if first {
						f.WriteString(palette)
					}
					f.WriteString(s)
					f.Close()
					break
				}
			}
			last, first = s, false
		}
		time.Sleep(time.Second)
	}
}

// RenderStatus is what an installed machine shows on its own display: the start-up splash
// gives way to this, so there is never a login prompt or boot text on screen.
func RenderStatus(host string, ports []Port, urls []string) string {
	var b strings.Builder
	line := func(s string) { b.WriteString("    " + s + reset + "\n") }
	b.WriteString("\x1b[?25l\x1b[2J\x1b[H\n\n")
	line(blue + "GATEHOUSE")
	if host != "" {
		line(dim + host)
	}
	b.WriteString("\n\n")
	if len(urls) == 0 {
		line(amber + "Waiting for a network connection.")
		b.WriteString("\n")
		line(fg + "Plug a network cable into a port below. This screen updates by itself.")
	} else {
		line(bold + "Ready.")
		b.WriteString("\n")
		if len(urls) > 1 {
			line(fg + "From a computer on your network, open the address for its port:")
		} else {
			line(fg + "From a computer on your network, open:")
		}
		b.WriteString("\n")
		for _, u := range urls {
			line(blue + "  " + u)
		}
		b.WriteString("\n")
		line(dim + "Your browser will warn about the certificate the first time. That is expected.")
	}
	if len(ports) > 0 {
		b.WriteString("\n\n")
		line(dim + "Network ports")
		for _, p := range ports {
			state, col := "no cable", dim
			if p.Link {
				state, col = "cable connected", green
				if p.Speed != "" {
					state += ", " + p.Speed + " Mb/s"
				}
			}
			name := p.Name
			if p.USB {
				name += " (USB)"
			}
			line(fmt.Sprintf("%s%-18s%s%s", fg, name, col, state))
		}
	}
	return b.String()
}

// RunStatus keeps an installed machine's display current. It never returns.
func RunStatus(port string) {
	var last string
	first := true
	for {
		host, _ := os.Hostname()
		s := RenderStatus(host, WiredPorts(), usableAddresses(port))
		if s != last || first {
			for _, tty := range []string{"/dev/tty1", "/dev/console"} {
				if f, err := os.OpenFile(tty, os.O_WRONLY, 0); err == nil {
					if first {
						f.WriteString(palette)
					}
					f.WriteString(s)
					f.Close()
					break
				}
			}
			last, first = s, false
		}
		time.Sleep(2 * time.Second)
	}
}

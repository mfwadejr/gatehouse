package installer

import (
	"fmt"
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
		line(fg + "On a computer connected to this network, open:")
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

// usableAddresses are the web addresses the installer can be reached on.
func usableAddresses(port string) []string {
	var out []string
	for _, u := range Addresses(port) {
		if strings.HasPrefix(u, "https://169.254.") || strings.HasPrefix(u, "https://127.") {
			continue
		}
		out = append(out, u)
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

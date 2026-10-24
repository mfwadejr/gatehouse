package installer

import (
	"fmt"
	"gatehouse/internal/gfx"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Port is a wired network port as the installer's own screen reports it.
type Port struct {
	Name  string
	MAC   string
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
		mac, _ := os.ReadFile(base + "/address")
		out = append(out, Port{Name: n, MAC: strings.TrimSpace(string(mac)), Link: strings.TrimSpace(string(carrier)) == "1", USB: strings.Contains(real, "/usb"), Speed: sp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// linkTried remembers when a port was last asked to come up, so a port with nothing plugged
// in is not poked every second.
var linkTried = map[string]time.Time{}

// EnsureLinksUp brings every wired port up. A port that is switched off cannot report a cable
// (its carrier file cannot be read), which is how USB adapters that appeared after boot went
// unnoticed until something else woke them. Ports are retried every few seconds because a USB
// adapter can take a while to finish starting.
func EnsureLinksUp() {
	for _, p := range WiredPorts() {
		state, _ := os.ReadFile("/sys/class/net/" + p.Name + "/operstate")
		if st := strings.TrimSpace(string(state)); st != "down" && st != "" {
			continue
		}
		if ifi, err := net.InterfaceByName(p.Name); err == nil && ifi.Flags&net.FlagUp != 0 && p.Link {
			continue
		}
		if t, ok := linkTried[p.Name]; ok && time.Since(t) < 8*time.Second {
			continue
		}
		linkTried[p.Name] = time.Now()
		bringUp(p.Name)
	}
}

// bringUp is replaced in tests.
var bringUp = func(name string) { exec.Command("ip", "link", "set", "dev", name, "up").Run() }

// RenderScreen is what the machine's own display shows while it waits for the browser-based
// installer. It is a pure function of the machine's state so it can be tested.
func RenderScreen(ports []Port, urls []string, code string, w, h int) string {
	s := NewScreen(w, h)
	s.header("Installer")
	connected := 0
	for _, p := range ports {
		if p.Link {
			connected++
		}
	}
	switch {
	case len(ports) == 0:
		s.mid(amber + "No network port found yet" + reset)
		s.blank()
		s.mid(fg + "Gatehouse needs at least one wired network port." + reset)
		s.mid(fg + "If you use a USB network adapter, plug it in now." + reset)
		s.mid(dim + "It is detected automatically." + reset)
	case connected == 0:
		s.mid(bold + "Connect your network cable to continue" + reset)
		s.blank()
		s.mid(fg + "Plug the cable from your own network (a computer or a switch)" + reset)
		s.mid(fg + "into any port. You finish setup from a computer on that network." + reset)
		s.blank()
		s.mid(dim + "The internet cable is optional. You can add it now or later." + reset)
	case len(urls) == 0:
		s.mid(bold + "Cable detected" + reset)
		s.blank()
		s.mid(fg + "Getting a network address…" + reset)
		s.mid(dim + "This normally takes a few seconds." + reset)
	default:
		s.mid(bold + "Ready to install" + reset)
		s.blank()
		if len(urls) > 1 {
			s.mid(fg + "On a computer on this network, open the address for its port" + reset)
		} else {
			s.mid(fg + "On a computer on this network, open" + reset)
		}
		var box []string
		for _, u := range urls {
			box = append(box, bold+u+reset)
		}
		box = append(box, "", dim+"Installer code  "+reset+bold+code+reset)
		s.midBox(box)
		s.blank()
		s.mid(dim + "Your browser will warn about the certificate. That is expected." + reset)
		s.mid(dim + "Nothing on this machine's disks changes until you confirm." + reset)
	}
	s.footer(portsLine(ports))
	return s.String()
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
	d := openGraphics()
	for {
		EnsureLinksUp()
		cw, ch := screenSize(d)
		s := RenderScreen(WiredPorts(), usableAddresses(port), code, cw, ch)
		if s != last || first {
			paint(d, s, first)
			last, first = s, false
		}
		time.Sleep(time.Second)
	}
}

// openGraphics takes over the display for graphics when it can. It returns nil on machines with no usable
// frame buffer, which then keep the plain console.
func openGraphics() *gfx.Display {
	d, err := gfx.OpenDisplay()
	if err != nil {
		log.Printf("screen: graphics unavailable (%v); using the text console", err)
		return nil
	}
	return d
}

// screenSize is the display's size in characters, graphical or not.
func screenSize(d *gfx.Display) (w, h int) {
	if d != nil {
		return d.Size()
	}
	return consoleSize()
}

// paint puts a finished screen on the display, or on the text console when there is no graphics.
func paint(d *gfx.Display, s string, first bool) {
	if d != nil {
		d.Write([]byte(s))
		return
	}
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
}

// RenderStatus is what an installed machine shows on its own display: the start-up splash
// gives way to this, so there is never a login prompt or boot text on screen. setup is
// "account", "wizard" or "ready" (see the daemon's setup-state file).
func RenderStatus(host string, ports []Port, urls []string, setup string, w, h int) string {
	s := NewScreen(w, h)
	sub := host
	if setup == "account" || setup == "wizard" {
		sub = "Setup not finished"
	}
	s.header(sub)
	open := func(lead string) {
		if len(urls) > 1 {
			lead = strings.Replace(lead, "open", "open the address for its port", 1)
		}
		s.mid(fg + lead + reset)
		var box []string
		for _, u := range urls {
			box = append(box, bold+u+reset)
		}
		s.midBox(box)
	}
	switch {
	case setup == "starting":
		s.mid(bold + "Starting" + reset)
		s.blank()
		s.mid(fg + "Gatehouse is starting. This takes up to a minute." + reset)
		s.mid(dim + "This screen updates by itself." + reset)
	case setup == "looping" && len(urls) > 0:
		s.mid(amber + "●" + reset + bold + " The console keeps stopping" + reset)
		s.blank()
		s.mid(fg + "Gatehouse restarts it by itself, but it keeps failing." + reset)
		s.mid(fg + "Your firewall is still protecting the network." + reset)
		s.mid(fg + "Press Alt+F2 on this keyboard, then choose 4, to see why." + reset)
	case setup == "down" && len(urls) > 0:
		s.mid(amber + "●" + reset + bold + " The console is not running" + reset)
		s.blank()
		s.mid(fg + "The network is up, but Gatehouse did not start." + reset)
		s.mid(fg + "Press Alt+F2 on this keyboard, then choose 4, to see why." + reset)
	case len(urls) == 0:
		s.mid(amber + "Waiting for a network connection" + reset)
		s.blank()
		s.mid(fg + "Plug a network cable into a port below." + reset)
		s.mid(dim + "This screen updates by itself." + reset)
	case setup == "account":
		s.mid(bold + "Welcome" + reset)
		s.blank()
		open("On a computer on your network, open")
		s.mid(fg + "and create your administrator account." + reset)
		s.blank()
		s.mid(dim + "Your browser will warn about the certificate. That is expected." + reset)
	case setup == "wizard":
		s.mid(bold + "Your account is ready" + reset)
		s.blank()
		open("To finish setting up, open")
		s.mid(fg + "and sign in." + reset)
	default:
		s.mid(green + "●" + reset + bold + " Gatehouse is running" + reset)
		s.blank()
		open("Manage it from a computer on your network: open")
		s.blank()
		s.mid(dim + "Your browser will warn about the certificate the first time. That is expected." + reset)
		s.mid(dim + "Locked out? Press Alt+F2 on this machine's keyboard." + reset)
	}
	s.footer(portsLine(ports))
	return s.String()
}

// RunStatus keeps an installed machine's display current. It never returns.
func RunStatus(port string) {
	var last string
	first := true
	d := openGraphics()
	fb := newFallback()
	for {
		host, _ := os.Hostname()
		EnsureLinksUp()
		fb.update(readSetupState(), WiredPorts(), portHasAddress)
		cw, ch := screenSize(d)
		port := ReadConsolePort(port)
		s := RenderStatus(host, WiredPorts(), usableAddresses(port), consoleState(port, readSetupState()), cw, ch)
		if s != last || first {
			paint(d, s, first)
			last, first = s, false
		}
		time.Sleep(2 * time.Second)
	}
}

// consoleState replaces the setup state with "starting" or "down" when the web console is not
// answering, so the screen never invites someone to open an address that is dead.
func consoleState(port, setup string) string {
	if port == "" {
		port = "443"
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); err == nil {
		c.Close()
		return setup
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		var up float64
		fmt.Sscanf(string(b), "%f", &up)
		if up < 90 {
			return "starting"
		}
	}
	if restarts() >= 3 {
		return "looping"
	}
	return "down"
}

// restarts is how many times systemd has restarted the console by itself.
func restarts() int {
	out, err := exec.Command("systemctl", "show", "gatehouse", "-p", "NRestarts", "--value").Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// ConsolePortFile is the copy of the console's port that the console keeps for these screens.
var ConsolePortFile = "/var/lib/gatehouse/console.port"

// ReadConsolePort is the port the console is set to answer on, or def when none is saved.
func ReadConsolePort(def string) string {
	if b, err := os.ReadFile(ConsolePortFile); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n >= 1 && n <= 65535 {
			return strconv.Itoa(n)
		}
	}
	if def == "" {
		return "443"
	}
	return def
}

// consoleSize is the size of the machine's own display in characters.
func consoleSize() (w, h int) {
	f, err := os.Open("/dev/tty1")
	if err != nil {
		return 80, 24
	}
	defer f.Close()
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

// readSetupState reads how far first-time setup has got, as written by the daemon.
func readSetupState() string {
	b, err := os.ReadFile("/var/lib/gatehouse/setup-state")
	if err != nil {
		return "ready"
	}
	return strings.TrimSpace(string(b))
}

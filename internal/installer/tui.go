package installer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/seed"
)

// The text-mode installer asks the same questions as the web installer, one at a time, on the
// machine's own screen or a serial console. It needs no network and no setup code: whoever is
// at the keyboard is at the machine.

var (
	errClosed  = errors.New("input closed")
	errRestart = errors.New("start over")
)

// Term is a line-oriented terminal. It speaks plain ANSI and never positions the cursor,
// so it works on a monitor, a serial terminal, or a terminal emulator alike.
type Term struct {
	r   *bufio.Reader
	w   io.Writer
	cur [3]any // the heading on screen: step, of, title
}

func NewTerm(r io.Reader, w io.Writer) *Term { return &Term{r: bufio.NewReader(r), w: w} }

func (t *Term) p(f string, a ...any) { fmt.Fprintf(t.w, strings.ReplaceAll(f, "\n", "\r\n"), a...) }

func (t *Term) clear() { io.WriteString(t.w, "\x1b[0m\x1b[2J\x1b[H") }

// readLine reads until Enter. Backspace edits, Ctrl-U clears, Ctrl-C starts over, and arrow
// keys and other escape sequences are ignored. With echo off each character shows as a star.
func (t *Term) readLine(echo bool) (string, error) {
	var line []rune
	for {
		b, err := t.r.ReadByte()
		if err != nil {
			return "", errClosed
		}
		switch {
		case b == '\r' || b == '\n':
			if b == '\r' && t.r.Buffered() > 0 {
				if n, _ := t.r.Peek(1); len(n) == 1 && n[0] == '\n' {
					t.r.ReadByte()
				}
			}
			io.WriteString(t.w, "\r\n")
			return string(line), nil
		case b == 0x03:
			io.WriteString(t.w, "\r\n")
			return "", errRestart
		case b == 0x04 && len(line) == 0:
			return "", errClosed
		case b == 0x7f || b == 0x08:
			if len(line) > 0 {
				line = line[:len(line)-1]
				io.WriteString(t.w, "\b \b")
			}
		case b == 0x15:
			for range line {
				io.WriteString(t.w, "\b \b")
			}
			line = line[:0]
		case b == 0x1b:
			c, err := t.r.ReadByte()
			if err != nil {
				return "", errClosed
			}
			if c == '[' || c == 'O' {
				for {
					d, err := t.r.ReadByte()
					if err != nil {
						return "", errClosed
					}
					if d >= 0x40 && d <= 0x7e {
						break
					}
				}
			}
		case b >= 0x20 && b != 0x7f:
			line = append(line, rune(b))
			if echo {
				io.WriteString(t.w, string(rune(b)))
			} else {
				io.WriteString(t.w, "*")
			}
		}
	}
}

// ask shows a prompt with its default in brackets and repeats until the answer is accepted.
func (t *Term) ask(prompt, def string, check func(string) error) (string, error) {
	for {
		if def != "" {
			t.p("  %s [%s]: ", prompt, def)
		} else {
			t.p("  %s: ", prompt)
		}
		s, err := t.readLine(true)
		if err != nil {
			return "", err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			s = def
		}
		if check != nil {
			if err := check(s); err != nil {
				t.p("  %s%s%s\n", red, err.Error(), reset)
				continue
			}
		}
		return s, nil
	}
}

func (t *Term) yesno(prompt string, def bool) (bool, error) {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	for {
		t.p("  %s [%s]: ", prompt, d)
		s, err := t.readLine(true)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		t.p("  Please answer y or n.\n")
	}
}

// choose lists options by number. A zero default means there is none.
func (t *Term) choose(title string, opts []string, def int) (int, error) {
	t.p("\n  %s%s%s\n\n", bold, title, reset)
	for i, o := range opts {
		t.p("    %d) %s\n", i+1, o)
	}
	t.p("\n")
	d := ""
	if def > 0 {
		d = strconv.Itoa(def)
	}
	for {
		s, err := t.ask("Number", d, nil)
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(s)
		if err == nil && n >= 1 && n <= len(opts) {
			return n - 1, nil
		}
		t.p("  %sEnter a number from 1 to %d.%s\n", red, len(opts), reset)
	}
}

func (t *Term) password(prompt string) (string, error) {
	t.p("  %s: ", prompt)
	return t.readLine(false)
}

// ---------------------------------------------------------------- the wizard

// TextEnv is what the wizard looks at. Tests replace the pieces.
type TextEnv struct {
	In      *Installer
	Ports   func() []Port
	Used    func() []netip.Prefix // networks this machine is already attached to
	Zones   func() []string
	Refresh func() // bring ports up so cables are noticed
	Poll    time.Duration
	Settle  time.Duration // how long to wait for a port to come up before listing
	Serial  string        // description of the serial console, shown on the first screen
}

func NewTextEnv(in *Installer, serial string) *TextEnv {
	return &TextEnv{In: in, Ports: WiredPorts, Used: usedPrefixes, Zones: timezones, Refresh: EnsureLinksUp, Poll: 500 * time.Millisecond, Settle: time.Second, Serial: serial}
}

// again redraws the current heading on a clean screen, so a long step does not scroll its
// first questions off a 24-line terminal.
func (t *Term) again(env *TextEnv) {
	t.banner(env, t.cur[0].(int), t.cur[1].(int), t.cur[2].(string))
}

func (t *Term) banner(env *TextEnv, step, of int, title string) {
	t.cur = [3]any{step, of, title}
	t.clear()
	t.p("\n  %sGATEHOUSE%s  %sInstaller · step %d of %d%s\n", blue, reset, dim, step, of, reset)
	t.p("  %s%s%s\n", bold, title, reset)
	t.p("  %s%s%s\n", dim, strings.Repeat("-", 60), reset)
}

const steps = 6

// RunText walks through the questions and installs. It returns when the machine is about to
// reboot, when the input closes, or after a failed install the person gives up on.
func RunText(t *Term, env *TextEnv) error {
	for {
		err := textOnce(t, env)
		if err == errRestart {
			t.p("\n  Starting over.\n")
			time.Sleep(300 * time.Millisecond)
			continue
		}
		return err
	}
}

func textOnce(t *Term, env *TextEnv) error {
	// Welcome
	t.clear()
	t.p("\n  %sGATEHOUSE%s  %sInstaller (text mode)%s\n", blue, reset, dim, reset)
	t.p("  %s%s%s\n\n", dim, strings.Repeat("-", 60), reset)
	t.p("  This installs Gatehouse on this machine and erases the disk you pick.\n")
	t.p("  Nothing is changed until you confirm at the end.\n\n")
	t.p("  Keys:   Enter accepts the suggestion in [brackets]\n")
	t.p("          Backspace edits      Ctrl-C starts over\n\n")
	if env.Serial != "" {
		t.p("  %sSerial console%s  %s\n", dim, reset, env.Serial)
		t.p("  %sThis screen and the serial console both work at the same time.%s\n\n", dim, reset)
	}
	t.p("  Press Enter to begin. ")
	if _, err := t.readLine(true); err != nil {
		return err
	}

	// 1. Disk
	var disk Disk
	for {
		t.banner(env, 1, steps, "Where should Gatehouse be installed?")
		disks, err := env.In.Disks()
		if err != nil {
			t.p("\n  %s%v%s\n", red, err, reset)
			return err
		}
		var opts []string
		var idx []int
		for i, d := range disks {
			if d.InUse {
				continue
			}
			label := fmt.Sprintf("%-14s %s  %s", d.Path, humanSize(d.Size), strings.TrimSpace(d.Model))
			if d.Parts > 0 {
				label += fmt.Sprintf("  (%d partitions, will be erased)", d.Parts)
			}
			opts = append(opts, label)
			idx = append(idx, i)
		}
		if len(opts) == 0 {
			t.p("\n  %sNo disk to install on was found.%s\n", amber, reset)
			t.p("  The disk must be at least 4 GB, and not the USB stick you started from.\n\n")
			t.p("  Press Enter to look again. ")
			if _, err := t.readLine(true); err != nil {
				return err
			}
			continue
		}
		def := 1
		n, err := t.choose("Everything on the disk you choose will be erased.", opts, def)
		if err != nil {
			return err
		}
		disk = disks[idx[n]]
		break
	}

	// 2. Network
	t.banner(env, 2, steps, "Your network")
	net, err := askNetwork(t, env)
	if err != nil {
		return err
	}

	// 3. Name and time zone
	t.banner(env, 3, steps, "This device")
	host, err := t.ask("Device name", "gatehouse", func(s string) error {
		if !reHost.MatchString(s) {
			return errors.New("use lowercase letters, digits and dashes")
		}
		return nil
	})
	if err != nil {
		return err
	}
	zone, err := askZone(t, env)
	if err != nil {
		return err
	}

	// 4. Administrator
	t.banner(env, 4, steps, "Administrator account")
	t.p("  You sign in to the console with this account.\n\n")
	user, err := t.ask("Administrator name", "admin", func(s string) error {
		if !reUser.MatchString(s) {
			return errors.New("start with a letter; use lowercase letters, digits, dots, dashes or underscores")
		}
		return nil
	})
	if err != nil {
		return err
	}
	var pass string
	for {
		p1, err := t.password("Password (at least 12 characters)")
		if err != nil {
			return err
		}
		if len(p1) < 12 {
			t.p("  %sToo short: %d of the 12 characters needed. Add %d more. Several unrelated\n  words in a row is easy to remember and long enough.%s\n", red, len([]rune(p1)), 12-len([]rune(p1)), reset)
			continue
		}
		p2, err := t.password("Type it again")
		if err != nil {
			return err
		}
		if p1 != p2 {
			t.p("  %sThe two passwords differ. Try again.%s\n", red, reset)
			continue
		}
		pass = p1
		break
	}
	mfa, err := t.yesno("Require two-step sign-in for every account", false)
	if err != nil {
		return err
	}

	// 5. Review
	req := Request{Disk: disk.Path, Hostname: host, Username: user, Password: pass, Timezone: zone, RequireMFA: mfa, Network: *net}
	for {
		t.banner(env, 5, steps, "Review")
		t.p("  Disk            %s%s %s%s  %s(will be erased)%s\n", bold, disk.Path, humanSize(disk.Size), reset, red, reset)
		for _, l := range describeNetwork(net) {
			t.p("  %s\n", l)
		}
		t.p("  Device name     %s\n", host)
		t.p("  Time zone       %s\n", zone)
		t.p("  Administrator   %s%s\n", user, map[bool]string{true: " (two-step sign-in required)", false: ""}[mfa])
		t.p("\n  To go ahead, type the disk name (%s%s%s) exactly. Anything else cancels.\n\n", bold, disk.Path, reset)
		c, err := t.ask("Disk name", "", nil)
		if err != nil {
			return err
		}
		if c != disk.Path {
			return errRestart
		}
		req.Confirm = c
		if err := env.In.Start(req); err != nil {
			t.p("\n  %s%v%s\n\n", red, err, reset)
			t.p("  Press Enter to start over. ")
			if _, err := t.readLine(true); err != nil {
				return err
			}
			return errRestart
		}
		break
	}

	// 6. Install
	for {
		st := env.In.Status()
		t.banner(env, 6, steps, "Installing")
		t.p("  This takes a few minutes. Do not switch the machine off.\n\n")
		for _, s := range st.Steps {
			mark, col := "[ ]", dim
			switch s.State {
			case "running":
				mark, col = "[>]", bold
			case "done":
				mark, col = "[x]", green
			case "error":
				mark, col = "[!]", red
			}
			t.p("  %s%s %s%s\n", col, mark, s.Name, reset)
		}
		switch st.State {
		case "done":
			t.p("\n  %sInstalled.%s\n\n", green, reset)
			t.p("  Press Enter to restart. Take the USB stick out as soon as the screen\n")
			t.p("  goes blank, or the machine will start the installer again.\n\n")
			t.p("  After it starts (about a minute), the machine's screen shows the\n")
			t.p("  address to open in a browser.\n\n")
			if _, err := t.readLine(true); err != nil {
				return err
			}
			return env.In.Reboot()
		case "error":
			t.p("\n  %s%s%s\n", red, st.Error, reset)
			if lines := lastLines(st.Log, 8); lines != "" {
				t.p("\n%s\n", indent(lines))
			}
			t.p("\n  Nothing more was changed. Press Enter to start over. ")
			if _, err := t.readLine(true); err != nil {
				return err
			}
			return errRestart
		}
		time.Sleep(env.Poll)
	}
}

func askZone(t *Term, env *TextEnv) (string, error) {
	zones := env.Zones()
	have := map[string]bool{}
	for _, z := range zones {
		have[z] = true
	}
	for {
		t.p("\n  Time zone: type a name like Europe/London or America/Chicago, or part of\n  one to search. UTC is fine if you are not sure.\n\n")
		s, err := t.ask("Time zone", "UTC", nil)
		if err != nil {
			return "", err
		}
		if have[s] {
			return s, nil
		}
		var hits []string
		for _, z := range zones {
			if strings.Contains(strings.ToLower(z), strings.ToLower(s)) {
				hits = append(hits, z)
			}
		}
		sort.Strings(hits)
		switch {
		case len(hits) == 0:
			t.p("  %sNothing matches %q.%s\n", red, s, reset)
		case len(hits) > 12:
			t.p("  %d matches. Type more of the name to narrow it down.\n", len(hits))
		default:
			n, err := t.choose("Matches", append(hits, "None of these, search again"), 1)
			if err != nil {
				return "", err
			}
			if n < len(hits) {
				return hits[n], nil
			}
		}
	}
}

func humanSize(b int64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.1f TB", float64(b)/(1<<40))
	default:
		return fmt.Sprintf("%d GB", b>>30)
	}
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}

func indent(s string) string {
	ls := strings.Split(s, "\n")
	for i := range ls {
		if len(ls[i]) > 70 {
			ls[i] = ls[i][:70]
		}
		ls[i] = "    " + dim + ls[i] + reset
	}
	return strings.Join(ls, "\r\n")
}

func portLabel(p Port) string {
	s := p.Name
	if p.USB {
		s += " (USB)"
	}
	state := "no cable"
	if p.Link {
		state = "cable connected"
		if p.Speed != "" {
			state += ", " + p.Speed + " Mb/s"
		}
	}
	return fmt.Sprintf("%-16s %s", s, state)
}

// askNetwork gathers the same choices as the web installer's network step, with defaults for
// everything that has a sensible one.
func askNetwork(t *Term, env *TextEnv) (*seed.Network, error) {
	env.Refresh()
	time.Sleep(env.Settle)
	ports := env.Ports()
	for len(ports) == 0 {
		t.p("\n  %sNo network port found.%s\n  Gatehouse needs at least one wired port. If you use a USB network adapter,\n  plug it in, then press Enter to look again. ", amber, reset)
		if _, err := t.readLine(true); err != nil {
			return nil, err
		}
		env.Refresh()
		time.Sleep(env.Settle)
		ports = env.Ports()
	}
	// Local network port
	t.p("\n  Plug a cable from your own network (a computer or switch) into a port now,\n  and it shows as connected below. Press Enter on a blank line to refresh.\n")
	var lan Port
	for {
		var opts []string
		def := 1
		for i, p := range ports {
			opts = append(opts, portLabel(p))
			if p.Link && def == 1 {
				def = i + 1
			}
		}
		opts = append(opts, "Refresh the list")
		n, err := t.choose("Which port is for your own network?", opts, def)
		if err != nil {
			return nil, err
		}
		if n == len(ports) {
			env.Refresh()
			time.Sleep(env.Settle)
			ports = env.Ports()
			continue
		}
		lan = ports[n]
		break
	}
	net := &seed.Network{LAN: seed.LAN{Port: lan.Name}}

	// Internet port (optional)
	var others []Port
	for _, p := range ports {
		if p.Name != lan.Name {
			others = append(others, p)
		}
	}
	var wan *Port
	if len(others) > 0 {
		t.again(env)
		opts := []string{"None. Another router provides the internet, or I will add it later"}
		for _, p := range others {
			opts = append(opts, portLabel(p))
		}
		n, err := t.choose("Which port goes to the internet (your modem or provider)?", opts, 1)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			wan = &others[n-1]
		}
	}
	if wan != nil {
		w := &seed.WAN{Port: wan.Name}
		n, err := t.choose("How does your provider connect you?", []string{
			"Automatic (most home and office connections)",
			"Username and password (PPPoE)",
			"Fixed address",
		}, 1)
		if err != nil {
			return nil, err
		}
		switch n {
		case 0:
			w.Type = "dhcp"
		case 1:
			w.Type = "pppoe"
			t.p("\n")
			if w.PPPoEUser, err = t.ask("Provider user name", "", nonEmpty("enter the user name from your provider")); err != nil {
				return nil, err
			}
			if w.PPPoEPass, err = t.password("Provider password"); err != nil {
				return nil, err
			}
		case 2:
			w.Type = "static"
			t.p("\n")
			if w.Addr, err = t.ask("Address with size (like 203.0.113.10/29)", "", prefixCheck); err != nil {
				return nil, err
			}
			pf, _ := netip.ParsePrefix(w.Addr)
			if w.Gateway, err = t.ask("Gateway", guessGateway(pf), addrCheck); err != nil {
				return nil, err
			}
			if w.DNS, err = t.ask("DNS servers (optional, comma separated)", "1.1.1.1, 9.9.9.9", nil); err != nil {
				return nil, err
			}
		}
		net.WAN = w
	}

	// How Gatehouse is used on the LAN
	t.again(env)
	if net.WAN != nil {
		net.LAN.Mode = "router"
	} else {
		n, err := t.choose("How will Gatehouse be used?", []string{
			"As the main router: it hands out addresses to your devices",
			"Behind another router: it gets its address from that router",
		}, 1)
		if err != nil {
			return nil, err
		}
		net.LAN.Mode = []string{"router", "behind"}[n]
	}
	if net.LAN.Mode == "router" {
		def := seed.Suggest(env.Used())
		t.p("\n  Gatehouse's own address on your network. The suggestion avoids any\n  network this machine is already connected to.\n\n")
		a, err := t.ask("Address with size", def, func(s string) error {
			x := seed.Network{LAN: seed.LAN{Port: "x", Mode: "router", Address: s}}
			return x.Validate()
		})
		if err != nil {
			return nil, err
		}
		net.LAN.Address = a
	} else {
		n, err := t.choose("What address should this device have?", []string{
			"Automatic, from your router",
			"A fixed address",
		}, 1)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			net.LAN.Addressing = "dhcp"
		} else {
			net.LAN.Addressing = "static"
			t.p("\n")
			if net.LAN.Static, err = t.ask("Address with size (like 192.168.0.50/24)", "", prefixCheck); err != nil {
				return nil, err
			}
			pf, _ := netip.ParsePrefix(net.LAN.Static)
			if net.LAN.Gateway, err = t.ask("Your router's address", guessGateway(pf), addrCheck); err != nil {
				return nil, err
			}
			if net.LAN.DNS, err = t.ask("DNS server (optional)", net.LAN.Gateway, nil); err != nil {
				return nil, err
			}
		}
	}

	// Remaining ports join the LAN as a switch.
	var spare []string
	for _, p := range ports {
		if p.Name != lan.Name && (wan == nil || p.Name != wan.Name) {
			spare = append(spare, p.Name)
		}
	}
	if len(spare) > 0 {
		t.p("\n  Unused ports: %s\n", strings.Join(spare, ", "))
		yes, err := t.yesno("Use them as extra sockets on your own network (like a switch)", false)
		if err != nil {
			return nil, err
		}
		if yes {
			net.Bridge = spare
		}
	}
	if err := net.Validate(); err != nil {
		t.p("\n  %s%v%s\n", red, err, reset)
		return nil, errRestart
	}
	return net, nil
}

func describeNetwork(n *seed.Network) []string {
	out := []string{fmt.Sprintf("Local network   port %s", n.LAN.Port)}
	if n.LAN.Mode == "router" {
		out[0] += ", Gatehouse is the router at " + n.LAN.Address
	} else if n.LAN.Addressing == "dhcp" {
		out[0] += ", behind another router, automatic address"
	} else {
		out[0] += ", behind " + n.LAN.Gateway + ", address " + n.LAN.Static
	}
	if n.WAN != nil {
		how := map[string]string{"dhcp": "automatic", "pppoe": "user name " + n.WAN.PPPoEUser, "static": n.WAN.Addr}[n.WAN.Type]
		out = append(out, fmt.Sprintf("Internet        port %s, %s", n.WAN.Port, how))
	} else {
		out = append(out, "Internet        not set up now (add it later in the console)")
	}
	if len(n.Bridge) > 0 {
		out = append(out, "Extra sockets   "+strings.Join(n.Bridge, ", "))
	}
	return out
}

func nonEmpty(msg string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(msg)
		}
		return nil
	}
}

func prefixCheck(s string) error {
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return errors.New("type the address and size, like 192.168.0.50/24")
	}
	return nil
}

func addrCheck(s string) error {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return errors.New("type an address like 192.168.0.1")
	}
	return nil
}

// guessGateway offers the first address of the network, which is where most routers live.
func guessGateway(p netip.Prefix) string {
	if !p.IsValid() {
		return ""
	}
	g := p.Masked().Addr().Next()
	if g == p.Addr() {
		g = g.Next()
	}
	return g.String()
}

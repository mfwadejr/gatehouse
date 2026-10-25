package installer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
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
	r    *bufio.Reader
	w    io.Writer
	cur  [3]any // the heading on screen: step, of, title
	W, H int    // screen size in characters
	M    string // left margin that centres the prompts under a themed heading
}

func NewTerm(r io.Reader, w io.Writer) *Term { return &Term{r: bufio.NewReader(r), w: w, W: 80, H: 24} }

// show draws a finished screen and leaves the cursor visible for the next prompt.
func (t *Term) show(s *Screen) { io.WriteString(t.w, s.String()+"\x1b[?25h") }

func (t *Term) p(f string, a ...any) {
	fmt.Fprintf(t.w, strings.ReplaceAll(f, "\n", "\r\n"+t.M), a...)
}

func (t *Term) clear() { io.WriteString(t.w, asciiReset+"\x1b[0m\x1b[2J\x1b[H") }

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
	In          *Installer
	Ports       func() []Port
	Refresh     func() // bring ports up so cables are noticed
	Poll        time.Duration
	Settle      time.Duration // how long to wait for a port to come up before listing
	Serial      string        // description of the serial console, shown on the first screen
	RestartWait time.Duration // how long to wait for the restart; tests shorten it
}

// restartWait is how long the screen waits for the machine to restart before it says it could not.
func (e *TextEnv) restartWait() time.Duration {
	if e.RestartWait > 0 {
		return e.RestartWait
	}
	return 30 * time.Second
}

func NewTextEnv(in *Installer, serial string) *TextEnv {
	return &TextEnv{In: in, Ports: WiredPorts, Refresh: EnsureLinksUp, Poll: 500 * time.Millisecond, Settle: time.Second, Serial: serial}
}

// portLine describes a port for the list.
func portLine(p Port) string {
	state := "no cable"
	mark := dim + "○" + reset
	if p.Link {
		mark = green + "●" + reset
		state = "cable connected"
		if p.Speed != "" {
			state += ", " + p.Speed + " Mb/s"
		}
	}
	kind := ""
	if p.USB {
		kind = "  (USB adapter)"
	}
	return fmt.Sprintf("%s %-16s %s%s", mark, p.Name, state, kind)
}

func portsSignature(ps []Port) string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "%s:%v;", p.Name, p.Link)
	}
	return b.String()
}

// pickMgmtPort asks which network port Gatehouse will be managed from. The list updates by itself
// as a cable is plugged in or an adapter appears, so the person can plug a cable in and watch it
// show up. A port with a cable is required, unless the person insists on one without.
func (t *Term) pickMgmtPort(env *TextEnv) (Port, error) {
	type lineRes struct {
		s   string
		err error
	}
	lines := make(chan lineRes, 1)
	reading := false
	read := func() {
		if reading {
			return
		}
		reading = true
		go func() { s, err := t.readLine(true); lines <- lineRes{s, err} }()
	}
	env.Refresh()
	time.Sleep(env.Settle)
	var ports []Port
	sig := "\x00"
	def := 0
	draw := func() {
		t.banner(env, 2, steps, "Which network port will you manage Gatehouse from?")
		t.p("  Plug a network cable into the port you want to use. After the restart you\n")
		t.p("  finish setup from a computer connected to it. The list updates by itself.\n\n")
		def = 0
		withLink := 0
		for i, p := range ports {
			t.p("    %d) %s\n", i+1, portLine(p))
			if p.Link {
				withLink++
				def = i + 1
			}
		}
		if withLink != 1 {
			def = 0
		}
		switch {
		case len(ports) == 0:
			t.p("    %sNo network port found yet. If you use a USB network adapter, plug it in now.%s\n", amber, reset)
		case withLink == 0:
			t.p("\n  %sNo cable is connected yet.%s\n", amber, reset)
		}
		t.p("\n")
		if def > 0 {
			t.p("  Number [%d]: ", def)
		} else {
			t.p("  Number: ")
		}
	}
	tick := time.NewTicker(env.Poll)
	defer tick.Stop()
	for {
		env.Refresh()
		ports = env.Ports()
		if s := portsSignature(ports); s != sig {
			sig = s
			draw()
		}
		read()
		select {
		case <-tick.C:
			continue
		case r := <-lines:
			reading = false
			if r.err != nil {
				return Port{}, r.err
			}
			ans := strings.TrimSpace(r.s)
			n := def
			if ans != "" {
				v, err := strconv.Atoi(ans)
				if err != nil || v < 1 || v > len(ports) {
					t.p("  %sEnter one of the numbers above.%s\n", red, reset)
					t.p("  Number: ")
					continue
				}
				n = v
			}
			if n == 0 {
				t.p("  %sPick a port from the list. Plug a cable in first if none shows a cable.%s\n  Number: ", amber, reset)
				continue
			}
			p := ports[n-1]
			if !p.Link {
				ok, err := t.yesno("No cable is plugged into "+p.Name+". Use it anyway?", false)
				if err != nil {
					return Port{}, err
				}
				if !ok {
					sig = "\x00"
					continue
				}
			}
			return p, nil
		}
	}
}

// again redraws the current heading on a clean screen, so a long step does not scroll its
// first questions off a 24-line terminal.
func (t *Term) again(env *TextEnv) {
	t.banner(env, t.cur[0].(int), t.cur[1].(int), t.cur[2].(string))
}

func (t *Term) banner(env *TextEnv, step, of int, title string) {
	t.cur = [3]any{step, of, title}
	t.heading(fmtf("Installer · step %d of %d", step, of), title)
}

// heading clears the screen and draws the themed heading; prompts that follow are centred under it.
func (t *Term) heading(sub, title string) {
	sc := NewScreen(t.W, t.H)
	sc.header(sub)
	sc.mid(bold + title + reset)
	sc.blank()
	t.M = strings.Repeat(" ", max(2, (t.W-58)/2))
	t.show(sc)
	io.WriteString(t.w, t.M)
}

const steps = 3

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
	w := NewScreen(t.W, t.H)
	w.header("Installer")
	w.mid(bold + "Install Gatehouse on this machine" + reset)
	w.blank()
	w.mid(fg + "This erases the disk you choose. Nothing changes until you confirm." + reset)
	w.blank()
	w.mid(dim + "Enter accepts the suggestion in [brackets]   ·   Ctrl-C starts over" + reset)
	if env.Serial != "" {
		w.blank()
		w.mid(dim + "Serial console: " + env.Serial + reset)
	}
	w.blank()
	w.mid(bold + "Press Enter to begin" + reset)
	t.show(w)
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

	// 2. The port Gatehouse is managed from
	mp, err := t.pickMgmtPort(env)
	if err != nil {
		return err
	}

	// 3. Confirm
	req := Request{Disk: disk.Path, MgmtPort: mp.Name, MgmtMAC: mp.MAC}
	for {
		t.banner(env, 3, steps, "Confirm")
		t.p("  Disk   %s%s %s%s  %s(everything on it will be erased)%s\n", bold, disk.Path, humanSize(disk.Size), reset, red, reset)
		t.p("  Port   %s%s%s  %s(you finish setup from a computer plugged into it)%s\n", bold, mp.Name, reset, dim, reset)
		t.p("\n  To go ahead, type %s%s%s below.\n  %sAnything else cancels.%s\n\n", bold, disk.Path, reset, dim, reset)
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

	// 4. Install
	for {
		st := env.In.Status()
		n, total := 0, len(st.Steps)
		cur := ""
		for _, x := range st.Steps {
			if x.State == "done" {
				n++
			}
			if x.State == "running" {
				cur = x.Name
			}
		}
		sc := NewScreen(t.W, t.H)
		switch st.State {
		case "done":
			return finish(t, env)
		case "error":
			sc.header("Installation failed")
			sc.mid(red + "●" + reset + bold + " " + trim(st.Error, t.W-8) + reset)
			sc.blank()
			for _, l := range strings.Split(lastLines(st.Log, 5), "\n") {
				sc.mid(dim + trim(l, t.W-8) + reset)
			}
			sc.blank()
			sc.mid(fg + "Nothing more was changed. Press Enter to start over." + reset)
			t.show(sc)
			if _, err := t.readLine(true); err != nil {
				return err
			}
			return errRestart
		}
		sc.header("Installing")
		sc.mid(bar(n, total, 40))
		sc.blank()
		if cur == "" {
			cur = "Starting"
		}
		sc.mid(bold + cur + reset + dim + fmtf("   step %d of %d", min(n+1, total), total) + reset)
		sc.blank()
		sc.mid(dim + "This takes a few minutes. Do not switch the machine off." + reset)
		t.show(sc)
		time.Sleep(env.Poll)
	}
}

func trim(s string, n int) string {
	if n < 10 {
		n = 10
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
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

// finish is the screen after a successful installation. It does not return until the machine has
// had time to restart, so the service never starts the installer over again.
func finish(t *Term, env *TextEnv) error {
	sc := NewScreen(t.W, t.H)
	sc.header("Installed")
	sc.mid(green + "●" + reset + bold + " Installation complete" + reset)
	sc.blank()
	sc.mid(bold + "Remove the USB stick now," + reset)
	sc.mid(fg + "then press Enter to restart." + reset)
	sc.blank()
	sc.mid(dim + "After it starts, this screen shows an address. Open it in a browser" + reset)
	sc.mid(dim + "on a computer plugged into the port you chose, to create your" + reset)
	sc.mid(dim + "administrator account and finish setup." + reset)
	t.show(sc)
	if _, err := t.readLine(true); err != nil {
		return err
	}
	if err := env.In.Reboot(); err != nil {
		return err
	}
	// The restart happens a moment from now, in the background. Returning here would end
	// this program before it, and the service would start the installer over again.
	r := NewScreen(t.W, t.H)
	r.header("Installed")
	r.mid(green + "●" + reset + bold + " Restarting" + reset)
	r.blank()
	r.mid(fg + "If you have not yet, take the USB stick out now." + reset)
	t.show(r)
	time.Sleep(env.restartWait())
	r = NewScreen(t.W, t.H)
	r.header("Installed")
	r.mid(amber + "●" + reset + bold + " Switch the machine off" + reset)
	r.blank()
	r.mid(fg + "It did not restart by itself. Hold the power button, take out the USB stick," + reset)
	r.mid(fg + "then switch it on again." + reset)
	t.show(r)
	time.Sleep(20 * env.restartWait()) // nothing more to do; do not start over
	return nil
}

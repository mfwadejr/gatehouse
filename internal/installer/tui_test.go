package installer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testEnv(in *Installer) *TextEnv {
	return &TextEnv{In: in,
		Ports: func() []Port {
			return []Port{{Name: "enp1s0"}, {Name: "enp2s0", Link: true, Speed: "1000"}, {Name: "enp3s0", USB: true}}
		},
		Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond, Serial: "ttyS0, 115200 baud, 8N1", RestartWait: time.Millisecond}
}

func run(t *testing.T, script string, env *TextEnv) (string, error) {
	var out bytes.Buffer
	err := RunText(NewTerm(strings.NewReader(script), &out), env)
	return out.String(), err
}

func TestTextInstallHappyPath(t *testing.T) {
	in, f := newTest(t)
	script := strings.Join([]string{
		"",             // welcome
		"1",            // internet port: enp1s0, which has no cable yet (allowed)
		"",             // management port: the only other one with a cable
		"",             // management address: the suggestion
		"",             // hand out addresses on it: the default
		"1",            // disk: the only usable one (must be typed; Enter alone does not choose)
		"/dev/nvme0n1", // confirm
		"",             // reboot
	}, "\r") + "\r"
	out, err := run(t, script, testEnv(in))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"115200", "Installation complete", "will be erased", "Remove the USB stick", "administrator account", "enp2s0", "cable connected", "Which port is the internet (WAN)?", "not plugged in yet", "Review and confirm", "192.168.50.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	joined := strings.Join(f.snap(), "\n")
	for _, c := range []string{"sgdisk --zap-all /dev/nvme0n1", "unsquashfs"} {
		if !strings.Contains(joined, c) {
			t.Errorf("missing command %q", c)
		}
	}
	// Only the disk is asked for: no account or network questions at the machine.
	for _, no := range []string{"Administrator", "Password", "time zone", "LAN port"} {
		if strings.Contains(out, no) {
			t.Errorf("the installer should not ask about %q", no)
		}
	}
}

func TestTextWrongConfirmationStartsOverAndErasesNothing(t *testing.T) {
	in, f := newTest(t)
	script := strings.Join([]string{"", "1", "", "", "", "1", "nope"}, "\n") + "\n"
	_, err := run(t, script, testEnv(in))
	if !errors.Is(err, errClosed) {
		t.Fatalf("expected the script to run out after starting over, got %v", err)
	}
	for _, c := range f.snap() {
		if strings.HasPrefix(c, "sgdisk") || strings.HasPrefix(c, "wipefs") {
			t.Fatalf("disk touched without confirmation: %s", c)
		}
	}
}

func TestReadLineEditing(t *testing.T) {
	var out bytes.Buffer
	tm := NewTerm(strings.NewReader("abx\x7fc\x1b[A\x1b[3~d\r\nnext\r"), &out)
	s, err := tm.readLine(true)
	if err != nil || s != "abcd" {
		t.Fatalf("%q %v", s, err)
	}
	// A CR LF pair is one Enter.
	if s, _ = tm.readLine(true); s != "next" {
		t.Fatalf("second line %q", s)
	}
	tm = NewTerm(strings.NewReader("secret\r"), &out)
	out.Reset()
	tm.readLine(false)
	if strings.Contains(out.String(), "secret") {
		t.Fatal("hidden input was echoed")
	}
	tm = NewTerm(strings.NewReader("abc\x03"), &out)
	if _, err := tm.readLine(true); !errors.Is(err, errRestart) {
		t.Fatalf("ctrl-c: %v", err)
	}
}

func TestSerialFromCmdline(t *testing.T) {
	cases := map[string][2]any{
		"boot=live quiet console=tty0 console=ttyS0,115200n8 nopewarden.text": {"ttyS0", 115200},
		"boot=live quiet console=tty0 console=ttyS0,9600n8 nopewarden.text":   {"ttyS0", 9600},
		"console=ttyS1,9600 nopewarden.text":                                  {"ttyS1", 9600},
		"boot=live nopewarden.text":                                           {"ttyS0", 115200},
		"boot=live nopewarden.serial=9600 nopewarden.text":                    {"ttyS0", 9600},
		"nopewarden.serial=9600 console=ttyS0,115200n8":                       {"ttyS0", 115200},
	}
	for in, want := range cases {
		d, b := SerialFromCmdline(in)
		if d != want[0] || b != want[1] {
			t.Errorf("%q: %s %d", in, d, b)
		}
	}
}

func TestRecoveryScreen(t *testing.T) {
	var got []string
	env := RecoveryEnv{
		Users:          func() []string { return []string{"marvin"} },
		SetPassword:    func(n, p string) error { got = append(got, "pw:"+n+":"+p); return nil },
		ClearTwoStep:   func(n string) error { got = append(got, "mfa:"+n); return nil },
		ResetFirewall:  func() error { got = append(got, "fw"); return nil },
		ResetAccess:    func() error { got = append(got, "access"); return nil },
		Diagnose:       func() string { return "nopewarden          failed" },
		Services:       func() string { return "DHCP (hands out addresses)\n  Listening: yes" },
		ResetToDefault: func(l string) (string, error) { got = append(got, "reset:"+l); return "/backup.db", nil },
	}
	script := strings.Join([]string{
		"1", "short", "a long enough pass", "a long enough pass", "", // new password (too short first)
		"2", "", // two-step off
		"3", "y", "", // firewall
		"4", "", // what is running
		"5", "", // DHCP and DNS status
		"6", "y", "", // port and management limit
		"7", "2", "nope", "", // reset: wrong word changes nothing
		"7", "1", "RESET", "", // network reset
		"8",
	}, "\r") + "\r"
	var out bytes.Buffer
	if err := RunRecovery(NewTerm(strings.NewReader(script), &out), env); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if strings.Join(got, ",") != "pw:marvin:a long enough pass,mfa:marvin,fw,access,reset:network" {
		t.Fatalf("actions: %v\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "nopewarden          failed") {
		t.Fatal("the diagnostics view shows nothing")
	}
	if !strings.Contains(out.String(), "Listening: yes") {
		t.Fatal("the DHCP and DNS view shows nothing")
	}
	if !strings.Contains(out.String(), "Too short: 5 of the 12") || strings.Contains(out.String(), "a long enough pass") {
		t.Fatal("password feedback wrong or the password was echoed")
	}
}

// After the installation the program must stay alive until the machine has restarted. If it
// returned straight away, the service would start the installer over again and the person would
// see the welcome screen instead of a restart.
func TestFinishedInstallWaitsForRestart(t *testing.T) {
	in, _ := newTest(t)
	restarted := make(chan struct{}, 1)
	in.RebootFn = func() { restarted <- struct{}{} }
	in.st.State = "done"
	env := testEnv(in)
	env.RestartWait = 200 * time.Millisecond
	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		// Enter at the "installed" screen.
		finish(NewTerm(strings.NewReader("\r"), &out), env)
		close(done)
	}()
	select {
	case <-restarted:
	case <-time.After(3 * time.Second):
		t.Fatal("the machine was never restarted")
	}
	select {
	case <-done:
		// returned only after the wait, never before the restart was requested
	case <-time.After(10 * time.Second):
		t.Fatal("the screen never ended")
	}
	if !strings.Contains(out.String(), "Restarting") {
		t.Fatalf("no restart message:\n%s", out.String())
	}
}

func TestManagementPortIsRememberedOnTheNewSystem(t *testing.T) {
	in, _ := newTest(t)
	env := testEnv(in)
	env.Ports = func() []Port {
		return []Port{{Name: "enp1s0", MAC: "aa:bb:cc:00:00:01"}, {Name: "enp2s0", MAC: "aa:bb:cc:00:00:02", Link: true}}
	}
	script := strings.Join([]string{"", "1", "", "", "", "1", "/dev/nvme0n1", ""}, "\r") + "\r"
	if out, err := run(t, script, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	deadline := time.Now().Add(3 * time.Second)
	for in.Status().State != "done" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	b, err := os.ReadFile(filepath.Join(in.Target, MgmtPortFile))
	if err != nil || strings.TrimSpace(string(b)) != "enp2s0 aa:bb:cc:00:00:02" {
		t.Fatalf("management port file: %q %v", b, err)
	}
	nb, err := os.ReadFile(filepath.Join(in.Target, InstallNetFile))
	if err != nil || string(nb) != "wan enp1s0 aa:bb:cc:00:00:01\nmgmt 192.168.50.1/24\n" {
		t.Fatalf("network choices file: %q %v", nb, err)
	}
}

// The DHCP choice made at the machine reaches the new system: another server on the LAN port turns it off
// by default, and the setup wizard is told.
func TestDHCPChoiceIsLeftOnTheNewSystem(t *testing.T) {
	in, _ := newTest(t)
	env := testEnv(in)
	env.Ports = func() []Port {
		return []Port{{Name: "enp1s0", MAC: "aa:bb:cc:00:00:01"}, {Name: "enp2s0", MAC: "aa:bb:cc:00:00:02", Link: true}}
	}
	env.Probe = func(p Port) ProbeResult {
		return ProbeResult{Port: p.Name, Offers: []Offer{offerFrom("192.168.88.1", "192.168.88.30/24")}}
	}
	// internet port 1, LAN port (the only one with a cable), address, DHCP default (off), disk, confirm.
	script := strings.Join([]string{"", "1", "", "", "", "1", "/dev/nvme0n1", ""}, "\r") + "\r"
	out, err := run(t, script, env)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "off, another server does it") {
		t.Fatalf("the confirm screen should show that DHCP is off:\n%s", out)
	}
	deadline := time.Now().Add(3 * time.Second)
	for in.Status().State != "done" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	nb, err := os.ReadFile(filepath.Join(in.Target, InstallNetFile))
	if err != nil || !strings.Contains(string(nb), "dhcp off\n") {
		t.Fatalf("network choices file: %q %v", nb, err)
	}
}

func TestManagementPortWithoutACableNeedsInsisting(t *testing.T) {
	ask := portAsk{step: 2, title: "Which port will you manage NopeWarden from?"}
	env := &TextEnv{Ports: func() []Port { return []Port{{Name: "enp1s0"}, {Name: "enp2s0"}} }, Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond}
	// No cable anywhere: Enter alone gets nowhere; choosing a port asks, and "n" goes back.
	script := strings.Join([]string{"", "2", "n", "2", "y"}, "\r") + "\r"
	var out bytes.Buffer
	p, err := NewTerm(strings.NewReader(script), &out).pickPort(env, ask)
	if err != nil || p.Name != "enp2s0" {
		t.Fatalf("%v %v\n%s", p, err, out.String())
	}
	if !strings.Contains(out.String(), "No cable is connected yet") || !strings.Contains(out.String(), "Use it anyway?") {
		t.Fatalf("missing warnings:\n%s", out.String())
	}
}

// The list follows the cables: plugging one in while the screen waits makes it appear and
// become the default.
func TestManagementPortListUpdatesByItself(t *testing.T) {
	var mu sync.Mutex
	linked := false
	ask := portAsk{step: 2, title: "Which port will you manage NopeWarden from?"}
	env := &TextEnv{Ports: func() []Port {
		mu.Lock()
		defer mu.Unlock()
		return []Port{{Name: "enp1s0", Link: linked}, {Name: "enp9s0"}}
	}, Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond}
	pr, pw := io.Pipe()
	var out safeBuf
	done := make(chan Port, 1)
	go func() {
		p, _ := NewTerm(pr, &out).pickPort(env, ask)
		done <- p
	}()
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	linked = true
	mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	pw.Write([]byte("\r"))
	select {
	case p := <-done:
		if p.Name != "enp1s0" || !p.Link {
			t.Fatalf("%+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("never finished:\n%s", out.String())
	}
	s := out.String()
	if !strings.Contains(s, "no cable") || !strings.Contains(s, "cable connected") {
		t.Fatalf("the list did not change when the cable was plugged in:\n%s", s)
	}
}

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *safeBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestScreensResetCharset(t *testing.T) {
	s := NewScreen(80, 24)
	s.header("Installer")
	if out := s.String(); !strings.HasPrefix(out, asciiReset) {
		t.Fatalf("screen must start by selecting ASCII, got %q", out[:12])
	}
	var b bytes.Buffer
	NewTerm(strings.NewReader(""), &b).clear()
	if !strings.HasPrefix(b.String(), asciiReset) {
		t.Fatalf("clear must select ASCII, got %q", b.String())
	}
}

func TestStrayEnterNeverChoosesTheDisk(t *testing.T) {
	in, _ := newTest(t)
	// Welcome, then four empty lines (a noisy serial line): no port or disk may get chosen.
	script := strings.Join([]string{"", "", "", "", ""}, "\r") + "\r"
	out, err := run(t, script, testEnv(in))
	if !errors.Is(err, errClosed) {
		t.Fatalf("expected the script to run out, got %v", err)
	}
	if strings.Contains(out, "Review and confirm") || strings.Contains(out, "step 3 of 4") {
		t.Fatalf("a stray Enter moved past the disk choice:\n%s", out)
	}
}

func TestRedrawingAScreenDoesNotWipeTheDisplay(t *testing.T) {
	var out strings.Builder
	term := NewTerm(strings.NewReader(""), &out)
	mk := func(n int) *Screen { s := NewScreen(80, 24); s.add("progress " + fmt.Sprint(n)); return s }
	term.show(mk(1))
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatal("the first screen should start from a clean display")
	}
	out.Reset()
	term.show(mk(2))
	if strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("a repaint must not clear the whole display (it makes it blink): %q", out.String())
	}
	if !strings.Contains(out.String(), "progress 2") || !strings.Contains(out.String(), "\x1b[K") {
		t.Fatalf("repaint: %q", out.String())
	}
	term.clear()
	out.Reset()
	term.show(mk(3))
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatal("after a clear the next screen starts clean again")
	}
}

func TestAnUnchangedScreenIsNotSentAgain(t *testing.T) {
	var out strings.Builder
	term := NewTerm(strings.NewReader(""), &out)
	mk := func() *Screen { s := NewScreen(80, 24); s.add("same"); return s }
	term.refresh(mk())
	out.Reset()
	term.refresh(mk())
	if out.Len() != 0 {
		t.Fatalf("an identical screen should send nothing, sent %q", out.String())
	}
}

func TestShowAlwaysDrawsEvenWhenUnchanged(t *testing.T) {
	var out strings.Builder
	term := NewTerm(strings.NewReader(""), &out)
	s := NewScreen(80, 24)
	s.add("same")
	term.show(s)
	out.Reset()
	term.show(s)
	if out.Len() == 0 {
		t.Fatal("a screen shown after a prompt must be drawn again so typed text is cleared")
	}
}

func TestAMachineWithOnePortIsToldItNeedsTwo(t *testing.T) {
	var mu sync.Mutex
	two := false
	env := &TextEnv{Ports: func() []Port {
		mu.Lock()
		defer mu.Unlock()
		if two {
			return []Port{{Name: "enp1s0", Link: true}, {Name: "enp5s0", USB: true}}
		}
		return []Port{{Name: "enp1s0", Link: true}}
	}, Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond}
	pr, pw := io.Pipe()
	var out safeBuf
	done := make(chan Port, 1)
	go func() {
		p, _ := NewTerm(pr, &out).pickPort(env, portAsk{step: 1, title: "Which network port is the internet?", wan: true})
		done <- p
	}()
	time.Sleep(60 * time.Millisecond)
	pw.Write([]byte("1\r")) // typed while there is nothing to choose: ignored
	time.Sleep(60 * time.Millisecond)
	select {
	case p := <-done:
		t.Fatalf("a port was chosen with only one on the machine: %+v", p)
	default:
	}
	if !strings.Contains(out.String(), "needs at least two network ports") {
		t.Fatalf("no explanation:\n%s", out.String())
	}
	mu.Lock()
	two = true
	mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	pw.Write([]byte("2\r"))
	select {
	case p := <-done:
		if p.Name != "enp5s0" {
			t.Fatalf("%+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("never finished:\n%s", out.String())
	}
}

func TestInternetPortIsAlwaysChosenByThePerson(t *testing.T) {
	env := &TextEnv{Ports: func() []Port {
		return []Port{{Name: "enp1s0", Link: true}, {Name: "enp2s0", Link: true, Addr: "203.0.113.9/24"}}
	}, Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond}
	var out bytes.Buffer
	// Enter alone chooses nothing, even though one port has an address.
	p, err := NewTerm(strings.NewReader("\r2\r"), &out).pickPort(env, portAsk{step: 1, title: "x", wan: true})
	if err != nil || p.Name != "enp2s0" {
		t.Fatalf("%+v %v\n%s", p, err, out.String())
	}
	if !strings.Contains(out.String(), "Pick the port the internet") {
		t.Fatalf("Enter was taken as a choice:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "address 203.0.113.9") || !strings.Contains(out.String(), "no address yet") {
		t.Fatalf("addresses not shown:\n%s", out.String())
	}
}

func TestManagementAddressAvoidsAndRefusesClashes(t *testing.T) {
	env := &TextEnv{Ports: func() []Port {
		return []Port{{Name: "enp1s0", Link: true, Addr: "192.168.50.20/24"}, {Name: "enp2s0", Link: true, Addr: "192.168.1.77/24"}}
	}}
	var out bytes.Buffer
	// The suggestion skips the network the internet port is on; a typed clash is refused.
	a, noDHCP, err := NewTerm(strings.NewReader("192.168.50.1\r\r\r"), &out).askMgmtAddr(env, Port{Name: "enp2s0"}, Port{Name: "enp1s0"})
	if err != nil || a != "192.168.77.1/24" || noDHCP {
		t.Fatalf("%q %v %v\n%s", a, noDHCP, err, out.String())
	}
	if !strings.Contains(out.String(), "overlaps") {
		t.Fatalf("a clash was not refused:\n%s", out.String())
	}
}

// lanScreen runs the address and DHCP questions for a LAN port (enp2s0) and an internet port (enp1s0)
// with the given probe results, and answers with input.
func lanScreen(t *testing.T, wanRes, lanRes ProbeResult, ports []Port, input string) (string, bool, string) {
	t.Helper()
	env := &TextEnv{Ports: func() []Port { return ports }, Refresh: func() {}, Poll: time.Millisecond, Settle: time.Millisecond,
		Probe: func(p Port) ProbeResult {
			if p.Name == "enp1s0" {
				return wanRes
			}
			return lanRes
		}}
	var out bytes.Buffer
	tm := NewTerm(strings.NewReader(input), &out)
	tm.probes.kick(ports, env.Probe)
	a, no, err := tm.askMgmtAddr(env, ports[1], ports[0])
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return a, no, out.String()
}

var twoLinkedPorts = []Port{{Name: "enp1s0", Link: true, MAC: "52:54:00:00:00:01"}, {Name: "enp2s0", Link: true, MAC: "52:54:00:00:00:02"}}

func offerFrom(server, given string) Offer {
	return Offer{Server: netip.MustParseAddr(server), Given: netip.MustParsePrefix(given)}
}

func TestAnotherServerOnTheLANPortTurnsDHCPOffAndSaysWhy(t *testing.T) {
	lan := ProbeResult{Port: "enp2s0", Offers: []Offer{offerFrom("192.168.88.1", "192.168.88.30/24")}}
	// Enter at the address, Enter at the question: the default is no.
	_, no, out := lanScreen(t, ProbeResult{Port: "enp1s0"}, lan, twoLinkedPorts, "\r\r")
	if !no {
		t.Fatalf("DHCP should default to off when another server answers:\n%s", out)
	}
	if !strings.Contains(out, "DHCP is off because another DHCP server (192.168.88.1)") || !strings.Contains(out, "answered on enp2s0") {
		t.Fatalf("the reason was not given:\n%s", out)
	}
	// It can be turned on anyway.
	_, no, _ = lanScreen(t, ProbeResult{Port: "enp1s0"}, lan, twoLinkedPorts, "\ry\r")
	if no {
		t.Fatal("answering y must turn DHCP on")
	}
}

func TestNoOtherServerMeansDHCPOnByDefault(t *testing.T) {
	_, no, out := lanScreen(t, ProbeResult{Port: "enp1s0", Offers: []Offer{offerFrom("203.0.113.1", "203.0.113.9/24")}}, ProbeResult{Port: "enp2s0"}, twoLinkedPorts, "\r\r")
	if no || !strings.Contains(out, "No other DHCP server answered on enp2s0") {
		t.Fatalf("no=%v\n%s", no, out)
	}
}

func TestACheckThatCouldNotRunIsSaidAndNeverBlocks(t *testing.T) {
	bad := ProbeResult{Port: "enp2s0", Err: errors.New("operation not permitted")}
	_, no, out := lanScreen(t, ProbeResult{Port: "enp1s0"}, bad, twoLinkedPorts, "\r\r")
	if no || !strings.Contains(out, "Could not check whether another DHCP server is on enp2s0") {
		t.Fatalf("no=%v\n%s", no, out)
	}
}

func TestBothPortsOnOneNetworkWarnsButContinues(t *testing.T) {
	one := []Offer{offerFrom("192.168.1.1", "192.168.1.155/24")}
	a, no, out := lanScreen(t, ProbeResult{Port: "enp1s0", Offers: one}, ProbeResult{Port: "enp2s0", Offers: one}, twoLinkedPorts, "\r\r")
	if !strings.Contains(out, "look like one network") || !strings.Contains(out, "192.168.1.1") {
		t.Fatalf("no warning:\n%s", out)
	}
	if !no {
		t.Fatal("another server answers on the LAN port, so DHCP should default to off")
	}
	// The suggested LAN address stays out of the network the internet port was offered.
	if strings.HasPrefix(a, "192.168.1.") {
		t.Fatalf("the LAN address %s is inside the internet port's network", a)
	}
}

func TestNetworkOfferedToTheInternetPortIsAvoided(t *testing.T) {
	// The internet port has no address yet, but the server offered 192.168.50.0/24: do not suggest it.
	wan := ProbeResult{Port: "enp1s0", Offers: []Offer{offerFrom("192.168.50.1", "192.168.50.20/24")}}
	a, _, _ := lanScreen(t, wan, ProbeResult{Port: "enp2s0"}, twoLinkedPorts, "\r\r")
	if strings.HasPrefix(a, "192.168.50.") {
		t.Fatalf("suggested %s, which is the internet port's network", a)
	}
}

func TestPortListSaysWhatWasHeardOnEachPort(t *testing.T) {
	b := newProbeBook()
	one := []Offer{offerFrom("192.168.1.1", "192.168.1.155/24")}
	b.kick(twoLinkedPorts, func(p Port) ProbeResult { return ProbeResult{Port: p.Name, Offers: one} })
	b.wait("enp1s0", time.Second)
	b.wait("enp2s0", time.Second)
	if n := portNote(b, twoLinkedPorts[0], true, ""); !strings.Contains(n, "a DHCP server answers here (192.168.1.1)") {
		t.Fatalf("internet side note: %q", n)
	}
	n := portNote(b, twoLinkedPorts[1], false, "enp1s0")
	if !strings.Contains(n, "another DHCP server answers here") || !strings.Contains(n, "one network?") {
		t.Fatalf("LAN side note: %q", n)
	}
	if portNote(b, Port{Name: "enp3s0"}, false, "") != "" {
		t.Fatal("a port with no cable gets no note")
	}
}

// plainRows returns the rows of a rendered screen with the colour codes and the tags taken out.
func plainRows(s *Screen) []string {
	out := s.String()
	out = strings.TrimPrefix(out, asciiReset+"\x1b[?25l\x1b[0m\x1b[2J\x1b[H")
	var rows []string
	for _, r := range strings.Split(out, "\r\n") {
		var b strings.Builder
		for i := 0; i < len(r); i++ {
			if r[i] == 0x1b {
				for i < len(r) && !(r[i] >= 'A' && r[i] <= 'Z' || r[i] >= 'a' && r[i] <= 'z') {
					i++
				}
				continue
			}
			b.WriteByte(r[i])
		}
		rows = append(rows, b.String())
	}
	return rows
}

func firstRow(rows []string, want string) int {
	for i, r := range rows {
		if strings.Contains(r, want) {
			return i
		}
	}
	return -1
}

func TestScreenIsCentredTopToBottom(t *testing.T) {
	for _, h := range []int{24, 40, 60} {
		s := NewScreen(80, h)
		s.add("first")
		s.add("last")
		s.lines = append(s.lines, "\x00footerbottom")
		rows := plainRows(s)
		a, b := firstRow(rows, "first"), firstRow(rows, "last")
		if b != a+1 {
			t.Fatalf("h=%d: lines are not together: %d %d", h, a, b)
		}
		above, below := a, (h-2)-b-1 // rows above the content, and between it and the footer row
		if d := above - below; d < -1 || d > 1 {
			t.Fatalf("h=%d: not centred: %d rows above, %d below", h, above, below)
		}
		if firstRow(rows, "bottom") != h-2 {
			t.Fatalf("h=%d: footer not on the last row", h)
		}
	}
}

func TestTallScreenDropsTheBadgeFirst(t *testing.T) {
	s := NewScreen(80, 24)
	s.header("Installer")
	for i := 0; i < 14; i++ {
		s.add(fmt.Sprintf("choice %d", i))
	}
	s.lines = append(s.lines, "\x00footerfoot")
	rows := plainRows(s)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, shield[0]) {
		t.Fatal("the badge should be dropped when the screen is too short for it")
	}
	if firstRow(rows, "N O P E W A R D E N") < 0 || firstRow(rows, "choice 13") < 0 {
		t.Fatalf("the name and the last choice must both show:\n%s", joined)
	}
	if len(rows) > 24 {
		t.Fatalf("%d rows on a 24-row screen", len(rows))
	}
}

func TestBadgeStaysWhenThereIsRoom(t *testing.T) {
	s := NewScreen(80, 40)
	s.header("Installer")
	s.add("choice")
	if !strings.Contains(strings.Join(plainRows(s), "\n"), shield[0]) {
		t.Fatal("the badge should show on a roomy screen")
	}
}

func TestPlainHeaderLeavesOutTheBadge(t *testing.T) {
	defer func() { PlainHeader = false }()
	PlainHeader = true
	s := NewScreen(80, 40)
	s.header("Installer")
	got := strings.Join(plainRows(s), "\n")
	if strings.Contains(got, shield[0]) {
		t.Fatal("no badge on a serial console")
	}
	if !strings.Contains(got, "N O P E W A R D E N") {
		t.Fatal("the name must stay")
	}
}

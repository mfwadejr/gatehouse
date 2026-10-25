package installer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
		"1",            // disk: the only usable one (must be typed; Enter alone does not choose)
		"",             // port: the only one with a cable
		"/dev/nvme0n1", // confirm
		"",             // reboot
	}, "\r") + "\r"
	out, err := run(t, script, testEnv(in))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"115200", "Installation complete", "will be erased", "Remove the USB stick", "administrator account", "enp2s0", "cable connected"} {
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
	script := strings.Join([]string{"", "1", "", "nope"}, "\n") + "\n"
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
		"boot=live quiet console=tty0 console=ttyS0,115200n8 gatehouse.text": {"ttyS0", 115200},
		"console=ttyS1,9600 gatehouse.text":                                  {"ttyS1", 9600},
		"boot=live gatehouse.text":                                           {"ttyS0", 115200},
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
		Diagnose:       func() string { return "gatehouse          failed" },
		ResetToDefault: func(l string) (string, error) { got = append(got, "reset:"+l); return "/backup.db", nil },
	}
	script := strings.Join([]string{
		"1", "short", "a long enough pass", "a long enough pass", "", // new password (too short first)
		"2", "", // two-step off
		"3", "y", "", // firewall
		"4", "", // what is running
		"5", "y", "", // port and management limit
		"6", "2", "nope", "", // reset: wrong word changes nothing
		"6", "1", "RESET", "", // network reset
		"7",
	}, "\r") + "\r"
	var out bytes.Buffer
	if err := RunRecovery(NewTerm(strings.NewReader(script), &out), env); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if strings.Join(got, ",") != "pw:marvin:a long enough pass,mfa:marvin,fw,access,reset:network" {
		t.Fatalf("actions: %v\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "gatehouse          failed") {
		t.Fatal("the diagnostics view shows nothing")
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
	script := strings.Join([]string{"", "1", "", "/dev/nvme0n1", ""}, "\r") + "\r"
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
}

func TestManagementPortWithoutACableNeedsInsisting(t *testing.T) {
	env := &TextEnv{Ports: func() []Port { return []Port{{Name: "enp1s0"}, {Name: "enp2s0"}} }, Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond}
	// No cable anywhere: Enter alone gets nowhere; choosing a port asks, and "n" goes back.
	script := strings.Join([]string{"", "2", "n", "2", "y"}, "\r") + "\r"
	var out bytes.Buffer
	p, err := NewTerm(strings.NewReader(script), &out).pickMgmtPort(env)
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
	env := &TextEnv{Ports: func() []Port { mu.Lock(); defer mu.Unlock(); return []Port{{Name: "enp1s0", Link: linked}} }, Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond}
	pr, pw := io.Pipe()
	var out safeBuf
	done := make(chan Port, 1)
	go func() {
		p, _ := NewTerm(pr, &out).pickMgmtPort(env)
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
	// Welcome, then four empty lines (a noisy serial line): the disk list must still be waiting.
	script := strings.Join([]string{"", "", "", "", ""}, "\r") + "\r"
	out, err := run(t, script, testEnv(in))
	if !errors.Is(err, errClosed) {
		t.Fatalf("expected the script to run out, got %v", err)
	}
	if strings.Contains(out, "step 2 of 3") {
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

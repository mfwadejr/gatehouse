package installer

import (
	"bytes"
	"errors"
	"strings"
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
		"",             // disk: the only usable one
		"/dev/nvme0n1", // confirm
		"",             // reboot
	}, "\r") + "\r"
	out, err := run(t, script, testEnv(in))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"115200", "Installation complete", "will be erased", "create your administrator account"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	joined := strings.Join(f.cmds, "\n")
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
	script := strings.Join([]string{"", "", "nope"}, "\n") + "\n"
	_, err := run(t, script, testEnv(in))
	if !errors.Is(err, errClosed) {
		t.Fatalf("expected the script to run out after starting over, got %v", err)
	}
	for _, c := range f.cmds {
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
		Users:         func() []string { return []string{"marvin"} },
		SetPassword:   func(n, p string) error { got = append(got, "pw:"+n+":"+p); return nil },
		ClearTwoStep:  func(n string) error { got = append(got, "mfa:"+n); return nil },
		ResetFirewall: func() error { got = append(got, "fw"); return nil },
		Diagnose:      func() string { return "gatehouse          failed" },
	}
	script := strings.Join([]string{
		"1", "short", "a long enough pass", "a long enough pass", "", // new password (too short first)
		"2", "", // two-step off
		"3", "y", "", // firewall
		"4", "", // what is running
		"5",
	}, "\r") + "\r"
	var out bytes.Buffer
	if err := RunRecovery(NewTerm(strings.NewReader(script), &out), env); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if strings.Join(got, ",") != "pw:marvin:a long enough pass,mfa:marvin,fw" {
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

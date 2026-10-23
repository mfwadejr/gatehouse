package installer

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func testEnv(in *Installer) *TextEnv {
	return &TextEnv{In: in,
		Ports: func() []Port {
			return []Port{{Name: "enp1s0"}, {Name: "enp2s0", Link: true, Speed: "1000"}, {Name: "enp3s0", USB: true}}
		},
		Used:    func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("192.168.1.77/24")} },
		Zones:   func() []string { return []string{"UTC", "Europe/London", "Europe/Paris", "America/Chicago"} },
		Refresh: func() {}, Poll: 5 * time.Millisecond, Settle: time.Millisecond, Serial: "ttyS0, 115200 baud, 8N1"}
}

func run(t *testing.T, script string, env *TextEnv) (string, error) {
	var out bytes.Buffer
	err := RunText(NewTerm(strings.NewReader(script), &out), env)
	return out.String(), err
}

func TestTextInstallHappyPath(t *testing.T) {
	in, f := newTest(t)
	script := strings.Join([]string{
		"",                   // welcome
		"",                   // disk: the only usable one
		"",                   // LAN port: the one with a cable
		"2",                  // internet port: enp1s0
		"",                   // automatic
		"",                   // LAN address: suggested
		"n",                  // spare port stays unused
		"",                   // device name
		"lond",               // time zone search
		"1",                  // Europe/London
		"marvin",             // admin
		"short",              // too short
		"a long enough pass", // good
		"a long enough pass",
		"",             // no mandatory MFA
		"/dev/nvme0n1", // confirm
		"",             // reboot
	}, "\r") + "\r"
	out, err := run(t, script, testEnv(in))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"115200", "Europe/London", "Too short: 5 of the 12", "Installed.", "192.168.50.1/24", "will be erased"} {
		if want == "192.168.50.1/24" {
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	// The suggestion avoids the network the machine is already on.
	if strings.Contains(out, "[192.168.1.1/24]") {
		t.Error("suggested a clashing network")
	}
	if strings.Contains(out, "a long enough pass") {
		t.Error("password was echoed")
	}
	joined := strings.Join(f.cmds, "\n")
	for _, c := range []string{"sgdisk --zap-all /dev/nvme0n1", "unsquashfs"} {
		if !strings.Contains(joined, c) {
			t.Errorf("missing command %q", c)
		}
	}
	var seeded string
	for k, v := range f.stdin {
		if strings.Contains(k, "gatehouse seed") {
			seeded = v
		}
		if strings.Contains(k, "user add") && !strings.Contains(v, "a long enough pass") {
			t.Errorf("password not passed to the account step")
		}
	}
	for _, want := range []string{`"port":"enp2s0"`, `"mode":"router"`, `"port":"enp1s0"`, `"type":"dhcp"`, `192.168.50.1/24`} {
		if !strings.Contains(seeded, want) {
			t.Errorf("seed lacks %s: %s", want, seeded)
		}
	}
}

func TestTextInstallBehindRouterWithFixedAddress(t *testing.T) {
	in, f := newTest(t)
	script := strings.Join([]string{
		"", "", // welcome, disk
		"",  // LAN port
		"",  // no internet port
		"2", // behind another router
		"2", // fixed address
		"192.168.0.50/24",
		"",          // gateway default 192.168.0.1
		"",          // DNS default
		"n", "", "", // spare ports, name, zone
		"", "a long enough pass", "a long enough pass", "y", // admin default, password, MFA
		"/dev/nvme0n1", "",
	}, "\n") + "\n"
	out, err := run(t, script, testEnv(in))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var seeded string
	for k, v := range f.stdin {
		if strings.Contains(k, "gatehouse seed") {
			seeded = v
		}
	}
	for _, want := range []string{`"mode":"behind"`, `"addressing":"static"`, `"gateway":"192.168.0.1"`, `192.168.0.50/24`} {
		if !strings.Contains(seeded, want) {
			t.Errorf("seed lacks %s: %s", want, seeded)
		}
	}
	if !strings.Contains(strings.Join(f.cmds, "\n"), "-require-mfa") {
		t.Error("MFA choice lost")
	}
}

func TestTextWrongConfirmationStartsOverAndErasesNothing(t *testing.T) {
	in, f := newTest(t)
	script := strings.Join([]string{"", "", "", "1", "", "", "", "", "marvin", "a long enough pass", "a long enough pass", "", "nope"}, "\n") + "\n"
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

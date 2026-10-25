package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSerialConsoleChoice(t *testing.T) {
	dir := t.TempDir()
	m := filepath.Join(dir, "serial-console")
	if d, _ := serialConsole(m, "quiet"); d != "" {
		t.Fatalf("no serial expected, got %q", d)
	}
	if d, b := serialConsole(m, "quiet console=tty0 console=ttyS1,57600n8"); d != "ttyS1" || b != 57600 {
		t.Fatalf("cmdline: %s %d", d, b)
	}
	os.WriteFile(m, []byte("ttyS0 115200\n"), 0o644)
	if d, b := serialConsole(m, "quiet"); d != "ttyS0" || b != 115200 {
		t.Fatalf("marker: %s %d", d, b)
	}
}

func TestGrubConfigForSerial(t *testing.T) {
	if grubConfig("") != grubDefaults {
		t.Fatal("no serial must leave the defaults untouched")
	}
	c := grubConfig("ttyS0,115200")
	for _, want := range []string{
		"console=tty0 console=ttyS0,115200n8",
		`GRUB_TERMINAL_OUTPUT="gfxterm serial"`,
		`GRUB_TERMINAL_INPUT="console serial"`,
		"serial --unit=0 --speed=115200",
	} {
		if !strings.Contains(c, want) {
			t.Fatalf("missing %q in\n%s", want, c)
		}
	}
	if strings.Count(c, "GRUB_TERMINAL_OUTPUT") != 1 {
		t.Fatal("terminal output set twice")
	}
}

func TestSerialRequestIsChecked(t *testing.T) {
	disks := []Disk{{Path: "/dev/sda"}}
	ok := &Request{Disk: "/dev/sda", Confirm: "/dev/sda", Serial: "ttyS0,115200"}
	if err := Validate(ok, disks); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"tty1,115200", "ttyS0;reboot,115200", "ttyS0,9", "ttyS0"} {
		r := &Request{Disk: "/dev/sda", Confirm: "/dev/sda", Serial: bad}
		if Validate(r, disks) == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}

func TestInstallOverSerialLeavesTheNewSystemTalkingOnIt(t *testing.T) {
	in, _ := newTest(t)
	if err := in.brand(in.Target, "ttyS0,115200"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(in.Target, "etc/default/grub.d/gatehouse.cfg"))
	if err != nil || !strings.Contains(string(b), "console=ttyS0,115200n8") {
		t.Fatalf("grub config: %v\n%s", err, b)
	}
	m, err := os.ReadFile(filepath.Join(in.Target, strings.TrimPrefix(SerialMarker, "/")))
	if err != nil || strings.TrimSpace(string(m)) != "ttyS0 115200" {
		t.Fatalf("marker: %v %q", err, m)
	}
	in2, _ := newTest(t)
	if err := in2.brand(in2.Target, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(in2.Target, strings.TrimPrefix(SerialMarker, "/"))); err == nil {
		t.Fatal("an install not done over serial must not leave the marker")
	}
}

func TestSerialStatusRepaintsOnKeyAndWhenStale(t *testing.T) {
	var out strings.Builder
	f, _ := os.CreateTemp(t.TempDir(), "ser")
	s := &serialStatus{f: f}
	s.show("A")
	s.show("A") // unchanged: nothing more
	s.poke.Store(true)
	s.show("A") // a key was pressed: draw again
	f.Seek(0, 0)
	b, _ := os.ReadFile(f.Name())
	out.Write(b)
	if out.String() != "AA" {
		t.Fatalf("got %q, want AA", out.String())
	}
}

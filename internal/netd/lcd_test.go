package netd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCFAChecksumAndReply(t *testing.T) {
	// A packet followed by its own checksum always leaves the same remainder.
	p := cfaPacket(0x01, nil)
	if got := cfaCRC(p); got != 0x0F47 {
		t.Fatalf("remainder %#04x, want 0x0f47", got)
	}
	data := []byte("CFA631: h1.2, v2.4")
	reply := append([]byte{0x41, byte(len(data))}, data...)
	c := cfaCRC(reply)
	reply = append(reply, byte(c), byte(c>>8))
	pr, ok := parseCFAReply(append([]byte{0x00, 0xff}, reply...)) // noise in front
	if !ok || pr.Model != "CFA631" || pr.Width != 20 || pr.Height != 2 || pr.Keys != 6 {
		t.Fatalf("got %+v ok=%v", pr, ok)
	}
	reply[len(reply)-1] ^= 0xff
	if _, ok := parseCFAReply(reply); ok {
		t.Fatal("a reply with a wrong checksum must be refused")
	}
}

func TestParseI2CDetect(t *testing.T) {
	out := `     0  1  2  3  4  5  6  7  8  9  a  b  c  d  e  f
20: -- -- -- -- -- -- -- 27 -- -- -- -- -- -- -- --
30: -- -- -- -- -- -- -- -- -- -- -- -- -- -- -- --
38: UU -- -- -- -- -- -- -- `
	got := parseI2CDetect(out)
	if len(got) != 1 || got[0] != "0x27" {
		t.Fatalf("got %v", got)
	}
}

func TestLCDConfKeepsTheRestAndChoosesOneDriver(t *testing.T) {
	base := "[server]\nDriver=curses\nDriver=hd44780\n#Bind=127.0.0.1\nPort=1\n\n[hd44780]\n#Size=20x4\nConnectionType=4bit\n\n[menu]\nMenuKey=Escape\n"
	out := LCDConf(base, LCDPlan{Enabled: true, Driver: "CFontz633", Settings: map[string]string{"Device": "/dev/ttyUSB0", "Size": "20x2"}})
	for _, want := range []string{"Driver=CFontz633", "#Driver=hd44780", "Bind=127.0.0.1", "Port=13666", "[CFontz633]\nDevice=/dev/ttyUSB0\nSize=20x2", "[menu]\nMenuKey=Escape", "ConnectionType=4bit"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "\nDriver=") != 1 {
		t.Errorf("exactly one active driver wanted:\n%s", out)
	}
	// A commented-out key in the driver's own section is switched on in place.
	out = LCDConf(base, LCDPlan{Enabled: true, Driver: "hd44780", Settings: map[string]string{"Size": "20x2", "ConnectionType": "i2c"}})
	if !strings.Contains(out, "Size=20x2") || strings.Contains(out, "#Size=20x4") || !strings.Contains(out, "ConnectionType=i2c") || strings.Contains(out, "ConnectionType=4bit") {
		t.Errorf("keys not set in place:\n%s", out)
	}
}

func TestLCDPlanCheck(t *testing.T) {
	if err := (LCDPlan{Enabled: true, Driver: "hd44780;rm", Settings: nil}).Check(); err == nil {
		t.Error("a driver name with a ; must be refused")
	}
	if err := (LCDPlan{Enabled: true, Driver: "hd44780", Settings: map[string]string{"Size": "20x2\n[server]"}}).Check(); err == nil {
		t.Error("a value with a newline must be refused (it could add a section)")
	}
	if err := (LCDPlan{Enabled: true, Driver: "hd44780", Settings: map[string]string{"Si ze": "1"}}).Check(); err == nil {
		t.Error("a bad setting name must be refused")
	}
	if err := (LCDPlan{Enabled: true, Driver: "CFontz633", Settings: map[string]string{"Device": "/dev/ttyUSB0", "Size": "20x2"}}).Check(); err != nil {
		t.Error(err)
	}
}

func TestLCDDetectReportsFactsAndConfirmedMatch(t *testing.T) {
	root := t.TempDir()
	w := func(p, v string) {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(v), 0o644)
	}
	w("sys/class/dmi/id/sys_vendor", "Sophos\n")
	w("sys/class/dmi/id/product_name", "SG210\n")
	w("sys/bus/usb/devices/1-1/idVendor", "0403\n")
	w("sys/bus/usb/devices/1-1/idProduct", "6001\n")
	w("sys/bus/usb/devices/1-1/product", "FT232R USB UART\n")
	w("dev/ttyUSB0", "")
	w("proc/cmdline", "root=/dev/sda1 console=ttyS0,115200")
	w("dev/ttyS0", "")
	w("sys/class/tty/ttyS0/device/x", "")
	w("proc/tty/driver/serial", "0: uart:16550A port:000003F8 irq:4\n")
	old := lcdProbeSerial
	t.Cleanup(func() { lcdProbeSerial = old })
	asked := []string{}
	lcdProbeSerial = func(dev string) *LCDProbe {
		asked = append(asked, dev)
		return &LCDProbe{Model: "CFA631", Firmware: "hardware h1.2, firmware v2.4", Speed: 115200, Width: 20, Height: 2, Keys: 6}
	}
	oldRun := sysRun
	t.Cleanup(func() { sysRun = oldRun })
	sysRun = func(time.Duration, string, ...string) (string, error) { return "", nil }
	s := &Service{Root: root}
	f, err := s.LCDDetect()
	if err != nil {
		t.Fatal(err)
	}
	if f.Vendor != "Sophos" || f.Product != "SG210" || len(f.USB) != 1 || f.USB[0].ID != "0403:6001" || f.USB[0].Note == "" {
		t.Fatalf("facts: %+v", f)
	}
	if len(asked) != 1 || asked[0] != "/dev/ttyUSB0" {
		t.Fatalf("the serial console must not be asked, the USB serial port must: %v", asked)
	}
	if len(f.Matches) == 0 || f.Matches[0].Sure != "confirmed" || f.Matches[0].Driver != "CFontz633" || f.Matches[0].Settings["Size"] != "20x2" || f.Matches[0].Settings["Speed"] != "115200" {
		t.Fatalf("matches: %+v", f.Matches)
	}
	if !strings.Contains(f.Report, "Sophos SG210") || !strings.Contains(f.Report, "0403:6001") || !strings.Contains(f.Report, "CFA631") {
		t.Errorf("the plain-text report must hold the facts:\n%s", f.Report)
	}
	var consoleNote bool
	for _, p := range f.Serial {
		if p.Path == "/dev/ttyS0" && strings.Contains(p.Note, "console") {
			consoleNote = true
		}
	}
	if !consoleNote {
		t.Errorf("the console port should be listed with a note: %+v", f.Serial)
	}
}

func TestLCDDetectWithNothingStillTellsWhatItSaw(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root}
	f, err := s.LCDDetect()
	if err != nil || len(f.Matches) != 0 || len(f.Notes) == 0 || f.Report == "" {
		t.Fatalf("%v %+v", err, f)
	}
}

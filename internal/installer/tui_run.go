package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var reConsole = regexp.MustCompile(`(?:^|\s)console=(ttyS\d+)(?:,(\d+))?`)

// SerialFromCmdline finds the serial console the boot menu asked for. With none named, the
// first serial port at 115200 is used, as the boot menu entry says.
func SerialFromCmdline(cmdline string) (dev string, baud int) {
	m := reConsole.FindStringSubmatch(cmdline)
	if m == nil {
		return "ttyS0", 115200
	}
	baud = 115200
	if n, err := strconv.Atoi(m[2]); err == nil && n > 0 {
		baud = n
	}
	return m[1], baud
}

// hasUART says whether a serial port really exists; the kernel lists empty slots too.
func hasUART(dev string) bool {
	n := strings.TrimPrefix(dev, "ttyS")
	b, err := os.ReadFile("/proc/tty/driver/serial")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, n+":") && !strings.Contains(l, "uart:unknown") {
			return true
		}
	}
	return false
}

func rawTTY(path string, baud int) {
	args := []string{"-F", path, "raw", "-echo", "-echoe", "-echok", "-ixon", "-ixoff", "clocal", "-crtscts"}
	if baud > 0 {
		args = append([]string{"-F", path, strconv.Itoa(baud), "cs8", "-parenb", "-cstopb"}, args[2:]...)
	}
	exec.Command("stty", args...).Run()
}

// RunTextInstaller runs the wizard on the machine's screen and keyboard and on the serial
// console, both at once: whichever one is used first gets the answers.
func RunTextInstaller() error {
	cmdline, _ := os.ReadFile("/proc/cmdline")
	sdev, baud := SerialFromCmdline(string(cmdline))

	var outs []io.Writer
	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	pump := func(f *os.File) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 256)
			for {
				n, err := f.Read(buf)
				if n > 0 {
					pw.Write(buf[:n])
				}
				if err != nil {
					return
				}
			}
		}()
	}
	serial := ""
	disp := openGraphics()
	if f, err := os.OpenFile("/dev/tty1", os.O_RDWR, 0); err == nil {
		rawTTY("/dev/tty1", 0)
		if disp != nil {
			outs = append(outs, disp) // drawn as graphics; the keyboard still comes from the console
		} else {
			io.WriteString(f, palette) // the Gatehouse colours, as on the other screens
			outs = append(outs, f)
		}
		pump(f)
	}
	if hasUART(sdev) {
		if f, err := os.OpenFile("/dev/"+sdev, os.O_RDWR|0x100 /* O_NOCTTY */, 0); err == nil {
			rawTTY("/dev/"+sdev, baud)
			outs = append(outs, f)
			pump(f)
			serial = fmt.Sprintf("%s, %d baud, 8 data bits, no parity, 1 stop bit (8N1), no flow control", sdev, baud)
		}
	}
	if len(outs) == 0 {
		return fmt.Errorf("no console to use")
	}
	t := NewTerm(pr, io.MultiWriter(outs...))
	if len(outs) > 0 {
		t.W, t.H = screenSize(disp)
		if serial != "" {
			t.W, t.H = min(t.W, 80), min(t.H, 24) // the serial terminal's size is unknown
		}
	}
	env := NewTextEnv(New(), serial)
	err := RunText(t, env)
	if err == errClosed {
		select {} // never exit on a lost console; the service would only restart the wizard
	}
	return err
}

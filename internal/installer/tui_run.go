package installer

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

func usedPrefixes() []netip.Prefix {
	var used []netip.Prefix
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, _ := ifi.Addrs()
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				if p, err := netip.ParsePrefix(n.String()); err == nil && !p.Addr().IsLinkLocalUnicast() {
					used = append(used, p)
				}
			}
		}
	}
	return used
}

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
	if f, err := os.OpenFile("/dev/tty1", os.O_RDWR, 0); err == nil {
		rawTTY("/dev/tty1", 0)
		outs = append(outs, f)
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
	env := NewTextEnv(New(), serial)
	err := RunText(t, env)
	if err == errClosed {
		select {} // never exit on a lost console; the service would only restart the wizard
	}
	return err
}

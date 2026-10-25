package installer

import (
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// SerialMarker is where the installer leaves "ttyS0 115200" when the machine was installed over a
// serial console, so the status screen keeps talking on that port.
var SerialMarker = "/var/lib/gatehouse/serial-console"

// serialConsole says which serial port the status screen should also use: the one the installer
// left in the marker, or one the kernel was told about with console=ttyS0,115200 on its command line.
func serialConsole(marker, cmdline string) (dev string, baud int) {
	if b, err := os.ReadFile(marker); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 1 && strings.HasPrefix(f[0], "ttyS") {
			baud = 115200
			if len(f) >= 2 {
				if n, err := strconv.Atoi(f[1]); err == nil && n > 0 {
					baud = n
				}
			}
			return f[0], baud
		}
	}
	if m := reConsole.FindStringSubmatch(cmdline); m != nil {
		baud = 115200
		if n, err := strconv.Atoi(m[2]); err == nil && n > 0 {
			baud = n
		}
		return m[1], baud
	}
	return "", 0
}

// serialStatus mirrors the status screen onto a serial port, in 80 by 24 characters (the size of
// the terminal at the other end is unknown). It repaints on any change, every half minute, and
// whenever a key is pressed there, so opening a terminal later still shows the address.
type serialStatus struct {
	dev     string
	baud    int
	f       *os.File
	poke    atomic.Bool
	lastOut string
	lastAt  time.Time
}

func newSerialStatus() *serialStatus {
	cmd, _ := os.ReadFile("/proc/cmdline")
	dev, baud := serialConsole(SerialMarker, string(cmd))
	if dev == "" || !hasUART(dev) {
		return nil
	}
	return &serialStatus{dev: dev, baud: baud}
}

func (s *serialStatus) open() bool {
	if s.f != nil {
		return true
	}
	f, err := os.OpenFile("/dev/"+s.dev, os.O_RDWR|0x100 /* O_NOCTTY */, 0)
	if err != nil {
		return false
	}
	rawTTY("/dev/"+s.dev, s.baud)
	s.f = f
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				s.poke.Store(true)
			}
			if err != nil {
				return
			}
		}
	}()
	return true
}

// show writes the frame when it changed, when someone pressed a key, or when it is stale.
func (s *serialStatus) show(frame string) {
	if !s.open() {
		return
	}
	if frame == s.lastOut && !s.poke.Swap(false) && time.Since(s.lastAt) < 30*time.Second {
		return
	}
	s.poke.Store(false)
	io.WriteString(s.f, frame)
	s.lastOut, s.lastAt = frame, time.Now()
}

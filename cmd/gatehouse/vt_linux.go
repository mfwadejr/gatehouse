package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// showStatusScreen switches the machine's display back to screen 1, the status screen.
func showStatusScreen() {
	f, err := os.OpenFile("/dev/tty0", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer f.Close()
	const vtActivate, vtWaitActive = 0x5606, 0x5607
	unix.Syscall(unix.SYS_IOCTL, f.Fd(), vtActivate, 1)
	unix.Syscall(unix.SYS_IOCTL, f.Fd(), vtWaitActive, 1)
}

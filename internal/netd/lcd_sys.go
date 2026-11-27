package netd

import "syscall"

// Flags for opening a serial port without making it this process's controlling terminal and without waiting for it.
const (
	syscallNoctty   = syscall.O_NOCTTY
	syscallNonblock = syscall.O_NONBLOCK
)

package netd

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// bindToDevice makes a socket leave through one network device, whatever the routing says.
func bindToDevice(dev string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) { serr = unix.BindToDevice(int(fd), dev) }); err != nil {
			return err
		}
		return serr
	}
}

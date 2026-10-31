package dnssrv

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// BindControl makes a socket leave through one network device, whatever the routing says. The
// resolver uses it to send a device's lookups out through a VPN tunnel.
func BindControl(dev string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) { serr = unix.BindToDevice(int(fd), dev) }); err != nil {
			return err
		}
		return serr
	}
}

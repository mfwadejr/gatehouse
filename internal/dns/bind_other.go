//go:build !linux

package dnssrv

import (
	"errors"
	"syscall"
)

// BindControl is only supported on Linux. Elsewhere lookups through a tunnel fail rather than
// leave by the normal connection.
func BindControl(dev string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		return errors.New("sending lookups through a tunnel is not supported here")
	}
}

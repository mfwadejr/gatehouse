//go:build !linux

package speedtest

import "syscall"

func bindToDevice(dev string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error { return nil }
}

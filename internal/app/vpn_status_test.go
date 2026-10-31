package app

import (
	"strings"
	"testing"
)

func TestClientProblemSaysWhyInPlainWords(t *testing.T) {
	if got := clientProblem("degraded", "Connected, but traffic is not getting through (test to 8.8.8.8 failed)", "", "", false); !strings.Contains(got, "has not given this device an address") {
		t.Errorf("no-address tunnel: %q", got)
	}
	if got := clientProblem("degraded", "x", "10.0.0.2", "", true); got != "x" {
		t.Errorf("a tunnel with an address keeps the test message, got %q", got)
	}
	if got := clientProblem("down", "x", "", "The provider refused the username and password", false); !strings.Contains(got, "refused") {
		t.Errorf("OpenVPN's own reason must win: %q", got)
	}
	// OpenVPN says it is up, yet the device has no address: say that, not "never finished".
	if got := clientProblem("degraded", "x", "", "", true); !strings.Contains(got, "tunnel is up") || !strings.Contains(got, "took it away") {
		t.Errorf("up but no address: %q", got)
	}
}

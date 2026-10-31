package app

import (
	"strings"
	"testing"
)

func TestClientProblemSaysWhyInPlainWords(t *testing.T) {
	if got := clientProblem("degraded", "Connected, but traffic is not getting through (test to 8.8.8.8 failed)", "", ""); !strings.Contains(got, "has not given this device an address") {
		t.Errorf("no-address tunnel: %q", got)
	}
	if got := clientProblem("degraded", "x", "10.0.0.2", ""); got != "x" {
		t.Errorf("a tunnel with an address keeps the test message, got %q", got)
	}
	if got := clientProblem("down", "x", "", "The provider refused the username and password"); !strings.Contains(got, "refused") {
		t.Errorf("OpenVPN's own reason must win: %q", got)
	}
}

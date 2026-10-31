package app

import (
	"strings"
	"testing"

	"gatehouse/internal/netd"
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

func TestClientSettlingHidesNoiseWhileConnecting(t *testing.T) {
	log := []string{"a", "b"}
	st, msg, l := clientSettling("down", "x", "", netd.OVPNStatus{Active: true, Connecting: true, Log: log}, 0, 1000)
	if st != "connecting" || msg != "Connecting" || l != nil {
		t.Fatalf("starting: %s %q %v", st, msg, l)
	}
	st, msg, l = clientSettling("degraded", "x", "", netd.OVPNStatus{Active: true, Connected: true, ConnectedAt: 990, Log: log}, 900, 1000)
	if st != "connecting" || l != nil || msg == "x" {
		t.Fatalf("just connected: %s %q %v", st, msg, l)
	}
	// the same situation a minute later is a real failure and shows the log
	st, _, l = clientSettling("degraded", "x", "10.0.0.2", netd.OVPNStatus{Active: true, Connected: true, ConnectedAt: 990, Log: log}, 900, 1100)
	if st != "degraded" || len(l) != 2 {
		t.Fatalf("stale: %s %v", st, l)
	}
	st, msg, l = clientSettling("down", "x", "", netd.OVPNStatus{Active: true, Connecting: true, Reason: "The login was refused", Log: log}, 0, 1000)
	if st != "down" || msg != "The login was refused" || len(l) != 2 {
		t.Fatalf("refused: %s %q %v", st, msg, l)
	}
}

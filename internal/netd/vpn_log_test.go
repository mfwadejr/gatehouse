package netd

import "testing"

func TestOVPNConnectingWhileStarting(t *testing.T) {
	starting := []string{"OpenVPN 2.6.14 starting", "UDPv4 link remote: [AF_INET]1.2.3.4:1195"}
	if !OVPNConnecting(starting) {
		t.Fatal("a tunnel that is only starting should count as connecting")
	}
	done := append(append([]string{}, starting...), "Initialization Sequence Completed")
	if OVPNConnecting(done) {
		t.Fatal("a finished connection is not connecting")
	}
	failed := append(append([]string{}, starting...), "AUTH: Received control message: AUTH_FAILED")
	if OVPNConnecting(failed) {
		t.Fatal("a refused login is a failure, not connecting")
	}
	again := append(append([]string{}, done...), "SIGUSR1[soft,ping-restart] received, process restarting")
	if !OVPNConnecting(again) {
		t.Fatal("a restart after a connection is connecting again")
	}
	if OVPNConnecting(nil) {
		t.Fatal("no lines means nothing is known")
	}
}

func TestParseJournalKeepsTimes(t *testing.T) {
	out := "-- Logs begin --\n1761800000.123456 deb openvpn[99]: OpenVPN 2.6.14 starting\n1761800004.5 deb openvpn[99]: Initialization Sequence Completed\nplain line\n"
	msgs, at := parseJournal(out)
	if len(msgs) != 3 || msgs[0] != "OpenVPN 2.6.14 starting" || at[0] != 1761800000 || at[2] != 0 {
		t.Fatalf("got %q %v", msgs, at)
	}
	if got := connectedAt(msgs, at); got != 1761800004 {
		t.Fatalf("connectedAt = %d", got)
	}
	if connectedAt([]string{"x"}, []int64{5}) != 0 {
		t.Fatal("no completion should give 0")
	}
}

package app

import (
	"strings"
	"testing"
)

// A port that was given a name reads "Name (enp2s0)" in the sentences made on the server, and an
// unnamed port reads as its plain device name.
func TestPortNameInSentences(t *testing.T) {
	a, _ := newSpeedApp(t)
	if got := a.portName("enp2s0"); got != "enp2s0" {
		t.Fatalf("unnamed port = %q", got)
	}
	if err := a.Config.SetSetting(portLabelKey, `{"enp2s0":"Management"}`); err != nil {
		t.Fatal(err)
	}
	if got := a.portName("enp2s0"); got != "Management (enp2s0)" {
		t.Fatalf("named port = %q", got)
	}
	if got := strings.Join(a.portNameList([]string{"enp2s0", "enp3s0"}), ", "); got != "Management (enp2s0), enp3s0" {
		t.Fatalf("list = %q", got)
	}
}

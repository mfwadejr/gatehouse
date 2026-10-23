package installer

import (
	"strings"
	"testing"
	"time"
)

func TestScreenStates(t *testing.T) {
	cases := []struct {
		name  string
		ports []Port
		urls  []string
		want  []string
		not   []string
	}{
		{"no ports", nil, nil, []string{"No network port found", "USB"}, []string{"Installer code"}},
		{"no cable", []Port{{Name: "enp1s0"}, {Name: "enx1", USB: true}}, nil,
			[]string{"Connect your network cable to continue", "internet cable", "optional", "enp1s0", "enx1 (USB)"}, []string{"Installer code"}},
		{"cable no address", []Port{{Name: "enp1s0", Link: true}}, nil, []string{"Getting a network address"}, []string{"Installer code"}},
		{"ready", []Port{{Name: "enp1s0", Link: true, Speed: "1000"}}, []string{"https://192.168.1.20"},
			[]string{"https://192.168.1.20", "1234-5678", "1000 Mb/s"}, []string{"Connect your network cable"}},
	}
	for _, c := range cases {
		out := RenderScreen(c.ports, c.urls, "1234-5678", 80, 24)
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q", c.name, w)
			}
		}
		for _, w := range c.not {
			if strings.Contains(out, w) {
				t.Errorf("%s: should not show %q", c.name, w)
			}
		}
	}
}

func TestRenderStatus(t *testing.T) {
	ready := RenderStatus("gatehouse", []Port{{Name: "enp1s0", Link: true, Speed: "1000"}}, []string{"https://192.168.1.1"}, "ready", 80, 24)
	for _, want := range []string{"G A T E H O U S E", "Gatehouse is running", "https://192.168.1.1", "enp1s0", "1000 Mb/s"} {
		if !strings.Contains(ready, want) {
			t.Errorf("status screen lacks %q", want)
		}
	}
	if strings.Contains(strings.ToLower(ready), "debian") || strings.Contains(strings.ToLower(ready), "login") {
		t.Error("status screen names another product or asks for a login")
	}
	account := RenderStatus("gatehouse", []Port{{Name: "enp1s0", Link: true}}, []string{"https://192.168.1.20"}, "account", 80, 24)
	for _, want := range []string{"Setup not finished", "create your administrator account", "https://192.168.1.20"} {
		if !strings.Contains(account, want) {
			t.Errorf("first-start screen lacks %q", want)
		}
	}
	waiting := RenderStatus("gatehouse", nil, nil, "ready", 80, 24)
	if !strings.Contains(waiting, "Waiting for a network connection") {
		t.Error("no waiting message")
	}
}

func TestEnsureLinksUpRetriesSlowly(t *testing.T) {
	var calls []string
	old := bringUp
	bringUp = func(n string) { calls = append(calls, n) }
	defer func() { bringUp = old }()
	linkTried = map[string]time.Time{}
	// Whatever ports this machine has, a second call right away must not repeat the first.
	EnsureLinksUp()
	n := len(calls)
	EnsureLinksUp()
	if len(calls) != n {
		t.Fatalf("ports were poked again immediately: %v", calls)
	}
}

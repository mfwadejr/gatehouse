package installer

import (
	"os"
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

func TestStatusSaysWhenTheConsoleIsDown(t *testing.T) {
	p := []Port{{Name: "enp1s0", Link: true}}
	down := RenderStatus("gatehouse", p, []string{"https://192.168.1.20"}, "down", 80, 24)
	if !strings.Contains(down, "The console is not running") || strings.Contains(down, "https://192.168.1.20") {
		t.Fatalf("a dead console must not be offered as an address:\n%s", down)
	}
	if st := RenderStatus("gatehouse", p, []string{"https://192.168.1.20"}, "starting", 80, 24); !strings.Contains(st, "is starting") {
		t.Fatalf("no start-up message:\n%s", st)
	}
}

func TestLoopingAndPort(t *testing.T) {
	out := RenderStatus("gatehouse", []Port{{Name: "enp1s0", Link: true}}, []string{"https://192.168.1.1:8443"}, "looping", 80, 24)
	for _, w := range []string{"keeps stopping", "still protecting", "Alt+F2"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q", w)
		}
	}
	ConsolePortFile = t.TempDir() + "/console.port"
	if got := ReadConsolePort(""); got != "443" {
		t.Fatalf("default %s", got)
	}
	os.WriteFile(ConsolePortFile, []byte("8443\n"), 0o644)
	if got := ReadConsolePort(""); got != "8443" {
		t.Fatalf("saved %s", got)
	}
}

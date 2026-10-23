package installer

import (
	"strings"
	"testing"
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
			[]string{"Connect a network cable to continue", "internet", "both", "no cable", "enx1 (USB)"}, []string{"Installer code"}},
		{"cable no address", []Port{{Name: "enp1s0", Link: true}}, nil, []string{"Getting a network address"}, []string{"Installer code"}},
		{"ready", []Port{{Name: "enp1s0", Link: true, Speed: "1000"}}, []string{"https://192.168.1.20"},
			[]string{"https://192.168.1.20", "1234-5678", "cable connected, 1000 Mb/s"}, []string{"Connect a network cable"}},
	}
	for _, c := range cases {
		out := RenderScreen(c.ports, c.urls, "1234-5678")
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

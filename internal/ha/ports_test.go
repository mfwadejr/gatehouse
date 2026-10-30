package ha

import (
	"strings"
	"testing"
)

func TestMapPortsRefusesFewerPorts(t *testing.T) {
	roles := []PortRole{{ID: "internet:A", Label: "Internet A", Port: "enp1s0", Speed: 1000}, {ID: "network:Home", Label: "Home", Port: "enp2s0", Speed: 1000}}
	_, err := MapPorts(roles, []Port{{Name: "eth0", Speed: 1000, Link: true}, {Name: "wlan0", Wireless: true}}, nil)
	if err == nil || !strings.Contains(err.Error(), "at least as many") {
		t.Fatalf("a backup with fewer wired ports must be refused in words: %v", err)
	}
	if _, err := MapPorts(nil, nil, nil); err == nil {
		t.Fatal("no roles")
	}
}

func TestMapPortsMatchesFastToFast(t *testing.T) {
	roles := []PortRole{{ID: "network:Home", Label: "Home", Port: "enp2s0", Speed: 1000}, {ID: "internet:A", Label: "Internet A", Port: "enp1s0", Speed: 2500}}
	back := []Port{{Name: "eth0", Speed: 1000, Link: true}, {Name: "eth1", Speed: 2500, Link: true}, {Name: "eth2", Speed: 1000, Link: true}}
	ms, err := MapPorts(roles, back, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range ms {
		got[m.Role] = m.Backup
		if m.Warning != "" {
			t.Errorf("no warning expected: %+v", m)
		}
	}
	if got["internet:A"] != "eth1" || got["network:Home"] != "eth0" {
		t.Fatalf("%v", got)
	}
	if len(ms) != 2 || ms[0].Role != "network:Home" {
		t.Fatal("the result must keep the primary's order")
	}
}

func TestMapPortsWarnings(t *testing.T) {
	roles := []PortRole{{ID: "internet:A", Label: "Internet A", Port: "enp1s0", Speed: 1000}, {ID: "network:Home", Label: "Home", Port: "enp2s0", Speed: 1000}, {ID: "network:Cams", Label: "Cameras", Port: "enp3s0", Speed: 100}}
	back := []Port{{Name: "a", Speed: 100, Link: true}, {Name: "b", Speed: -1}, {Name: "c", Speed: 1000, Link: true}}
	ms, err := MapPorts(roles, back, nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Match{}
	for _, m := range ms {
		by[m.Role] = m
	}
	if by["internet:A"].Backup != "c" || by["internet:A"].Warning != "" {
		t.Fatalf("%+v", by["internet:A"])
	}
	if !strings.Contains(by["network:Home"].Warning, "slower") || by["network:Home"].Backup != "a" {
		t.Fatalf("a slower port needs a plain warning: %+v", by["network:Home"])
	}
	if !strings.Contains(by["network:Cams"].Warning, "No cable") {
		t.Fatalf("an unplugged port needs a warning: %+v", by["network:Cams"])
	}
}

func TestMapPortsHandChoices(t *testing.T) {
	roles := []PortRole{{ID: "x", Label: "X", Port: "p1", Speed: 1000}, {ID: "y", Label: "Y", Port: "p2", Speed: 1000}}
	back := []Port{{Name: "a", Speed: 1000, Link: true}, {Name: "b", Speed: 1000, Link: true}}
	ms, err := MapPorts(roles, back, map[string]string{"x": "b"})
	if err != nil || ms[0].Backup != "b" || ms[1].Backup != "a" {
		t.Fatalf("%+v %v", ms, err)
	}
	if _, err := MapPorts(roles, back, map[string]string{"x": "b", "y": "b"}); err == nil {
		t.Fatal("one port chosen twice")
	}
	if _, err := MapPorts(roles, back, map[string]string{"x": "nope"}); err == nil {
		t.Fatal("a port that does not exist")
	}
	if Rename("p1", ms) != "b" || Rename("zz", ms) != "zz" {
		t.Fatal("rename")
	}
}

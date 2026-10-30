package bridgemode

import (
	"strings"
	"testing"
)

func good() Config {
	return Config{Enabled: true, Outside: "enp1s0", Inside: []string{"enp2s0"}, Mode: "static", Addr: "192.168.1.2/24", Gateway: "192.168.1.1"}
}

func TestValidate(t *testing.T) {
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Config){
		"no inside":           func(c *Config) { c.Inside = nil },
		"same port twice":     func(c *Config) { c.Inside = []string{"enp1s0"} },
		"bad address":         func(c *Config) { c.Addr = "192.168.1.2" },
		"gateway outside net": func(c *Config) { c.Gateway = "10.0.0.1" },
		"no gateway":          func(c *Config) { c.Gateway = "" },
		"bad mode":            func(c *Config) { c.Mode = "x" },
		"bad subnet":          func(c *Config) { c.Subnets = []string{"nope"} },
		"oob without address": func(c *Config) { c.OOB = "enp3s0" },
		"oob overlaps":        func(c *Config) { c.OOB, c.OOBAddr = "enp3s0", "192.168.1.9/24" },
		"bad allow":           func(c *Config) { c.Allow = []Allow{{Name: "x", Proto: "icmp", Port: 0, To: "x"}} },
		"quote in name":       func(c *Config) { c.Allow = []Allow{{Name: "a\"b", Proto: "tcp", Port: 80, To: "192.168.1.5"}} },
	}
	for name, f := range bad {
		c := good()
		f(&c)
		if c.Validate() == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
	d := good()
	d.Mode, d.Addr, d.Gateway = "dhcp", "", ""
	if err := d.Validate(); err != nil {
		t.Errorf("an automatic address needs no gateway: %v", err)
	}
	off := good()
	off.Enabled, off.Inside = false, nil
	if err := off.Validate(); err != nil {
		t.Errorf("a switched-off setup is not checked: %v", err)
	}
}

func TestCheckPorts(t *testing.T) {
	have := []PortInfo{{Name: "enp1s0"}, {Name: "enp2s0"}, {Name: "enp3s0"}, {Name: "wlp4s0", Wireless: true}}
	c := good()
	if _, err := c.CheckPorts(have, nil); err != nil {
		t.Fatal(err)
	}
	c.Inside = []string{"missing"}
	if _, err := c.CheckPorts(have, nil); err == nil {
		t.Error("a port that does not exist must be refused")
	}
	c.Inside = []string{"wlp4s0"}
	if _, err := c.CheckPorts(have, nil); err == nil || !strings.Contains(err.Error(), "wired") {
		t.Error("a Wi-Fi card must be refused with a plain reason")
	}
	c = good()
	w, _ := c.CheckPorts(have, []string{"enp2s0"})
	for _, x := range w {
		if strings.Contains(x, "managing from") {
			t.Errorf("managing from an inside port is fine: %s", x)
		}
	}
	w, _ = c.CheckPorts(have, []string{"enp1s0"})
	if !strings.Contains(strings.Join(w, " "), "face your other router") {
		t.Errorf("managing from the outside port needs a warning: %v", w)
	}
	w, _ = c.CheckPorts(have, []string{"enp3s0"})
	if !strings.Contains(strings.Join(w, " "), "not part of") {
		t.Errorf("managing from an unrelated port needs a warning: %v", w)
	}
	c.OOB, c.OOBAddr = "enp3s0", "192.168.99.1/24"
	w, _ = c.CheckPorts(have, []string{"enp3s0"})
	for _, x := range w {
		if strings.Contains(x, "managing from") || strings.Contains(x, "No management-only") {
			t.Errorf("the management-only port is the safe way in: %s", x)
		}
	}
}

func TestInterfaces(t *testing.T) {
	c := good()
	c.OOB, c.OOBAddr = "enp3s0", "192.168.99.1/24"
	ifs := c.Interfaces()
	if len(ifs) != 2 || ifs[0].Name != Name || ifs[0].Kind != "bridge" || len(ifs[0].Members) != 2 || ifs[0].IPv4 != "192.168.1.2/24" {
		t.Fatalf("%+v", ifs)
	}
	if ifs[1].Name != "enp3s0" || ifs[1].Kind != "phys" {
		t.Fatalf("%+v", ifs[1])
	}
	for _, m := range ifs[0].Members {
		if m == "enp3s0" {
			t.Fatal("the management-only port must never join the bridge")
		}
	}
	d := good()
	d.Mode = "dhcp"
	if i := d.Interfaces()[0]; !i.DHCP || i.IPv4 != "" {
		t.Fatalf("%+v", i)
	}
}

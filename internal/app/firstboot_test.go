package app

import (
	"context"
	"testing"
	"time"

	"gatehouse/internal/seed"
)

// The network chosen in the installer is applied by itself on the first start.
func TestFirstBootAppliesInstallerChoices(t *testing.T) {
	web, dnsAddr := freeAddr(t), freeAddr(t)
	fn, ff := &fakeNet{}, &fakeFW{}
	a, err := New(Config{DataDir: t.TempDir(), WebAddr: web, DNSAddrs: []string{dnsAddr}, Net: fn, FW: ff})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	err = seed.Apply(a.Config, a.Box, &seed.Network{
		LAN: seed.LAN{Port: "enp2s0", Mode: "router", Address: "192.168.1.1/24"},
		WAN: &seed.WAN{Port: "enp1s0", Type: "dhcp"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()

	for i := 0; i < 100 && a.Config.Setting("net.autoapply", "") == "1"; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if a.Config.Setting("net.autoapply", "") != "0" {
		t.Fatal("the installer's choices were not applied")
	}
	fn.mu.Lock()
	defer fn.mu.Unlock()
	if len(fn.plan.Config.Interfaces) != 1 || fn.plan.Config.Interfaces[0].IPv4 != "192.168.1.1/24" || len(fn.plan.Config.WANs) != 1 {
		t.Fatalf("plan: %+v", fn.plan.Config)
	}
	if fn.pending || ff.pending {
		t.Fatal("first-boot changes must be confirmed, not left waiting")
	}
	if ff.script == "" {
		t.Fatal("no firewall was loaded")
	}
	if a.lastApplied() == "" {
		t.Fatal("the loaded firewall should count as confirmed")
	}
}

package app

import (
	"testing"
	"time"
)

func TestTrafficRates(t *testing.T) {
	var tr wanTraffic
	ctr := map[string]uint64{"eth0/rx_bytes": 1000, "eth0/tx_bytes": 500}
	read := func(dev, n string) (uint64, bool) { v, ok := ctr[dev+"/"+n]; return v, ok }
	t0 := time.Unix(1000, 0)
	devs := map[string]string{"Fibre": "eth0"}
	tr.sample(devs, t0, read)
	ctr["eth0/rx_bytes"], ctr["eth0/tx_bytes"] = 1000+375000, 500+125000
	tr.sample(devs, t0.Add(3*time.Second), read)
	s := tr.snapshot()["Fibre"]
	if len(s.Rx) != 2 || s.Rx[0] != 0 || s.Rx[1] != 1_000_000 || s.Tx[1] != 333333.3333333333 {
		t.Fatalf("rates: %+v", s)
	}
	// A counter reset gives a zero, not a huge number.
	ctr["eth0/rx_bytes"], ctr["eth0/tx_bytes"] = 10, 10
	tr.sample(devs, t0.Add(6*time.Second), read)
	if s = tr.snapshot()["Fibre"]; s.Rx[2] != 0 {
		t.Fatalf("reset: %+v", s)
	}
	// A device that is not there records zero; a connection that was removed disappears.
	tr.sample(map[string]string{"Fibre": "eth9"}, t0.Add(9*time.Second), read)
	if s = tr.snapshot()["Fibre"]; s.Dev != "eth9" || len(s.Rx) != 1 || s.Rx[0] != 0 {
		t.Fatalf("missing dev: %+v", s)
	}
	tr.sample(nil, t0.Add(12*time.Second), read)
	if len(tr.snapshot()) != 0 {
		t.Fatal("removed link kept")
	}
}

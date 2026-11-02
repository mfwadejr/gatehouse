package app

import (
	"testing"
	"time"
)

func TestTrafficTail(t *testing.T) {
	var tr wanTraffic
	n := uint64(0)
	read := func(dev, name string) (uint64, bool) { n += 1000; return n, true }
	now := time.Now()
	for i := 0; i < 10; i++ {
		tr.sample(map[string]string{"A": "eth0"}, now.Add(time.Duration(i)*time.Second), read)
	}
	l, seq := tr.tail(3)
	if seq != 10 || len(l["A"].Rx) != 3 || len(l["A"].Tx) != 3 {
		t.Fatalf("tail: seq %d, %d samples", seq, len(l["A"].Rx))
	}
	if l, _ := tr.tail(0); len(l["A"].Rx) != 10 {
		t.Fatalf("full: %d", len(l["A"].Rx))
	}
}

func TestTrafficLatestSwapsNothingUntilServed(t *testing.T) {
	var tr wanTraffic
	var rx, tx uint64
	read := func(dev, name string) (uint64, bool) {
		if name == "rx_bytes" {
			rx += 1000
			return rx, true
		}
		tx += 4000
		return tx, true
	}
	now := time.Now()
	tr.sample(map[string]string{"LAN": "LAN"}, now, read)
	tr.sample(map[string]string{"LAN": "LAN"}, now.Add(time.Second), read)
	v := tr.latest()["LAN"]
	if v[0] != 8000 || v[1] != 32000 {
		t.Fatalf("latest rx/tx = %v", v)
	}
}

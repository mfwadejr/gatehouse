package events

import (
	"testing"
	"time"
)

func TestSnapshotOnSubscribeAndFanout(t *testing.T) {
	b := NewBus()
	b.Publish("dns.stats", map[string]int{"qps": 5})
	ch, cancel := b.Subscribe("dns.stats", "fw.stats")
	defer cancel()
	select {
	case m := <-ch:
		if m.Topic != "dns.stats" {
			t.Fatalf("topic %s", m.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("no snapshot")
	}
	b.Publish("fw.stats", 1)
	b.Publish("other", 1) // not subscribed
	select {
	case m := <-ch:
		if m.Topic != "fw.stats" {
			t.Fatalf("topic %s", m.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("no fanout")
	}
	select {
	case m := <-ch:
		t.Fatalf("unexpected %v", m)
	default:
	}
}

func TestSlowConsumerKeepsNewest(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe("t")
	defer cancel()
	for i := 0; i < 100; i++ {
		b.Publish("t", i)
	}
	var last Message
	for len(ch) > 0 {
		last = <-ch
	}
	if string(last.Data) != "99" {
		t.Fatalf("newest lost: %s", last.Data)
	}
}

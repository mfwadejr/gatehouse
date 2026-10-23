package netd

import (
	"testing"
	"time"
)

func TestParseRealKernelLine(t *testing.T) {
	// A record exactly as /dev/kmsg returned it for a Gatehouse log rule.
	rec := "4,465,478930341,-;gh|allow|test:x IN= OUT=lo SRC=127.0.0.1 DST=127.0.0.1 LEN=31 TOS=0x00 PREC=0x00 TTL=64 ID=17787 DF PROTO=UDP SPT=54529 DPT=9999 LEN=11 "
	ev, ok := ParseKmsg(rec, time.Unix(5, 0))
	if !ok || ev.Verdict != "allow" || ev.Tag != "test:x" || ev.Out != "lo" || ev.DPort != 9999 || ev.SPort != 54529 || ev.Proto != "udp" || ev.TS != 5 {
		t.Fatalf("%+v %v", ev, ok)
	}
}

func TestRingKeepsNewestAndSequences(t *testing.T) {
	l := NewFWLog()
	for i := 0; i < fwRing+10; i++ {
		l.add(FWEvent{Verdict: "allow"})
	}
	got, last := l.Since(0, 0)
	if len(got) != fwRing || last != uint64(fwRing+10) || got[len(got)-1].Seq != last {
		t.Fatalf("ring: %d events, last %d", len(got), last)
	}
	more, _ := l.Since(last-3, 0)
	if len(more) != 3 {
		t.Fatalf("since: %d", len(more))
	}
}

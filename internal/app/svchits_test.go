package app

import (
	"testing"

	"nopewarden/internal/fw"
)

func TestServiceHitsPerEntry(t *testing.T) {
	ru := fw.RuleRow{Name: "Web", From: "Home", To: "wan", Service: "https,list:Mine"}
	tag := fwTag(ru)
	sp80, _ := fw.ParseService("tcp/80")
	hits := map[string]hitInfo{
		fw.ServiceTag(tag, sp80[0]): {Hits: 7, LastHit: 100},
	}
	got := serviceHits(ru, map[string][]string{"Mine": {"tcp/80"}}, tag, hits)
	if len(got) != 2 || got[0].Label != "https" || len(got[0].Ports) != 2 || got[1].Hits != 7 || got[1].LastHit != 100 {
		t.Fatalf("unexpected %+v", got)
	}
	if one := serviceHits(fw.RuleRow{Service: "tcp/22"}, nil, tag, hits); one != nil {
		t.Fatalf("single service should have no breakdown: %+v", one)
	}
}

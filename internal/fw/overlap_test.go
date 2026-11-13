package fw

import "testing"

func rule(id int64, name, action string) RuleRow {
	return RuleRow{ID: id, Name: name, From: "Home", To: "Internet", Src: "any", Dst: "any", Service: "any", Action: action, Enabled: true}
}

func TestOverlaps(t *testing.T) {
	a := rule(1, "Allow web", "accept")
	a.Service = "https,http"
	dup := rule(2, "Web again", "accept")
	dup.Service = "http,https"
	narrow := rule(3, "Just https", "accept")
	narrow.Service = "https"
	block := rule(4, "Block https", "drop")
	block.Service = "https"
	other := rule(5, "Other network", "accept")
	other.To = "Guest"
	other.Service = "https"
	got := FindOverlaps([]RuleRow{a, dup, narrow, block, other})
	if got[2].Kind != "duplicate" || got[3].Kind != "redundant" || got[4].Kind != "shadowed" {
		t.Fatalf("unexpected: %+v", got)
	}
	if _, ok := got[5]; ok {
		t.Fatal("a rule for another pair of networks must not be reported")
	}
	if _, ok := got[1]; ok {
		t.Fatal("the first rule has nothing above it")
	}
}

func TestOverlapsLeaveOutWhatCannotCover(t *testing.T) {
	limited := rule(1, "Limited", "accept")
	limited.Limit = 10
	paused := rule(2, "Paused", "accept")
	paused.PausedUntil = 99
	timed := rule(3, "Evenings", "drop")
	timed.Sched = &Schedule{Mode: "daily", From: "21:00", To: "07:00"}
	later := rule(4, "Later", "accept")
	if len(FindOverlaps([]RuleRow{limited, paused, timed, later})) != 0 {
		t.Fatal("a limited, paused or timed rule cannot hide a later one")
	}
	off := rule(5, "Off", "accept")
	off.Enabled = false
	again := rule(6, "Again", "accept")
	if len(FindOverlaps([]RuleRow{off, again})) != 0 {
		t.Fatal("a disabled rule hides nothing")
	}
	any1 := rule(7, "Everything", "accept")
	one := rule(8, "One", "accept")
	one.Src = "192.168.1.5"
	if FindOverlaps([]RuleRow{any1, one})[8].Kind != "redundant" {
		t.Fatal("a broader earlier rule makes a narrower one redundant")
	}
	if len(FindOverlaps([]RuleRow{one, any1})) != 0 {
		t.Fatal("a narrower earlier rule does not cover a broader one")
	}
}

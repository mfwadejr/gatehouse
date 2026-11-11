package fw

import (
	"strconv"
	"strings"
	"testing"
)

// A rule that lists a few services counts each one under its own label, and a rule with a single
// service keeps the plain label.
func TestPerServiceCounterLabels(t *testing.T) {
	m := sample()
	m.Rules = []Rule{
		{Name: "Web", From: "Home", To: "Guests", Service: "tcp/80,tcp/443,udp/53", Action: "accept", Enabled: true},
		{Name: "One", From: "Home", To: "Guests", Service: "tcp/22", Action: "accept", Enabled: true},
	}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`comment "rule:Home:Guests:Web#svc:tcp/80"`, `comment "rule:Home:Guests:Web#svc:tcp/443"`, `comment "rule:Home:Guests:Web#svc:udp/53"`, `comment "rule:Home:Guests:One"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `One#svc`) {
		t.Fatal("a single service must keep the plain label")
	}
}

// More services than MaxServiceCounters share the rule's own label.
func TestManyServicesShareTheRuleLabel(t *testing.T) {
	m := sample()
	var svc []string
	for i := 0; i <= MaxServiceCounters; i++ {
		svc = append(svc, "tcp/"+strconv.Itoa(1000+i))
	}
	m.Rules = []Rule{{Name: "Lots", From: "Home", To: "Guests", Service: strings.Join(svc, ","), Action: "accept", Enabled: true}}
	out, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Lots#svc") {
		t.Fatal("too many services should not get separate labels")
	}
}

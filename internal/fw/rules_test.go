package fw

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func richModel() *Model {
	m := sample()
	m.Devices = map[string]bool{"aa:bb:cc:dd:ee:01": true, "aa:bb:cc:dd:ee:02": true}
	m.DevGroups = map[string]DevGroup{"Kids": {MACs: []string{"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"}}}
	m.PortLists = map[string][]string{"Games": {"tcp/3074", "udp/3074", "udp/27000-27050"}}
	m.Loc = time.FixedZone("EST", -5*3600)
	m.Now = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	m.Rules = append(m.Rules,
		Rule{Name: "Kids no games", From: "Home", To: "Internet", Src: "grp:Kids", Service: "list:Games", Action: "drop", Enabled: true,
			Sched: `{"mode":"daily","from":"21:00","to":"07:00"}`, CtState: "new", Descr: "bedtime"},
		Rule{Name: "TV only", From: "Home", To: "Internet", Src: "dev:aa:bb:cc:dd:ee:01", Dst: "dom:example.com", Service: "web", Action: "accept", Enabled: true, AlsoMAC: true, IPVer: "4"},
		Rule{Name: "No Russia", From: "Home", To: "Internet", Dst: "geo:RU", Action: "drop", Enabled: true},
		Rule{Name: "v6 only", From: "Home", To: "Internet", Service: "ping", Action: "accept", Enabled: true, IPVer: "6"},
		Rule{Name: "Typed mac", From: "Home", To: "Internet", Src: "aa:bb:cc:dd:ee:09", Action: "accept", Enabled: true},
	)
	return m
}

func TestRichRulesCompileAndLoad(t *testing.T) {
	m := richModel()
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set gc_ru_4 { type ipv4_addr; flags interval; auto-merge; }",
		"set mv_",
		"ct state new",
		"meta day",
		"meta nfproto ipv4",
		"meta nfproto ipv6",
		"ether saddr aa:bb:cc:dd:ee:09",
		"udp dport 27000-27050",
		"ip daddr != { 10.0.0.0/8",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("missing %q in:\n%s", want, script)
		}
	}
	// 21:00-07:00 in UTC-5 is 02:00-12:00 UTC the next day: one window, one weekday per night.
	if !strings.Contains(script, `meta hour "02:00:00"-"11:59:59"`) {
		t.Errorf("schedule was not converted to UTC:\n%s", script)
	}
	if _, err := exec.LookPath("nft"); err != nil || os.Geteuid() != 0 {
		t.Skip("needs nft and root")
	}
	ap := NewApplier(t.TempDir())
	body, err := BuildSetScript(map[string]SetFill{
		SetNameFor(Ref{RefGeo, "RU"}, 4):                   {Replace: true, Elems: []string{"5.8.0.0/16", "31.13.0.0/20"}},
		SetNameFor(Ref{RefDomain, "example.com"}, 4):       {Replace: true, Elems: []string{"93.184.216.34 timeout 300s"}},
		SetNameFor(Ref{RefDevice, "aa:bb:cc:dd:ee:01"}, 4): {Replace: true, Elems: []string{"192.168.1.20"}},
		SetNameFor(Ref{RefDevice, "aa:bb:cc:dd:ee:01"}, 0): {Replace: true, Elems: []string{"aa:bb:cc:dd:ee:01"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ap.Check(script + "\n" + body); err != nil {
		t.Fatalf("nft refused the ruleset: %v\n%s\n%s", err, script, body)
	}
}

// Set contents come from DHCP leases and DNS answers, so they are checked before they reach nft.
func TestSetScriptRefusesAnythingButAddresses(t *testing.T) {
	for _, bad := range []string{"1.2.3.4 } ; flush ruleset ; add element inet gatehouse x {", "evil", "1.2.3.4 timeout 5s; drop", "aa:bb"} {
		if _, err := BuildSetScript(map[string]SetFill{"dv_abcd1234_4": {Elems: []string{bad}}}, nil); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := BuildSetScript(map[string]SetFill{"x; flush ruleset": {Elems: []string{"1.2.3.4"}}}, nil); err == nil {
		t.Error("accepted a bad set name")
	}
	// Names that do not exist in the loaded ruleset are skipped, not an error.
	out, err := BuildSetScript(map[string]SetFill{"dv_aaaaaaaa_4": {Elems: []string{"1.2.3.4"}}, "dv_bbbbbbbb_4": {Elems: []string{"1.2.3.5"}}}, map[string]bool{"dv_aaaaaaaa_4": true})
	if err != nil || strings.Contains(out, "dv_bbbbbbbb_4") || !strings.Contains(out, "dv_aaaaaaaa_4") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestRuleValidation(t *testing.T) {
	cases := map[string]func(*Model){
		"a device address (MAC) can only say where traffic comes from": func(m *Model) { m.Rules[0].Dst = "aa:bb:cc:dd:ee:09" },
		"does not exist":                  func(m *Model) { m.Rules[0].Src = "dev:aa:bb:cc:dd:ee:77" },
		"no device group called":          func(m *Model) { m.Rules[0].Src = "grp:Nobody" },
		"only be used as the destination": func(m *Model) { m.Rules[0].Src = "dom:example.com" },
		"IP version":                      func(m *Model) { m.Rules[0].IPVer = "5" },
		"no list of ports":                func(m *Model) { m.Rules[0].Service = "list:Nope" },
		"end must be after": func(m *Model) {
			m.Rules[0].Sched = `{"mode":"once","start":"2026-01-02T10:00","end":"2026-01-02T09:00"}`
		},
		"choose at least one day": func(m *Model) { m.Rules[0].Sched = `{"mode":"weekly","from":"08:00","to":"09:00"}` },
		"also require":            func(m *Model) { m.Rules[0].AlsoMAC = true },
		"not a domain name":       func(m *Model) { m.Rules[0].Dst = "dom:not_a_domain" },
		"not a country code":      func(m *Model) { m.Rules[0].Dst = "geo:USA" },
		"only work for devices on your own": func(m *Model) {
			m.Rules[0].From, m.Rules[0].Src, m.Rules[0].To = "Internet", "aa:bb:cc:dd:ee:09", "Home"
		},
	}
	for want, mut := range cases {
		m := richModel()
		m.Rules = []Rule{{Name: "R", From: "Guests", To: "Home", Action: "accept", Enabled: true, Service: "any"}}
		mut(m)
		err := m.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

func TestScheduleClauses(t *testing.T) {
	est := time.FixedZone("EST", -5*3600)
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	// Weekdays 09:00-17:00 EST is 14:00-22:00 UTC, same day.
	sc := &Schedule{Mode: "weekly", Days: []int{1, 2}, From: "09:00", To: "17:00"}
	got := sc.Clauses(est, now)
	if len(got) != 2 || got[0] != `meta day "Monday" meta hour "14:00:00"-"21:59:59"` {
		t.Fatalf("%q", got)
	}
	// A night window across midnight in UTC splits in two.
	night := &Schedule{Mode: "daily", From: "20:00", To: "08:00"}
	segs := night.Clauses(time.UTC, now)
	if len(segs) != 14 {
		t.Fatalf("expected 14 clauses (two per day), got %d: %q", len(segs), segs)
	}
	// Moving the clock forward changes the conversion: that is why summer time needs a new ruleset.
	edt := time.FixedZone("EDT", -4*3600)
	if sc.Clauses(edt, now)[0] == got[0] {
		t.Fatal("offset ignored")
	}
	once := &Schedule{Mode: "once", Start: "2026-02-01T08:00", End: "2026-02-01T09:00"}
	if c := once.Clauses(time.UTC, now); len(c) != 1 || !strings.Contains(c[0], "meta time >= ") {
		t.Fatalf("%q", c)
	}
	if err := (&Schedule{Mode: "daily", From: "8", To: "9"}).Check(time.UTC); err == nil {
		t.Fatal("accepted a bad time")
	}
}

func TestSplitAddrField(t *testing.T) {
	plain, macs, refs, err := SplitAddrField("10.0.0.0/8, dev:AA:BB:CC:DD:EE:01 grp:Kids dom:*.Example.com. geo:de aa:bb:cc:dd:ee:09")
	if err != nil || plain != "10.0.0.0/8" || len(macs) != 1 || len(refs) != 4 {
		t.Fatalf("%q %v %v %v", plain, macs, refs, err)
	}
	if refs[2].Val != "example.com" || refs[3].Val != "DE" || refs[0].Val != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("%v", refs)
	}
	if _, _, _, err := SplitAddrField("dev:nonsense"); err == nil {
		t.Fatal("accepted a bad device")
	}
}

// With real packets: a rule keyed to a set only matches while the address is in the set.
func TestSetsDriveRulesInLiveKernel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("needs unshare")
	}
	m := richModel()
	script, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ap := NewApplier(dir)
	ap.Netns = "ghsets"
	if out, err := exec.Command("ip", "netns", "add", "ghsets").CombinedOutput(); err != nil {
		t.Skipf("no network namespaces: %s", out)
	}
	defer exec.Command("ip", "netns", "del", "ghsets").Run()
	if err := ap.run(script, false); err != nil {
		t.Fatalf("%v\n%s", err, script)
	}
	set := SetNameFor(Ref{RefGroup, "Kids"}, 4)
	if err := ap.LoadSets(map[string]SetFill{set: {Replace: true, Elems: []string{"192.168.1.20", "192.168.1.21"}}, "dv_00000000_4": {Elems: []string{"1.1.1.1"}}}); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("ip", "netns", "exec", "ghsets", "nft", "list", "set", "inet", "gatehouse", set).CombinedOutput()
	if !strings.Contains(string(out), "192.168.1.20") || !strings.Contains(string(out), "192.168.1.21") {
		t.Fatalf("set not filled:\n%s", out)
	}
	// Replacing shrinks it: a device that left is no longer covered.
	ap.LoadSets(map[string]SetFill{set: {Replace: true, Elems: []string{"192.168.1.21"}}})
	out, _ = exec.Command("ip", "netns", "exec", "ghsets", "nft", "list", "set", "inet", "gatehouse", set).CombinedOutput()
	if strings.Contains(string(out), "192.168.1.20") {
		t.Fatalf("old address stayed:\n%s", out)
	}
	cs, err := ap.Counters()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cs["rule:Home:Internet:Kids no games"]; !ok {
		t.Fatalf("counters: %v", cs)
	}
}

func TestDescribeNamesDevicesAndGroups(t *testing.T) {
	m := richModel()
	m.Names = map[string]string{"aa:bb:cc:dd:ee:01": "Kitchen TV"}
	d := strings.Join(Describe(m), "\n")
	for _, want := range []string{
		`Exception "Kids no games": block the Games ports from the group Kids to the internet, every day, 21:00 to 07:00.`,
		`Exception "TV only": allow web from Kitchen TV to the internet (example.com and its subdomains).`,
		`Exception "No Russia": block everything to the internet (addresses in RU).`,
	} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q in:\n%s", want, d)
		}
	}
}

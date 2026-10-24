package ha

import (
	"strings"
	"testing"
)

func pair(t *testing.T) Pairing {
	t.Helper()
	s, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	return Pairing{V: 1, ID: "home", Secret: s, Primary: "192.168.1.2", Standby: "192.168.1.3",
		VIPs: []VIP{{"br0", "192.168.1.1/24"}, {"br1", "192.168.20.1/24"}}}
}

func TestCodeRoundTrip(t *testing.T) {
	p := pair(t)
	code, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(" " + code[:20] + "\n" + code[20:] + " ") // pasted with line breaks
	if err != nil || got.Secret != p.Secret || len(got.VIPs) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"", "hello", codePrefix + "!!!", codePrefix + "e30"} {
		if _, err := Decode(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]func(*Pairing){
		"same address":    func(p *Pairing) { p.Standby = p.Primary },
		"vip is the unit": func(p *Pairing) { p.VIPs[0].CIDR = "192.168.1.2/24" },
		"bad vip":         func(p *Pairing) { p.VIPs[0].CIDR = "nope" },
		"two on one port": func(p *Pairing) { p.VIPs[1].Iface = "br0" },
		"bad iface":       func(p *Pairing) { p.VIPs[0].Iface = "br0 {" },
		"no vips":         func(p *Pairing) { p.VIPs = nil },
		"short secret":    func(p *Pairing) { p.Secret = "abc" },
	}
	for name, mut := range cases {
		p := pair(t)
		mut(&p)
		if p.Validate() == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestKeepalived(t *testing.T) {
	p := pair(t)
	a, err := KeepalivedConf(p, Primary, "192.168.1.2")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := KeepalivedConf(p, Standby, "192.168.1.3")
	for _, want := range []string{"nopreempt", "advert_int 0.1", "state BACKUP", "192.168.1.1/24 dev br0", "unicast_peer {\n    192.168.1.3"} {
		if !strings.Contains(a, want) {
			t.Errorf("primary missing %q", want)
		}
	}
	if !strings.Contains(a, "priority 150") || !strings.Contains(b, "priority 100") || !strings.Contains(b, "unicast_src_ip 192.168.1.3") {
		t.Fatal("priorities or addresses wrong")
	}
	// both units must agree on each network's router id and password
	idsA := grabAll(a, "virtual_router_id ")
	idsB := grabAll(b, "virtual_router_id ")
	if strings.Join(idsA, ",") != strings.Join(idsB, ",") || len(idsA) != 2 || idsA[0] == idsA[1] {
		t.Fatalf("router ids: %v %v", idsA, idsB)
	}
	if strings.Join(grabAll(a, "auth_pass "), ",") != strings.Join(grabAll(b, "auth_pass "), ",") {
		t.Fatal("passwords differ")
	}
	if _, err := KeepalivedConf(p, Primary, "10.0.0.9"); err == nil {
		t.Fatal("wrong own address accepted")
	}
	if _, err := KeepalivedConf(p, Role("x"), ""); err == nil {
		t.Fatal("unknown role accepted")
	}
}

func grabAll(s, key string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, key) {
			out = append(out, strings.TrimPrefix(l, key))
		}
	}
	return out
}

func TestStandbyGuard(t *testing.T) {
	if !Allowed(Primary, "PUT", "/api/fw/rules") {
		t.Fatal("primary must allow changes")
	}
	for _, c := range [][3]interface{}{{"GET", "/api/fw/rules", true}, {"PUT", "/api/fw/rules", false}, {"POST", "/api/ha/failover", true},
		{"POST", "/api/system/restart", true}, {"DELETE", "/api/dhcp/leases/3", false}, {"POST", "/api/updates/install", true}} {
		if got := Allowed(Standby, c[0].(string), c[1].(string)); got != c[2].(bool) {
			t.Errorf("%v %v: got %v", c[0], c[1], got)
		}
	}
}

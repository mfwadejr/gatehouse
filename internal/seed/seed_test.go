package seed

import (
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"gatehouse/internal/crypt"
	"gatehouse/internal/fw"
	"gatehouse/internal/store"
)

func open(t *testing.T) (*store.DB, *crypt.Box) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenConfig(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypt.LoadOrCreate(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	return db, box
}

func TestSuggestAvoidsClashes(t *testing.T) {
	if got := Suggest(nil); got != "192.168.1.1/24" {
		t.Fatalf("default: %s", got)
	}
	used := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24"), netip.MustParsePrefix("192.168.50.77/24")}
	if got := Suggest(used); got != "10.10.10.1/24" {
		t.Fatalf("with clashes: %s", got)
	}
}

func TestRange(t *testing.T) {
	s, e := Range(netip.MustParsePrefix("192.168.1.1/24"))
	if s.String() != "192.168.1.100" || e.String() != "192.168.1.250" {
		t.Fatalf("/24: %s-%s", s, e)
	}
	s, e = Range(netip.MustParsePrefix("10.0.0.1/28"))
	if s.String() != "10.0.0.8" || e.String() != "10.0.0.14" {
		t.Fatalf("/28: %s-%s", s, e)
	}
}

func TestValidate(t *testing.T) {
	good := func() *Network {
		return &Network{LAN: LAN{Port: "enp2s0", Mode: "router", Address: "192.168.1.1/24"}}
	}
	cases := []struct {
		name string
		mod  func(*Network)
		want string
	}{
		{"fine", func(n *Network) {}, ""},
		{"no lan port", func(n *Network) { n.LAN.Port = "" }, "port for your local network"},
		{"public subnet", func(n *Network) { n.LAN.Address = "8.8.8.1/24" }, "private network"},
		{"too small", func(n *Network) { n.LAN.Address = "192.168.1.1/31" }, "between /16 and /29"},
		{"network address", func(n *Network) { n.LAN.Address = "192.168.1.0/24" }, "network address"},
		{"same port", func(n *Network) { n.WAN = &WAN{Port: "enp2s0", Type: "dhcp"} }, "different ports"},
		{"wan ok", func(n *Network) { n.WAN = &WAN{Port: "enp1s0", Type: "dhcp"} }, ""},
		{"pppoe needs login", func(n *Network) { n.WAN = &WAN{Port: "enp1s0", Type: "pppoe"} }, "user name and password"},
		{"bridge reuse", func(n *Network) { n.Bridge = []string{"enp2s0"} }, "already used"},
		{"mode missing", func(n *Network) { n.LAN.Mode = "" }, "how Gatehouse is used"},
		{"behind with wan", func(n *Network) {
			n.LAN = LAN{Port: "enp2s0", Mode: "behind", Addressing: "dhcp"}
			n.WAN = &WAN{Port: "enp1s0", Type: "dhcp"}
		}, "leave the internet port out"},
		{"behind static no gateway", func(n *Network) {
			n.LAN = LAN{Port: "enp2s0", Mode: "behind", Addressing: "static", Static: "192.168.0.50/24"}
		}, "router's address"},
		{"behind static ok", func(n *Network) {
			n.LAN = LAN{Port: "enp2s0", Mode: "behind", Addressing: "static", Static: "192.168.0.50/24", Gateway: "192.168.0.1"}
		}, ""},
	}
	for _, c := range cases {
		n := good()
		c.mod(n)
		err := n.Validate()
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

func TestApplyRouterWithBridgeAndWAN(t *testing.T) {
	db, box := open(t)
	n := &Network{LAN: LAN{Port: "enp2s0", Mode: "router", Address: "192.168.1.1/24"},
		WAN: &WAN{Port: "enp1s0", Type: "pppoe", PPPoEUser: "me@isp", PPPoEPass: "secret"}, Bridge: []string{"enp3s0"}}
	if err := Apply(db, box, n); err != nil {
		t.Fatal(err)
	}
	m, err := fw.Load(db)
	if err != nil {
		t.Fatal(err)
	}
	home, ok := m.ZoneByName("Home")
	if !ok || len(home.Ifaces) != 1 || home.Ifaces[0] != "br-lan" || !home.Gateway || !home.DNSForce {
		t.Fatalf("home zone: %+v", home)
	}
	if wan := m.WanIfaces(); len(wan) != 1 {
		t.Fatalf("wan ifaces: %v", wan)
	}
	if _, err := fw.Compile(m); err != nil {
		t.Fatal(err)
	}
	var kind, members, cidr string
	db.QueryRow(`SELECT kind,members,ipv4_cidr FROM interfaces WHERE name='br-lan'`).Scan(&kind, &members, &cidr)
	if kind != "bridge" || members != "enp2s0,enp3s0" || cidr != "192.168.1.1/24" {
		t.Fatalf("bridge row: %s %s %s", kind, members, cidr)
	}
	var start, end, router, subnet string
	db.QueryRow(`SELECT subnet,range_start,range_end,router FROM dhcp_scopes WHERE iface='br-lan'`).Scan(&subnet, &start, &end, &router)
	if subnet != "192.168.1.0/24" || start != "192.168.1.100" || end != "192.168.1.250" || router != "192.168.1.1" {
		t.Fatalf("scope: %s %s %s %s", subnet, start, end, router)
	}
	var sealed string
	db.QueryRow(`SELECT pppoe_pass FROM wan_links`).Scan(&sealed)
	if sealed == "" || strings.Contains(sealed, "secret") {
		t.Fatalf("the provider password must be stored sealed, got %q", sealed)
	}
	if db.Setting("net.autoapply", "") != "1" {
		t.Fatal("first boot must apply the network")
	}
}

func TestApplyBehindAnotherRouter(t *testing.T) {
	db, box := open(t)
	n := &Network{LAN: LAN{Port: "enp1s0", Mode: "behind", Addressing: "dhcp"}}
	if err := Apply(db, box, n); err != nil {
		t.Fatal(err)
	}
	m, err := fw.Load(db)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := m.ZoneByName("Home")
	if home == nil || home.Gateway || len(m.WanIfaces()) != 0 {
		t.Fatalf("behind mode should not be a gateway or have a WAN: %+v", home)
	}
	if _, err := fw.Compile(m); err != nil {
		t.Fatal(err)
	}
	var dhcp int
	var n2 int
	db.QueryRow(`SELECT dhcp FROM interfaces WHERE name='enp1s0'`).Scan(&dhcp)
	db.QueryRow(`SELECT count(*) FROM dhcp_scopes`).Scan(&n2)
	if dhcp != 1 || n2 != 0 {
		t.Fatalf("dhcp client %d, scopes %d", dhcp, n2)
	}
}

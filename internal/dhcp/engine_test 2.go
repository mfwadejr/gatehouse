package dhcp

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"gatehouse/internal/store"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	db, err := store.OpenConfig(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`INSERT INTO dhcp_scopes(id,name,iface,mode,subnet,range_start,range_end,router,dns,domain,lease_seconds)
		VALUES(1,'LAN','eth1','serve','192.168.1.0/24','192.168.1.100','192.168.1.102','192.168.1.1','192.168.1.1','lan',3600),
		      (2,'IoT','eth2','relay','10.0.5.0/24','10.0.5.10','10.0.5.20','10.0.5.1','','',600)`)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(db)
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	return e
}

var lan = Ingress{Iface: "eth1", ServerIP: net.ParseIP("192.168.1.1").To4()}

func pkt(typ byte, mac string, opts map[byte][]byte) *Packet {
	hw, _ := net.ParseMAC(mac)
	p := &Packet{Op: 1, HType: 1, HLen: 6, XID: 7, CHAddr: hw,
		CIAddr: net.IPv4zero.To4(), YIAddr: net.IPv4zero.To4(), SIAddr: net.IPv4zero.To4(), GIAddr: net.IPv4zero.To4()}
	p.Set(OptMsgType, []byte{typ})
	for k, v := range opts {
		p.Set(k, v)
	}
	return p
}

func dora(t *testing.T, e *Engine, in Ingress, mac string) *Packet {
	t.Helper()
	off, err := e.Handle(pkt(Discover, mac, map[byte][]byte{OptHostname: []byte("Laptop.local")}), in)
	if err != nil || off == nil || off.Type() != Offer {
		t.Fatalf("discover: %v %v", off, err)
	}
	req := pkt(Request, mac, map[byte][]byte{OptRequestedIP: off.YIAddr.To4(), OptServerID: off.Options[OptServerID]})
	ack, err := e.Handle(req, in)
	if err != nil || ack == nil {
		t.Fatalf("request: %v %v", ack, err)
	}
	return ack
}

func TestPacketRoundTrip(t *testing.T) {
	p := pkt(Discover, "aa:bb:cc:00:00:01", map[byte][]byte{OptHostname: []byte("x")})
	q, err := Parse(p.Marshal())
	if err != nil || q.Type() != Discover || q.CHAddr.String() != "aa:bb:cc:00:00:01" || string(q.Options[OptHostname]) != "x" {
		t.Fatalf("round trip: %+v %v", q, err)
	}
	if _, err := Parse(make([]byte, 100)); err == nil {
		t.Fatal("short packet accepted")
	}
	bad := p.Marshal()
	bad[236] = 0
	if _, err := Parse(bad); err == nil {
		t.Fatal("bad cookie accepted")
	}
	trunc := p.Marshal()[:241]
	trunc[240] = 12
	if _, err := Parse(append(trunc[:240:240], 12, 200)); err == nil {
		t.Fatal("truncated option accepted")
	}
}

func TestLeaseFlow(t *testing.T) {
	e := newEngine(t)
	ack := dora(t, e, lan, "aa:bb:cc:00:00:01")
	if ack.Type() != Ack || ack.YIAddr.String() != "192.168.1.100" {
		t.Fatalf("ack: %v %s", ack.Type(), ack.YIAddr)
	}
	if got := net.IP(ack.Options[OptSubnetMask]).String(); got != "255.255.255.0" {
		t.Fatalf("mask %s", got)
	}
	if net.IP(ack.Options[OptRouter]).String() != "192.168.1.1" || string(ack.Options[OptDomain]) != "lan" {
		t.Fatal("router/domain missing")
	}
	if lt := ack.Options[OptLeaseTime]; len(lt) != 4 || lt[2] != 0x0e || lt[3] != 0x10 {
		t.Fatalf("lease time %v", lt)
	}
	// Same device gets the same address again.
	ack2 := dora(t, e, lan, "aa:bb:cc:00:00:01")
	if !ack2.YIAddr.Equal(ack.YIAddr) {
		t.Fatalf("address changed: %s", ack2.YIAddr)
	}
	// A second device gets the next one, and the name appears for DNS.
	ack3 := dora(t, e, lan, "aa:bb:cc:00:00:02")
	if ack3.YIAddr.String() != "192.168.1.101" {
		t.Fatalf("second: %s", ack3.YIAddr)
	}
	if h, ok := e.Lookups().LookupAddr(net.ParseIP("192.168.1.100")); !ok || h != "laptop" {
		t.Fatalf("hostname not exposed to DNS: %q %v", h, ok)
	}
	if ips := e.Lookups().LookupHost("laptop"); len(ips) == 0 {
		t.Fatal("name lookup failed")
	}
}

func TestExhaustionAndRelease(t *testing.T) {
	e := newEngine(t)
	for i, m := range []string{"aa:00:00:00:00:01", "aa:00:00:00:00:02", "aa:00:00:00:00:03"} {
		if a := dora(t, e, lan, m); a.YIAddr.String() != u32ToIP(ipToU32(ip4("192.168.1.100"))+uint32(i)).String() {
			t.Fatalf("unexpected %s", a.YIAddr)
		}
	}
	off, err := e.Handle(pkt(Discover, "aa:00:00:00:00:04", nil), lan)
	if off != nil || err == nil {
		t.Fatalf("pool should be exhausted: %v %v", off, err)
	}
	rel := pkt(Release, "aa:00:00:00:00:01", nil)
	rel.CIAddr = ip4("192.168.1.100")
	e.Handle(rel, lan)
	if a := dora(t, e, lan, "aa:00:00:00:00:04"); a.YIAddr.String() != "192.168.1.100" {
		t.Fatalf("released address not reused: %s", a.YIAddr)
	}
}

func TestReservation(t *testing.T) {
	e := newEngine(t)
	e.db.Exec(`INSERT INTO dhcp_reservations(scope_id,mac,ip,hostname) VALUES(1,'AA:BB:CC:00:00:09','192.168.1.50','printer')`)
	e.Reload()
	a := dora(t, e, lan, "aa:bb:cc:00:00:09")
	if a.YIAddr.String() != "192.168.1.50" {
		t.Fatalf("reservation ignored: %s", a.YIAddr)
	}
	// Nobody else may take it, even by asking.
	p := pkt(Request, "aa:bb:cc:00:00:0a", map[byte][]byte{OptRequestedIP: ip4("192.168.1.50")})
	if r, _ := e.Handle(p, lan); r == nil || r.Type() != Nak {
		t.Fatal("reserved address handed to another device")
	}
	// The reserved device cannot take another address either.
	p = pkt(Request, "aa:bb:cc:00:00:09", map[byte][]byte{OptRequestedIP: ip4("192.168.1.101")})
	if r, _ := e.Handle(p, lan); r == nil || r.Type() != Nak {
		t.Fatal("reserved device accepted a different address")
	}
	if h, _ := e.Lookups().LookupAddr(ip4("192.168.1.50")); h != "printer" {
		t.Fatalf("reservation name: %q", h)
	}
}

func TestNakAndOtherServer(t *testing.T) {
	e := newEngine(t)
	p := pkt(Request, "aa:00:00:00:00:01", map[byte][]byte{OptRequestedIP: ip4("10.9.9.9")})
	if r, _ := e.Handle(p, lan); r == nil || r.Type() != Nak {
		t.Fatal("out of subnet request must be NAKed")
	}
	// The client chose some other server: stay silent and drop our offer.
	e.Handle(pkt(Discover, "aa:00:00:00:00:02", nil), lan)
	p = pkt(Request, "aa:00:00:00:00:02", map[byte][]byte{OptRequestedIP: ip4("192.168.1.100"), OptServerID: ip4("192.168.1.254")})
	if r, _ := e.Handle(p, lan); r != nil {
		t.Fatal("answered a request addressed to another server")
	}
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM dhcp_leases WHERE lower(mac)='aa:00:00:00:00:02'`).Scan(&n)
	if n != 0 {
		t.Fatal("offer not released")
	}
}

func TestDeclineQuarantinesAddress(t *testing.T) {
	e := newEngine(t)
	d := pkt(Decline, "aa:00:00:00:00:01", map[byte][]byte{OptRequestedIP: ip4("192.168.1.100")})
	e.Handle(d, lan)
	if a := dora(t, e, lan, "aa:00:00:00:00:02"); a.YIAddr.String() != "192.168.1.101" {
		t.Fatalf("declined address was offered: %s", a.YIAddr)
	}
}

func TestRelayAndModes(t *testing.T) {
	e := newEngine(t)
	// A relayed request is answered from the relay scope; the server address is the interface it arrived on.
	p := pkt(Discover, "aa:00:00:00:00:07", nil)
	p.GIAddr = ip4("10.0.5.1")
	off, err := e.Handle(p, Ingress{Iface: "eth1", ServerIP: ip4("192.168.1.1")})
	if err != nil || off == nil || off.YIAddr.String() != "10.0.5.10" {
		t.Fatalf("relay offer: %v %v", off, err)
	}
	if off.GIAddr.String() != "10.0.5.1" {
		t.Fatal("giaddr not echoed")
	}
	// A relay from an unknown subnet gets nothing.
	p = pkt(Discover, "aa:00:00:00:00:08", nil)
	p.GIAddr = ip4("172.16.0.1")
	if r, _ := e.Handle(p, lan); r != nil {
		t.Fatal("answered an unknown relay")
	}
	// Mode off and disabled scopes stay silent.
	e.db.Exec(`UPDATE dhcp_scopes SET mode='off' WHERE id=1`)
	e.Reload()
	if r, _ := e.Handle(pkt(Discover, "aa:00:00:00:00:09", nil), lan); r != nil {
		t.Fatal("answered while off")
	}
	// Traffic on an interface with no scope is ignored.
	if r, _ := e.Handle(pkt(Discover, "aa:00:00:00:00:09", nil), Ingress{Iface: "eth9", ServerIP: ip4("172.30.0.1")}); r != nil {
		t.Fatal("answered on an unconfigured interface")
	}
}

func TestExpiredLeaseReusable(t *testing.T) {
	e := newEngine(t)
	base := time.Now()
	e.now = func() time.Time { return base }
	dora(t, e, lan, "aa:00:00:00:00:01")
	e.now = func() time.Time { return base.Add(2 * time.Hour) }
	if a := dora(t, e, lan, "aa:00:00:00:00:02"); a.YIAddr.String() != "192.168.1.100" {
		t.Fatalf("expired lease not reclaimed: %s", a.YIAddr)
	}
}

func TestInformAndLeaseList(t *testing.T) {
	e := newEngine(t)
	dora(t, e, lan, "aa:00:00:00:00:01")
	p := pkt(Inform, "aa:00:00:00:00:05", nil)
	p.CIAddr = ip4("192.168.1.77")
	r, _ := e.Handle(p, lan)
	if r == nil || r.Type() != Ack || r.Options[OptLeaseTime] != nil {
		t.Fatal("inform reply wrong")
	}
	rows, err := e.Leases()
	if err != nil || len(rows) != 1 || rows[0].IP != "192.168.1.100" || rows[0].Hostname != "laptop" {
		t.Fatalf("leases: %+v %v", rows, err)
	}
	if u := e.Utilisation(); u[0].Size != 3 || u[0].Active != 1 {
		t.Fatalf("utilisation: %+v", u)
	}
	if err := e.Revoke(1, "192.168.1.100"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := e.Leases(); len(rows) != 0 {
		t.Fatal("revoke did not remove the lease")
	}
}

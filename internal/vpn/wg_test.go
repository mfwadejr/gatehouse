package vpn

import (
	"net/netip"
	"strings"
	"testing"
)

func TestKeys(t *testing.T) {
	priv, pub, err := NewKeyPair()
	if err != nil || !ValidKey(priv) || !ValidKey(pub) {
		t.Fatal("bad key pair", err)
	}
	d, err := PublicKey(priv)
	if err != nil || d != pub {
		t.Fatalf("derived %q want %q (%v)", d, pub, err)
	}
	if _, err := PublicKey("nope"); err == nil {
		t.Fatal("accepted junk")
	}
	// The pair published in the WireGuard documentation.
	if got, _ := PublicKey("yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="); got != "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=" {
		t.Fatalf("reference vector mismatch: %s", got)
	}
}

func TestSubnetAndAddresses(t *testing.T) {
	used := []netip.Prefix{netip.MustParsePrefix("10.8.0.0/16")}
	if s := SuggestSubnet(used); s != "10.9.0.0/24" && !strings.HasPrefix(s, "10.") {
		t.Fatal(s)
	}
	if s := SuggestSubnet(nil); s != "10.8.0.0/24" {
		t.Fatal(s)
	}
	a, err := ServerAddress("10.8.0.0/24")
	if err != nil || a != "10.8.0.1/24" {
		t.Fatal(a, err)
	}
	if _, err := ServerAddress("10.8.0.0/31"); err == nil {
		t.Fatal("tiny network accepted")
	}
	n, err := NextPeerAddress("10.8.0.1/24", nil)
	if err != nil || n != "10.8.0.2/32" {
		t.Fatal(n, err)
	}
	n, _ = NextPeerAddress("10.8.0.1/24", []string{"10.8.0.2/32", "10.8.0.3/32"})
	if n != "10.8.0.4/32" {
		t.Fatal(n)
	}
	// A full /29 has hosts .1-.6; .1 is the router.
	var taken []string
	for i := 2; i <= 6; i++ {
		taken = append(taken, "10.8.0."+string(rune('0'+i))+"/32")
	}
	if _, err := NextPeerAddress("10.8.0.1/29", taken); err == nil {
		t.Fatal("expected exhausted network")
	}
}

func TestClientConfigAndParse(t *testing.T) {
	priv, _, _ := NewKeyPair()
	_, spub, _ := NewKeyPair()
	psk, _ := NewPSK()
	c := ClientConfig{PrivateKey: priv, Address: "10.8.0.2/32", DNS: "10.8.0.1", ServerKey: spub, PSK: psk,
		Endpoint: "vpn.example.net:51820", AllowedIPs: []string{"0.0.0.0/0", "::/0"}, Keepalive: 25}
	txt := c.Text()
	im, err := ParseConf(txt)
	if err != nil {
		t.Fatal(err, txt)
	}
	if im.PrivateKey != priv || im.Address != "10.8.0.2/32" || im.DNS != "10.8.0.1" || im.PeerKey != spub || im.PSK != psk ||
		im.Endpoint != "vpn.example.net:51820" || len(im.AllowedIPs) != 2 || im.Keepalive != 25 {
		t.Fatalf("%+v", im)
	}
	if _, err := ParseConf("[Interface]\nPrivateKey = x\n"); err == nil {
		t.Fatal("junk accepted")
	}
	png, err := QR(txt)
	if err != nil || len(png) < 100 || string(png[1:4]) != "PNG" {
		t.Fatal("qr", err)
	}
	spec, full := SplitFull([]string{"0.0.0.0/0", "10.0.0.0/8"})
	if !full || len(spec) != 1 {
		t.Fatal(spec, full)
	}
}

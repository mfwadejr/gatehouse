package upnp

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func ip(s string) netip.Addr { return netip.MustParseAddr(s) }

func newTable() *Table {
	p := DefaultPolicy()
	p.Reserved = func(proto string, port int) bool { return port == 8443 }
	return NewTable(p)
}

func TestDeviceCanOnlyOpenPortsToItself(t *testing.T) {
	tb := newTable()
	if err := tb.Add(ip("192.168.1.20"), ip("192.168.1.30"), "tcp", 40000, 40000, "x", 0); err != ErrNotAuthorized {
		t.Fatalf("forwarding to another device allowed: %v", err)
	}
	if err := tb.Add(ip("192.168.1.20"), ip("192.168.1.20"), "tcp", 40000, 80, "game", time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyLimits(t *testing.T) {
	tb := newTable()
	me := ip("192.168.1.20")
	for _, port := range []int{22, 80, 443, 1023} {
		if err := tb.Add(me, me, "tcp", port, port, "x", 0); err != ErrNotAuthorized {
			t.Errorf("port %d allowed: %v", port, err)
		}
	}
	if err := tb.Add(me, me, "tcp", 8443, 8443, "x", 0); err != ErrNotAuthorized {
		t.Error("reserved port allowed")
	}
	if err := tb.Add(me, me, "icmp", 5000, 5000, "x", 0); err != ErrBadArgs {
		t.Error("bad protocol allowed")
	}
	for i := 0; i < 32; i++ {
		if err := tb.Add(me, me, "udp", 20000+i, 20000+i, "x", 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := tb.Add(me, me, "udp", 30000, 30000, "x", 0); err != ErrNoneLeft {
		t.Errorf("per-device cap not enforced: %v", err)
	}
}

func TestConflictAndOwnership(t *testing.T) {
	tb := newTable()
	a, b := ip("192.168.1.20"), ip("192.168.1.21")
	tb.Add(a, a, "tcp", 40000, 40000, "a", 0)
	if err := tb.Add(b, b, "tcp", 40000, 40000, "b", 0); err != ErrConflict {
		t.Fatalf("took another device's port: %v", err)
	}
	if err := tb.Delete(b, "tcp", 40000); err != ErrNotAuthorized {
		t.Fatalf("deleted another device's mapping: %v", err)
	}
	if err := tb.Delete(a, "tcp", 40000); err != nil {
		t.Fatal(err)
	}
	if err := tb.Delete(a, "tcp", 40000); err != ErrNoSuchEntry {
		t.Fatal(err)
	}
}

func TestLeaseCapAndExpiry(t *testing.T) {
	tb := newTable()
	now := time.Now()
	tb.now = func() time.Time { return now }
	me := ip("192.168.1.20")
	tb.Add(me, me, "tcp", 40000, 40000, "x", 1000*time.Hour)
	if m, _ := tb.Get("tcp", 40000); m.Expires.Sub(now) != 24*time.Hour {
		t.Fatalf("lease not capped: %v", m.Expires.Sub(now))
	}
	tb.Add(me, me, "tcp", 40001, 40001, "short", time.Minute)
	now = now.Add(2 * time.Minute)
	if _, ok := tb.Get("tcp", 40001); ok {
		t.Fatal("expired mapping still open")
	}
	if _, ok := tb.Get("tcp", 40000); !ok {
		t.Fatal("live mapping lost")
	}
}

func TestAllowlist(t *testing.T) {
	tb := newTable()
	tb.pol.Allowed = func(a netip.Addr) bool { return a == ip("192.168.1.20") }
	other := ip("192.168.1.21")
	if err := tb.Add(other, other, "tcp", 40000, 40000, "x", 0); err != ErrNotAuthorized {
		t.Fatal("device outside the allowlist was served")
	}
}

func TestOnChangeAndClear(t *testing.T) {
	tb := newTable()
	var last []Mapping
	tb.OnChange = func(m []Mapping) { last = m }
	me := ip("192.168.1.20")
	tb.Add(me, me, "tcp", 40000, 40000, "x", 0)
	if len(last) != 1 {
		t.Fatal("no change notification")
	}
	tb.Clear("test")
	if len(last) != 0 {
		t.Fatal("clear did not notify")
	}
}

func gw(tb *Table, ext string) *Gateway {
	return &Gateway{Table: tb, LAN: netip.MustParsePrefix("192.168.1.0/24"), ExtIP: func() string { return ext }, Name: "Gatehouse", UUID: "u"}
}

func soap(g *Gateway, from, action, body string) *httptest.ResponseRecorder {
	env := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:` + action + ` xmlns:u="` + svcType + `">` + body + `</u:` + action + `></s:Body></s:Envelope>`
	r := httptest.NewRequest("POST", "/ctl/IPConn", strings.NewReader(env))
	r.RemoteAddr = from + ":4000"
	r.Header.Set("SOAPAction", `"`+svcType+"#"+action+`"`)
	w := httptest.NewRecorder()
	g.Handler("http://x/").ServeHTTP(w, r)
	return w
}

const addBody = `<NewRemoteHost></NewRemoteHost><NewExternalPort>40000</NewExternalPort><NewProtocol>TCP</NewProtocol><NewInternalPort>40000</NewInternalPort><NewInternalClient>192.168.1.20</NewInternalClient><NewEnabled>1</NewEnabled><NewPortMappingDescription>Game</NewPortMappingDescription><NewLeaseDuration>3600</NewLeaseDuration>`

func TestSOAPRoundTrip(t *testing.T) {
	tb := newTable()
	g := gw(tb, "203.0.113.5")
	if w := soap(g, "192.168.1.20", "GetExternalIPAddress", ""); !strings.Contains(w.Body.String(), "203.0.113.5") {
		t.Fatalf("external address: %s", w.Body)
	}
	if w := soap(g, "192.168.1.20", "AddPortMapping", addBody); w.Code != 200 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	w := soap(g, "192.168.1.20", "GetGenericPortMappingEntry", `<NewPortMappingIndex>0</NewPortMappingIndex>`)
	if !strings.Contains(w.Body.String(), "<NewInternalClient>192.168.1.20</NewInternalClient>") || !strings.Contains(w.Body.String(), "Game") {
		t.Fatalf("list: %s", w.Body)
	}
	if w := soap(g, "192.168.1.20", "GetGenericPortMappingEntry", `<NewPortMappingIndex>1</NewPortMappingIndex>`); w.Code != 500 || !strings.Contains(w.Body.String(), "<errorCode>714</errorCode>") {
		t.Fatalf("end of list: %d %s", w.Code, w.Body)
	}
	if w := soap(g, "192.168.1.20", "DeletePortMapping", `<NewRemoteHost></NewRemoteHost><NewExternalPort>40000</NewExternalPort><NewProtocol>TCP</NewProtocol>`); w.Code != 200 {
		t.Fatalf("delete: %s", w.Body)
	}
	if len(tb.List()) != 0 {
		t.Fatal("mapping not removed")
	}
}

func TestSOAPRefusals(t *testing.T) {
	tb := newTable()
	g := gw(tb, "203.0.113.5")
	// From outside the LAN.
	if w := soap(g, "8.8.8.8", "AddPortMapping", addBody); w.Code != http.StatusForbidden {
		t.Fatalf("outside request served: %d", w.Code)
	}
	// Someone else's address as the target.
	if w := soap(g, "192.168.1.21", "AddPortMapping", addBody); w.Code != 500 || !strings.Contains(w.Body.String(), "606") {
		t.Fatalf("forwarding to another device: %d %s", w.Code, w.Body)
	}
	// A remote host restriction is not supported.
	b := strings.Replace(addBody, "<NewRemoteHost></NewRemoteHost>", "<NewRemoteHost>1.2.3.4</NewRemoteHost>", 1)
	if w := soap(g, "192.168.1.20", "AddPortMapping", b); w.Code != 500 {
		t.Fatal("remote-host mapping allowed")
	}
	// No internet: nothing is opened.
	if w := soap(gw(tb, ""), "192.168.1.20", "AddPortMapping", addBody); w.Code != 500 {
		t.Fatal("mapping opened with no internet")
	}
	// Garbage and unknown actions.
	if w := soap(g, "192.168.1.20", "Reboot", ""); w.Code != 500 || !strings.Contains(w.Body.String(), "401") {
		t.Fatalf("unknown action: %d", w.Code)
	}
	if len(tb.List()) != 0 {
		t.Fatal("something was opened")
	}
}

func TestSOAPRateLimit(t *testing.T) {
	g := gw(newTable(), "203.0.113.5")
	limited := false
	for i := 0; i < 200; i++ {
		if soap(g, "192.168.1.20", "GetExternalIPAddress", "").Code == 503 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("a flood of requests was not slowed down")
	}
}

func TestSSDP(t *testing.T) {
	pkt := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	st := searchTarget([]byte(pkt))
	if m := matches(st); len(m) != 1 {
		t.Fatalf("not matched: %q", st)
	}
	if matches("urn:schemas-upnp-org:device:MediaServer:1") != nil {
		t.Fatal("unrelated search answered")
	}
	if searchTarget([]byte("GET / HTTP/1.1\r\n\r\n")) != "" {
		t.Fatal("non-search accepted")
	}
	r := string(response(st, "http://192.168.1.1:5000/rootDesc.xml", "abc"))
	if !strings.Contains(r, "LOCATION: http://192.168.1.1:5000/rootDesc.xml") || !strings.Contains(r, "200 OK") {
		t.Fatal(r)
	}
}

func TestDescriptionIsWellFormed(t *testing.T) {
	g := gw(newTable(), "1.2.3.4")
	for _, p := range []string{"/rootDesc.xml", "/WANIPCn.xml"} {
		r := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		g.Handler("http://192.168.1.1:5000/").ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "WANIPConnection") && p == "/rootDesc.xml" {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
}

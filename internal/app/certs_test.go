package app

import (
	"testing"
	"time"

	"nopewarden/internal/vpn"
)

func TestCertCountdowns(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now()
	// A CA made nine years and eleven months ago has about a month left; a fresh one has years.
	old, err := vpn.NewCA(now.AddDate(-10, 0, 20))
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := old.ServerCert(now.AddDate(-10, 0, 20))
	fresh, _ := vpn.NewCA(now)
	a.Config.Exec(`INSERT INTO vpn_ovpn(id,name,label,mode,ca_cert,srv_cert,created_at) VALUES(1,'tun0','Office','server',?,?,1)`, old.CertPEM, srv.CertPEM)
	a.Config.Exec(`INSERT INTO vpn_ovpn(id,name,label,mode,ca_cert,srv_cert,created_at) VALUES(2,'tun1','Cabin','server',?,?,1)`, fresh.CertPEM, "")
	// A user certificate that ran out a week ago.
	gone, _ := old.ServerCert(now.AddDate(-10, 0, -7)) // any certificate: only its end date matters
	a.Config.Exec(`INSERT INTO vpn_ovpn_users(ovpn_id,name,serial,cert,created_at) VALUES(1,'Sam','1',?,1)`, gone.CertPEM)
	a.Config.Exec(`INSERT INTO vpn_ovpn_users(ovpn_id,name,serial,cert,created_at,revoked_at) VALUES(1,'Old phone','2',?,1,5)`, gone.CertPEM)

	got := map[string]certInfo{}
	for _, c := range a.certReport(now) {
		got[c.Label] = c
	}
	if c := got["Office: certificate authority"]; c.Level != "warn" || c.Days < 18 || c.Days > 21 {
		t.Fatalf("old CA = %+v", c)
	}
	if c := got["Office: Sam"]; c.Level != "bad" || c.Days >= 0 {
		t.Fatalf("expired user = %+v", c)
	}
	if _, ok := got["Office: Old phone"]; ok {
		t.Fatal("a revoked user should not be counted")
	}
	if c := got["Cabin: certificate authority"]; c.Level != "" || c.Days < 3600 {
		t.Fatalf("fresh CA = %+v", c)
	}
	for l, want := range map[int]string{-1: "bad", 0: "warn", 30: "warn", 31: "info", 60: "info", 61: ""} {
		if certLevel(l) != want {
			t.Errorf("level(%d) = %q want %q", l, certLevel(l), want)
		}
	}
	// The report is soonest first.
	r := a.certReport(now)
	for i := 1; i < len(r); i++ {
		if r[i].Expires < r[i-1].Expires {
			t.Fatal("not sorted by date")
		}
	}
}

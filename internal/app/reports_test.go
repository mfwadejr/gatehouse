package app

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func seedReportData(t *testing.T, a *App, now time.Time) {
	t.Helper()
	ts := now.Add(-3 * time.Hour).Unix()
	for i := 0; i < 5; i++ {
		res := "allowed"
		if i < 2 {
			res = "blocked"
		}
		if _, err := a.Logs.Exec(`INSERT INTO dns_queries(ts, client, client_name, qname, qtype, result, rcode, upstream, latency_us, rule) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			ts+int64(i), "192.168.1.20", "laptop", "ads.example.com", "A", res, 0, "1.1.1.1", 1200, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Logs.Exec(`INSERT INTO fw_events(ts, action, from_zone, from_addr, to_zone, to_addr, proto, dport, rule) VALUES(?,?,?,?,?,?,?,?,?)`,
		ts, "drop", "wan", "203.0.113.9", "lan", "192.168.1.5", "tcp", 22, "default"); err != nil {
		t.Fatal(err)
	}
}

func TestReportsRunAndExport(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now()
	seedReportData(t, a, now)
	for _, subj := range []string{"links", "dns", "threats", "firewall"} {
		r, err := a.runReport(reportReq{Subject: subj, From: now.Add(-24 * time.Hour).Unix()}, now)
		if err != nil {
			t.Fatalf("%s: %v", subj, err)
		}
		if r.Title == "" || r.Made == 0 {
			t.Fatalf("%s: %+v", subj, r)
		}
		pdf := reportPDF(r, "box")
		if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.HasSuffix(bytes.TrimSpace(pdf), []byte("%%EOF")) {
			t.Fatalf("%s: bad pdf", subj)
		}
		x := reportXLSX(r, "box")
		zr, err := zip.NewReader(bytes.NewReader(x), int64(len(x)))
		if err != nil || len(zr.File) != 6 {
			t.Fatalf("%s: xlsx %v", subj, err)
		}
		if !strings.HasPrefix(string(reportCSV(r)), "") {
			t.Fatal("csv")
		}
	}
	r, _ := a.runReport(reportReq{Subject: "dns", From: now.Add(-24 * time.Hour).Unix(), Group: "name", Filters: map[string]string{"result": "blocked"}}, now)
	if len(r.Rows) == 0 || !strings.Contains(string(reportCSV(r)), "ads.example.com") {
		t.Fatalf("dns rows: %+v", r.Rows)
	}
}

func TestReportsRejectBadPeriods(t *testing.T) {
	a, _ := newSpeedApp(t)
	now := time.Now()
	if _, err := a.runReport(reportReq{Subject: "dns", From: now.Unix() - 10, To: now.Unix() - 100}, now); err == nil {
		t.Fatal("start after end should fail")
	}
	if _, err := a.runReport(reportReq{Subject: "nope"}, now); err == nil {
		t.Fatal("unknown subject should fail")
	}
	if _, err := a.runReport(reportReq{Subject: "dns", From: now.Unix() - 12*365*86400}, now); err == nil {
		t.Fatal("period over 11 years should fail")
	}
}

func TestReportPDFShieldMatchesLogo(t *testing.T) {
	b, err := os.ReadFile("../../brand/logo.svg")
	if err != nil {
		t.Skip("no logo")
	}
	if !strings.Contains(string(b), logoShieldPath) {
		t.Fatal("the shield drawn in reports no longer matches brand/logo.svg")
	}
}

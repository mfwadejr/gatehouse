package ndpi

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLineIsStrict(t *testing.T) {
	good := `{"proto":"tcp","src":"192.168.1.50","sp":40000,"dst":"142.250.80.46","dp":443,"app":"TLS.YouTube","cat":"Media","risk":[]}`
	if _, err := ParseLine([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for name, l := range map[string]string{
		"extra field": `{"proto":"tcp","src":"1.2.3.4","sp":1,"dst":"1.2.3.5","dp":2,"app":"x","cat":"y","risk":[],"cmd":"rm"}`,
		"bad address": `{"proto":"tcp","src":"nope","sp":1,"dst":"1.2.3.5","dp":2,"app":"x","cat":"y","risk":[]}`,
		"v6":          `{"proto":"tcp","src":"::1","sp":1,"dst":"1.2.3.5","dp":2,"app":"x","cat":"y","risk":[]}`,
		"bad port":    `{"proto":"tcp","src":"1.2.3.4","sp":70000,"dst":"1.2.3.5","dp":2,"app":"x","cat":"y","risk":[]}`,
		"bad proto":   `{"proto":"icmp","src":"1.2.3.4","sp":1,"dst":"1.2.3.5","dp":2,"app":"x","cat":"y","risk":[]}`,
		"empty app":   `{"proto":"tcp","src":"1.2.3.4","sp":1,"dst":"1.2.3.5","dp":2,"app":"","cat":"y","risk":[]}`,
		"control":     `{"proto":"tcp","src":"1.2.3.4","sp":1,"dst":"1.2.3.5","dp":2,"app":"a\u0001b","cat":"y","risk":[]}`,
		"many risks":  `{"proto":"tcp","src":"1.2.3.4","sp":1,"dst":"1.2.3.5","dp":2,"app":"x","cat":"y","risk":["a","b","c","d"]}`,
		"not json":    `hello`,
	} {
		if _, err := ParseLine([]byte(l)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLabel(t *testing.T) {
	for in, want := range map[string]string{"TLS.YouTube": "YouTube", "QUIC.Google": "Google", "Netflix": "Netflix", "TLS.Microsoft_Teams": "Microsoft Teams"} {
		if got, ok := Label(in); !ok || got != want {
			t.Errorf("%s: %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"TLS", "DNS", "QUIC", "Unknown", "", "HTTP_Proxy"} {
		if got, ok := Label(in); ok {
			t.Errorf("%q must not be an application: %q", in, got)
		}
	}
}

func TestTableLookupBoundsAndWarnings(t *testing.T) {
	tb := NewTable()
	now := time.Unix(1700000000, 0)
	tb.Now = func() time.Time { return now }
	tb.Max = 3
	f := func(sp int, risk ...string) Flow {
		return Flow{Proto: "tcp", Src: "192.168.1.5", SP: sp, Dst: "8.8.8.8", DP: 443, App: "TLS.YouTube", Cat: "Media", Risk: risk}
	}
	for i := 1; i <= 5; i++ {
		tb.Add(f(i))
	}
	if _, ok := tb.Lookup("tcp", "192.168.1.5", 1, "8.8.8.8", 443); ok {
		t.Fatal("the oldest flows must be forgotten past the limit")
	}
	if got, ok := tb.Lookup("tcp", "192.168.1.5", 5, "8.8.8.8", 443); !ok || got.App != "TLS.YouTube" {
		t.Fatal("recent flow must be found")
	}
	if _, ok := tb.Lookup("tcp", "8.8.8.8", 443, "192.168.1.5", 5); !ok {
		t.Fatal("the other direction must match too")
	}
	now = now.Add(time.Hour)
	if _, ok := tb.Lookup("tcp", "192.168.1.5", 5, "8.8.8.8", 443); ok {
		t.Fatal("an old flow must expire")
	}
	tb.Add(f(9, "TLS Certificate Self-Signed", "Some Noise"))
	tb.Add(f(10, "Some Noise"))
	w := tb.Warnings()
	if len(w) != 1 || len(w[0].Risk) != 1 || w[0].Risk[0] != "TLS Certificate Self-Signed" {
		t.Fatalf("only worthwhile cautions are shown: %+v", w)
	}
}

func TestServeReceivesLinesAndRejectsBadOnes(t *testing.T) {
	tb := NewTable()
	path := filepath.Join(t.TempDir(), "n.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tb.Serve(ctx, path, "")
	var c net.Conn
	var err error
	for i := 0; i < 50; i++ {
		if c, err = net.Dial("unix", path); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("garbage\n" + `{"proto":"udp","src":"10.0.0.2","sp":5000,"dst":"10.0.0.1","dp":53,"app":"DNS","cat":"Network","risk":[]}` + "\n"))
	for i := 0; i < 100 && tb.Seen.Load() == 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if tb.Seen.Load() != 1 || tb.Rejected.Load() != 1 {
		t.Fatalf("seen %d rejected %d", tb.Seen.Load(), tb.Rejected.Load())
	}
	if tb.Connected.Load() != 1 {
		t.Fatal("the program should show as connected")
	}
	c.Close()
}

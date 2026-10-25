package speedtest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func fakeServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/__down", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
		w.Header().Set("cf-meta-colo", "TST")
		buf := make([]byte, 32<<10)
		for n > 0 {
			k := int64(len(buf))
			if k > n {
				k = n
			}
			if _, err := w.Write(buf[:k]); err != nil {
				return
			}
			n -= k
		}
	})
	mux.HandleFunc("/__up", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
	})
	return httptest.NewServer(mux)
}

func TestRunMeasuresSpeedAndDelay(t *testing.T) {
	if testing.Short() {
		t.Skip("takes a few seconds")
	}
	s := fakeServer()
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := Run(ctx, Params{Server: s.URL, Quick: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.DownBps <= 0 || r.UpBps <= 0 {
		t.Fatalf("speeds: %+v", r)
	}
	if r.DelayMS <= 0 || r.DelayMS > 500 {
		t.Fatalf("delay: %+v", r)
	}
	if r.Server != "Cloudflare TST" || r.Grade == "" || r.BytesUsed <= 0 {
		t.Fatalf("result: %+v", r)
	}
}

func TestRunStopsWhenAsked(t *testing.T) {
	s := fakeServer()
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, Params{Server: s.URL, Quick: true}); err == nil {
		t.Fatal("a cancelled test must fail")
	}
}

func TestRunRefusesBadInput(t *testing.T) {
	for _, p := range []Params{{Dev: "eth0; rm", Server: "https://x"}, {Dev: "eth0", Server: "ftp://x"}} {
		if _, err := Run(context.Background(), p); err == nil {
			t.Fatalf("%+v should be refused", p)
		}
	}
}

func TestGrade(t *testing.T) {
	for _, c := range []struct {
		idle, loaded float64
		want         string
	}{{10, 20, "A"}, {10, 60, "B"}, {10, 150, "C"}, {10, 300, "D"}, {10, 900, "F"}, {10, 0, ""}} {
		if got := Grade(c.idle, c.loaded); got != c.want {
			t.Errorf("%v: %q want %q", c, got, c.want)
		}
	}
}

func TestValidDevice(t *testing.T) {
	for d, ok := range map[string]bool{"eth0": true, "ppp-fibre": true, "eth0.20": true, "": false, "a b": false, "eth0;x": false, "waytoolongdevicename": false} {
		if ValidDevice(d) != ok {
			t.Errorf("%q", d)
		}
	}
}

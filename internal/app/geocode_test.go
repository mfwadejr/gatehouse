package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaceLookup(t *testing.T) {
	var gotQ, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ, gotUA = r.URL.Query().Get("q"), r.Header.Get("User-Agent")
		if gotQ == "broken" {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`[{"display_name":"New York, USA","lat":"40.71","lon":"-74.01"},{"display_name":"Bad","lat":"200","lon":"1"}]`))
	}))
	defer srv.Close()
	old := geocodeBase
	geocodeBase = srv.URL
	defer func() { geocodeBase = old }()
	c, _, _ := siteBox(t, "192.168.1.1/24")
	rc, m, raw := c.call("POST", "/api/overview/find", map[string]string{"q": "10001"})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	res := m["results"].([]any)
	if len(res) != 1 || gotQ != "10001" || !strings.Contains(gotUA, "NopeWarden") {
		t.Fatalf("results %v q=%q ua=%q", res, gotQ, gotUA)
	}
	if rc, _, _ := c.call("POST", "/api/overview/find", map[string]string{"q": "x"}); rc == 200 {
		t.Fatal("one letter accepted")
	}
	if rc, _, _ := c.call("POST", "/api/overview/find", map[string]string{"q": "broken"}); rc == 200 {
		t.Fatal("service error passed as success")
	}
}

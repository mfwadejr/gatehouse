package installer

import (
	"net/http/httptest"
	"testing"
)

func TestCodeWithOrWithoutDash(t *testing.T) {
	s := &Server{Code: "1234-5678"}
	for code, want := range map[string]bool{"1234-5678": true, "12345678": true, " 1234 5678 ": true, "1234-5679": false, "": false} {
		r := httptest.NewRequest("GET", "/api/x", nil)
		r.Header.Set("X-Install-Code", code)
		if got := s.authorised(httptest.NewRecorder(), r); got != want {
			t.Fatalf("%q: got %v want %v", code, got, want)
		}
	}
}

package installer

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Server exposes the installer over HTTP. Every call except /info needs the code that is
// displayed on the machine's own screen.
type Server struct {
	In   *Installer
	Code string
	Web  http.Handler // the embedded console files

	mu     sync.Mutex
	fails  int
	locked time.Time
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) authorised(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Now().Before(s.locked) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many wrong codes; wait a few minutes"})
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Install-Code"))
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.Code)) != 1 {
		s.fails++
		if s.fails >= 5 {
			s.fails, s.locked = 0, time.Now().Add(10*time.Minute)
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "that code is not right; it is shown on the screen of this device"})
		return false
	}
	s.fails = 0
	return true
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	i := strings.Index(o, "://")
	return i >= 0 && strings.EqualFold(o[i+3:], r.Host)
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/install/info", func(w http.ResponseWriter, r *http.Request) {
		host, _ := os.Hostname()
		writeJSON(w, 200, map[string]any{"installer": true, "efi": s.In.EFI(), "host": host})
	})
	m.HandleFunc("POST /api/install/unlock", func(w http.ResponseWriter, r *http.Request) {
		if sameOrigin(r) && s.authorised(w, r) {
			writeJSON(w, 200, map[string]string{"status": "ok"})
		}
	})
	m.HandleFunc("GET /api/install/disks", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorised(w, r) {
			return
		}
		d, err := s.In.Disks()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, d)
	})
	m.HandleFunc("GET /api/install/timezones", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorised(w, r) {
			return
		}
		writeJSON(w, 200, timezones())
	})
	m.HandleFunc("POST /api/install/start", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) || !s.authorised(w, r) {
			return
		}
		var req Request
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad request"})
			return
		}
		if err := s.In.Start(req); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "started"})
	})
	m.HandleFunc("GET /api/install/status", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorised(w, r) {
			return
		}
		writeJSON(w, 200, s.In.Status())
	})
	m.HandleFunc("POST /api/install/reboot", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) || !s.authorised(w, r) {
			return
		}
		if err := s.In.Reboot(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "rebooting"})
	})
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
	})
	m.Handle("/", s.Web)
	return secure(m)
}

func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func timezones() []string {
	b, err := os.ReadFile("/usr/share/zoneinfo/zone1970.tab")
	out := []string{"UTC"}
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) >= 3 {
			out = append(out, f[2])
		}
	}
	sort.Strings(out)
	return out
}

// Addresses lists the URLs people can use to reach the installer.
func Addresses(port string) []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, _ := ifi.Addrs()
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				u := "https://" + n.IP.String()
				if port != "" && port != "443" {
					u += ":" + port
				}
				out = append(out, u)
			}
		}
	}
	return out
}

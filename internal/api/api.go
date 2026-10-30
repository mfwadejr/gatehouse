// Package api is the console's HTTP interface: authentication, JSON endpoints and live streams.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"gatehouse/internal/auth"
	"gatehouse/internal/events"
)

const (
	cookieName = "gh_session"
	csrfHeader = "X-CSRF-Token"
	maxBody    = 1 << 20
)

// Server holds what every handler needs.
type Server struct {
	Auth   *auth.Service
	Bus    *events.Bus
	Static http.Handler // the embedded console; nil in tests
	Issuer string       // shown in authenticator apps
	// Secure marks cookies Secure; true whenever the console is served over TLS.
	Secure bool
	// Gate, when set, may refuse a signed-in request before it runs. It is how the standby unit of a
	// high-availability pair stays read-only. It returns false and a plain reason to refuse.
	Gate func(method, path string) (bool, string)
	// Topics a signed-in user may stream. Subsystems register theirs.
	topics map[string]bool
	mux    *http.ServeMux
}

func New(a *auth.Service, bus *events.Bus) *Server {
	s := &Server{Auth: a, Bus: bus, Issuer: "Gatehouse", topics: map[string]bool{}, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Ctx is passed to protected handlers.
type Ctx struct {
	User    *auth.User
	Session *auth.Session
	IP      string
	Token   string
}

// HandlerFunc is a protected endpoint.
type HandlerFunc func(w http.ResponseWriter, r *http.Request, c *Ctx)

// Handle registers a signed-in endpoint. Pattern uses Go 1.22 syntax: "GET /api/x".
// Mutating methods additionally require the CSRF header. Fully authenticated means
// password and second factor are both done.
func (s *Server) Handle(pattern string, h HandlerFunc) {
	s.mux.HandleFunc(pattern, s.guard(h, true, false))
}

// Public registers an endpoint that needs no session. It still gets the security headers.
// The handler must do its own origin check and rate limiting.
func (s *Server) Public(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

// SameOrigin reports whether a browser request came from this site.
func SameOrigin(r *http.Request) bool { return sameOrigin(r) }

// ClientIP is the address of the peer.
func ClientIP(r *http.Request) string { return clientIP(r) }

// HandleAdmin is Handle restricted to administrators.
func (s *Server) HandleAdmin(pattern string, h HandlerFunc) {
	s.mux.HandleFunc(pattern, s.guard(h, true, true))
}

// Topic lets signed-in users stream a bus topic.
func (s *Server) Topic(names ...string) {
	for _, n := range names {
		s.topics[n] = true
	}
}

// Mux exposes the router for subsystems with special needs (uploads, downloads).
func (s *Server) Mux() *http.ServeMux { return s.mux }

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) guard(h HandlerFunc, needFull, needAdmin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		if needFull && (!c.Session.MFA || c.Session.NeedsEnrol) {
			Error(w, http.StatusForbidden, "two-factor sign-in required")
			return
		}
		if needAdmin && c.User.Role != "admin" {
			Error(w, http.StatusForbidden, "administrator access required")
			return
		}
		if s.Gate != nil {
			if ok, why := s.Gate(r.Method, r.URL.Path); !ok {
				Error(w, http.StatusConflict, why)
				return
			}
		}
		h(w, r, c)
	}
}

// authenticate resolves the session and applies CSRF protection to unsafe methods.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*Ctx, bool) {
	ck, err := r.Cookie(cookieName)
	if err != nil {
		Error(w, http.StatusUnauthorized, "sign in required")
		return nil, false
	}
	sess, u, err := s.Auth.Authenticate(ck.Value)
	if err != nil {
		s.clearCookie(w)
		Error(w, http.StatusUnauthorized, "sign in required")
		return nil, false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(csrfHeader)), []byte(sess.CSRF)) != 1 || !sameOrigin(r) {
			Error(w, http.StatusForbidden, "request rejected")
			return nil, false
		}
	}
	return &Ctx{User: u, Session: sess, IP: clientIP(r), Token: ck.Value}, true
}

// sameOrigin rejects cross-site requests that carry an Origin header.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true // non-browser clients and same-origin GET-less form posts omit it
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func (s *Server) setCookie(w http.ResponseWriter, sess *auth.Session) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sess.Token, Path: "/", HttpOnly: true,
		Secure: s.Secure, SameSite: http.SameSiteStrictMode, Expires: sess.ExpiresAt})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: s.Secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// JSON writes a response body.
func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// Error writes {"error": msg}.
func Error(w http.ResponseWriter, code int, msg string) {
	JSON(w, code, map[string]string{"error": msg})
}

// Decode reads a JSON body with a size cap and unknown fields rejected.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		Error(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// Middleware applies security headers to everything.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// Handler is the full HTTP handler including headers.
func (s *Server) Handler() http.Handler {
	return Middleware(s.mux)
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("POST /api/auth/login", s.login)
	m.HandleFunc("POST /api/auth/mfa", s.mfa)
	m.HandleFunc("POST /api/auth/logout", s.guard(s.logout, false, false))
	m.HandleFunc("GET /api/auth/me", s.guard(s.me, false, false))
	m.HandleFunc("POST /api/auth/enrol/begin", s.guard(s.enrolBegin, false, false))
	m.HandleFunc("POST /api/auth/enrol/confirm", s.guard(s.enrolConfirm, false, false))
	m.HandleFunc("POST /api/auth/mfa/disable", s.guard(s.mfaDisable, true, false))
	m.HandleFunc("PUT /api/auth/policy", s.guard(s.mfaPolicy, true, true))
	m.HandleFunc("POST /api/auth/password", s.guard(s.password, true, false))
	m.HandleFunc("GET /api/auth/sessions", s.guard(s.sessions, true, false))
	m.HandleFunc("DELETE /api/auth/sessions/{id}", s.guard(s.revoke, true, false))
	m.HandleFunc("POST /api/auth/sessions/revoke-others", s.guard(s.revokeOthers, true, false))
	m.HandleFunc("GET /api/stream", s.guard(s.stream, true, false))
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { Error(w, http.StatusNotFound, "not found") })
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.Static == nil {
			http.NotFound(w, r)
			return
		}
		s.Static.ServeHTTP(w, r)
	})
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func authErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "600")
		Error(w, http.StatusTooManyRequests, err.Error())
	default:
		Error(w, http.StatusUnauthorized, auth.ErrInvalid.Error())
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		Error(w, http.StatusForbidden, "request rejected")
		return
	}
	var in loginReq
	if !Decode(w, r, &in) {
		return
	}
	res, err := s.Auth.Login(in.Username, in.Password, clientIP(r), r.UserAgent())
	if err != nil {
		authErr(w, err)
		return
	}
	if res.Session != nil {
		s.setCookie(w, res.Session)
		status := "ok"
		if res.Session.NeedsEnrol {
			status = "enrol"
		}
		JSON(w, 200, map[string]any{"status": status, "csrf": res.Session.CSRF})
		return
	}
	JSON(w, 200, map[string]any{"status": "mfa", "ticket": res.MFATicket})
}

type mfaReq struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

func (s *Server) mfa(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		Error(w, http.StatusForbidden, "request rejected")
		return
	}
	var in mfaReq
	if !Decode(w, r, &in) {
		return
	}
	sess, err := s.Auth.VerifyMFA(in.Ticket, in.Code, clientIP(r), r.UserAgent())
	if err != nil {
		authErr(w, err)
		return
	}
	s.setCookie(w, sess)
	JSON(w, 200, map[string]any{"status": "ok", "csrf": sess.CSRF})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, c *Ctx) {
	s.Auth.Logout(c.Token)
	s.clearCookie(w)
	JSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, c *Ctx) {
	JSON(w, 200, map[string]any{"user": c.User, "csrf": c.Session.CSRF, "needs_enrol": c.Session.NeedsEnrol,
		"mfa": c.Session.MFA, "recovery_left": s.Auth.RecoveryCodesLeft(c.User.ID), "mfa_required": s.Auth.MFARequired()})
}

func (s *Server) enrolBegin(w http.ResponseWriter, r *http.Request, c *Ctx) {
	// Changing an already enabled second factor needs the full sign-in.
	if c.User.TOTPEnabled && (!c.Session.MFA) {
		Error(w, http.StatusForbidden, "two-factor sign-in required")
		return
	}
	secret, uri, err := s.Auth.BeginEnrol(c.User.ID, s.Issuer)
	if err != nil {
		Error(w, 500, "could not start enrolment")
		return
	}
	JSON(w, 200, map[string]string{"secret": secret, "uri": uri})
}

type codeReq struct {
	Code string `json:"code"`
}

func (s *Server) enrolConfirm(w http.ResponseWriter, r *http.Request, c *Ctx) {
	var in codeReq
	if !Decode(w, r, &in) {
		return
	}
	codes, err := s.Auth.ConfirmEnrol(c.User.ID, in.Code)
	if err != nil {
		Error(w, http.StatusBadRequest, "that code did not match")
		return
	}
	// The session that proved the code is now a full session.
	s.Auth.MarkMFA(c.Session.ID)
	JSON(w, 200, map[string]any{"recovery_codes": codes})
}

func (s *Server) mfaDisable(w http.ResponseWriter, r *http.Request, c *Ctx) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !Decode(w, r, &in) {
		return
	}
	if err := s.Auth.DisableTOTP(c.User.ID, in.Password, strings.TrimSpace(in.Code)); err != nil {
		if errors.Is(err, auth.ErrInvalid) {
			Error(w, http.StatusBadRequest, "the password or code did not match")
		} else {
			Error(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	JSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) mfaPolicy(w http.ResponseWriter, r *http.Request, c *Ctx) {
	var in struct {
		Require bool `json:"require_mfa"`
	}
	if !Decode(w, r, &in) {
		return
	}
	if in.Require && !c.User.TOTPEnabled {
		Error(w, http.StatusBadRequest, "turn on two-factor sign-in for your own account first, so you are not locked out")
		return
	}
	if err := s.Auth.SetMFARequired(in.Require); err != nil {
		Error(w, 500, "could not save")
		return
	}
	JSON(w, 200, map[string]string{"status": "ok"})
}

type passReq struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

func (s *Server) password(w http.ResponseWriter, r *http.Request, c *Ctx) {
	var in passReq
	if !Decode(w, r, &in) {
		return
	}
	if err := s.Auth.ChangePassword(c.User.ID, in.Current, in.New); err != nil {
		if errors.Is(err, auth.ErrInvalid) {
			Error(w, http.StatusForbidden, "current password is wrong")
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Auth.RevokeOthers(c.User.ID, c.Session.ID)
	JSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) sessions(w http.ResponseWriter, r *http.Request, c *Ctx) {
	list, err := s.Auth.ListSessions(c.User.ID, c.Session.ID)
	if err != nil {
		Error(w, 500, "could not list sessions")
		return
	}
	JSON(w, 200, list)
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request, c *Ctx) {
	if err := s.Auth.Revoke(c.User.ID, r.PathValue("id")); err != nil {
		Error(w, 500, "could not revoke")
		return
	}
	JSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) revokeOthers(w http.ResponseWriter, r *http.Request, c *Ctx) {
	s.Auth.RevokeOthers(c.User.ID, c.Session.ID)
	JSON(w, 200, map[string]string{"status": "ok"})
}

// stream serves server-sent events: GET /api/stream?topics=dns.stats,dhcp.leases
func (s *Server) stream(w http.ResponseWriter, r *http.Request, c *Ctx) {
	fl, ok := w.(http.Flusher)
	if !ok {
		Error(w, 500, "streaming unsupported")
		return
	}
	var topics []string
	for _, t := range strings.Split(r.URL.Query().Get("topics"), ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !s.topics[t] {
			Error(w, http.StatusBadRequest, "unknown topic "+t)
			return
		}
		topics = append(topics, t)
	}
	if len(topics) == 0 {
		Error(w, http.StatusBadRequest, "no topics")
		return
	}
	ch, cancel := s.Bus.Subscribe(topics...)
	defer cancel()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()
	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	ctx := r.Context()
	rc := http.NewResponseController(w)
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Topic, m.Data); err != nil {
				return
			}
			fl.Flush()
		case <-keep.C:
			rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// Serve runs the handler on a listener until ctx ends.
func Serve(ctx context.Context, srv *http.Server, l net.Listener, tlsCert, tlsKey string) error {
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sc)
	}()
	var err error
	if tlsCert != "" {
		err = srv.ServeTLS(l, tlsCert, tlsKey)
	} else {
		err = srv.Serve(l)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type connKey struct{}

var connCounter atomic.Uint64

// ConnContext tags each TCP connection with an id. Install it as http.Server.ConnContext.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, connCounter.Add(1))
}

// ConnID identifies the connection a request arrived on (0 if untagged).
func ConnID(r *http.Request) uint64 {
	v, _ := r.Context().Value(connKey{}).(uint64)
	return v
}

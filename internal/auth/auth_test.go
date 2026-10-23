package auth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/crypt"
	"gatehouse/internal/store"
)

func newSvc(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenConfig(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := crypt.LoadOrCreate(filepath.Join(dir, "k"))
	s := New(db, box)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func TestPasswordHashing(t *testing.T) {
	h, err := hashWith("correct horse battery", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery", h) || VerifyPassword("wrong", h) {
		t.Fatal("verify mismatch")
	}
	if VerifyPassword("x", "garbage") {
		t.Fatal("garbage hash accepted")
	}
	if CheckPasswordPolicy("short", "u") == nil {
		t.Fatal("short password accepted")
	}
}

// RFC 6238 appendix B test vector (SHA1, secret "12345678901234567890").
func TestTOTPVector(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	code, err := totpCode(secret, 59/30, 8)
	if err != nil || code != "94287082" {
		t.Fatalf("got %s err %v", code, err)
	}
	code6, _ := totpCode(secret, 59/30, 6)
	if code6 != "287082" {
		t.Fatalf("6 digit: %s", code6)
	}
	step, ok := VerifyTOTP(secret, "287082", time.Unix(59, 0), 0)
	if !ok || step != 1 {
		t.Fatal("verify failed")
	}
	if _, ok := VerifyTOTP(secret, "287082", time.Unix(59, 0), step); ok {
		t.Fatal("replay accepted")
	}
	if _, ok := VerifyTOTP(secret, "000000", time.Unix(59, 0), 0); ok {
		t.Fatal("bad code accepted")
	}
}

func enrol(t *testing.T, s *Service, now time.Time, uid int64) (string, []string) {
	t.Helper()
	secret, uri, err := s.BeginEnrol(uid, "Gatehouse")
	if err != nil || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("begin: %v %s", err, uri)
	}
	code, _ := totpCode(secret, now.Unix()/30, 6)
	codes, err := s.ConfirmEnrol(uid, code)
	if err != nil || len(codes) != 10 {
		t.Fatalf("confirm: %v", err)
	}
	return secret, codes
}

func TestFullLoginFlow(t *testing.T) {
	s, now := newSvc(t)
	u, err := s.CreateUser("marvin", "a-long-passphrase-1", "admin")
	if err != nil {
		t.Fatal(err)
	}
	s.SetMFARequired(true)
	// Without TOTP: session is created but must enrol.
	r, err := s.Login("marvin", "a-long-passphrase-1", "10.0.0.5", "ua")
	if err != nil || r.Session == nil || !r.Session.NeedsEnrol || r.Session.MFA {
		t.Fatalf("pre-enrol login: %+v %v", r, err)
	}
	secret, codes := enrol(t, s, *now, u.ID)

	// With TOTP: password alone only yields a ticket.
	r, err = s.Login("marvin", "a-long-passphrase-1", "10.0.0.5", "ua")
	if err != nil || r.Session != nil || r.MFATicket == "" {
		t.Fatalf("expected ticket: %+v %v", r, err)
	}
	*now = now.Add(31 * time.Second) // next time step (enrolment used the current one)
	code, _ := totpCode(secret, now.Unix()/30, 6)
	sess, err := s.VerifyMFA(r.MFATicket, code, "10.0.0.5", "ua")
	if err != nil || !sess.MFA {
		t.Fatalf("mfa: %v", err)
	}
	// Authenticate with the token.
	got, user, err := s.Authenticate(sess.Token)
	if err != nil || user.Username != "marvin" || got.NeedsEnrol {
		t.Fatalf("authenticate: %v", err)
	}
	// Ticket is single use.
	if _, err := s.VerifyMFA(r.MFATicket, code, "10.0.0.5", "ua"); err == nil {
		t.Fatal("ticket reused")
	}
	// Recovery code works once.
	r, _ = s.Login("marvin", "a-long-passphrase-1", "10.0.0.5", "ua")
	if _, err := s.VerifyMFA(r.MFATicket, codes[0], "10.0.0.5", "ua"); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	r, _ = s.Login("marvin", "a-long-passphrase-1", "10.0.0.5", "ua")
	if _, err := s.VerifyMFA(r.MFATicket, codes[0], "10.0.0.5", "ua"); err == nil {
		t.Fatal("recovery code reused")
	}
	if n := s.RecoveryCodesLeft(u.ID); n != 9 {
		t.Fatalf("codes left %d", n)
	}
	// Logout.
	s.Logout(sess.Token)
	if _, _, err := s.Authenticate(sess.Token); err == nil {
		t.Fatal("session survived logout")
	}
}

func TestSessionTimeouts(t *testing.T) {
	s, now := newSvc(t)
	u, _ := s.CreateUser("a", "a-long-passphrase-1", "admin")
	sess, _ := s.newSession(u.ID, "ip", "ua", true)
	*now = now.Add(7 * time.Hour)
	if _, _, err := s.Authenticate(sess.Token); err != nil {
		t.Fatalf("within idle: %v", err)
	}
	*now = now.Add(9 * time.Hour) // idle exceeded since last use
	if _, _, err := s.Authenticate(sess.Token); err == nil {
		t.Fatal("idle timeout not enforced")
	}
	sess, _ = s.newSession(u.ID, "ip", "ua", true)
	for i := 0; i < 30; i++ { // keep it busy for > 7 days
		*now = now.Add(7 * time.Hour)
		if _, _, err := s.Authenticate(sess.Token); err != nil {
			return // absolute limit reached
		}
	}
	t.Fatal("absolute timeout not enforced")
}

func TestLockoutAndRateLimit(t *testing.T) {
	s, now := newSvc(t)
	s.CreateUser("bob", "a-long-passphrase-1", "admin")
	for i := 0; i < 5; i++ {
		s.Login("bob", "wrong", "10.0.0."+string(rune('1'+i)), "ua") // different IPs: account lock only
	}
	if _, err := s.Login("bob", "a-long-passphrase-1", "10.9.9.9", "ua"); err != ErrRateLimited {
		t.Fatalf("account should be locked, got %v", err)
	}
	*now = now.Add(6 * time.Minute)
	if _, err := s.Login("bob", "a-long-passphrase-1", "10.9.9.9", "ua"); err != nil {
		t.Fatalf("lock should have expired: %v", err)
	}
	// One IP hammering unknown users gets blocked.
	for i := 0; i < 10; i++ {
		s.Login("nobody", "x", "7.7.7.7", "ua")
	}
	if _, err := s.Login("nobody", "x", "7.7.7.7", "ua"); err != ErrRateLimited {
		t.Fatalf("ip should be blocked, got %v", err)
	}
	// Unknown user and wrong password are indistinguishable.
	_, e1 := s.Login("nobody", "x", "8.8.8.8", "ua")
	_, e2 := s.Login("bob", "x", "8.8.8.9", "ua")
	if e1 != e2 {
		t.Fatalf("user enumeration: %v vs %v", e1, e2)
	}
}

func TestMFATicketAttempts(t *testing.T) {
	s, now := newSvc(t)
	u, _ := s.CreateUser("c", "a-long-passphrase-1", "admin")
	enrol(t, s, *now, u.ID)
	r, _ := s.Login("c", "a-long-passphrase-1", "1.1.1.1", "ua")
	for i := 0; i < 3; i++ {
		s.VerifyMFA(r.MFATicket, "000000", "1.1.1.1", "ua")
	}
	if _, err := s.VerifyMFA(r.MFATicket, "000000", "1.1.1.1", "ua"); err != ErrInvalid {
		t.Fatalf("ticket should be dead: %v", err)
	}
}

// Two-step sign-in is optional unless required, and can be turned off with password and code.
func TestOptionalMFA(t *testing.T) {
	s, now := newSvc(t)
	u, _ := s.CreateUser("marvin", "a-long-passphrase-1", "admin")
	r, err := s.Login("marvin", "a-long-passphrase-1", "10.0.0.5", "ua")
	if err != nil || r.Session == nil || r.Session.NeedsEnrol || !r.Session.MFA {
		t.Fatalf("optional login should be a full session: %+v %v", r, err)
	}
	if sess, _, err := s.Authenticate(r.Session.Token); err != nil || sess.NeedsEnrol || !sess.MFA {
		t.Fatalf("authenticate: %+v %v", sess, err)
	}
	secret, _ := enrol(t, s, *now, u.ID)
	*now = now.Add(31 * time.Second)
	if err := s.DisableTOTP(u.ID, "wrong-password-123", "000000"); err == nil {
		t.Fatal("disabled with the wrong password")
	}
	if err := s.DisableTOTP(u.ID, "a-long-passphrase-1", "000000"); err == nil {
		t.Fatal("disabled with the wrong code")
	}
	code, _ := totpCode(secret, now.Unix()/30, 6)
	if err := s.DisableTOTP(u.ID, "a-long-passphrase-1", code); err != nil {
		t.Fatalf("disable: %v", err)
	}
	r, err = s.Login("marvin", "a-long-passphrase-1", "10.0.0.5", "ua")
	if err != nil || r.Session == nil || r.MFATicket != "" {
		t.Fatalf("after disabling, password alone should sign in: %+v %v", r, err)
	}
	// When required, it cannot be disabled and sign-in forces enrolment.
	s.SetMFARequired(true)
	if err := s.DisableTOTP(u.ID, "a-long-passphrase-1", ""); err == nil {
		t.Fatal("disabled while required")
	}
	r, _ = s.Login("marvin", "a-long-passphrase-1", "10.0.0.5", "ua")
	if r.Session == nil || !r.Session.NeedsEnrol {
		t.Fatal("required policy did not force enrolment")
	}
}

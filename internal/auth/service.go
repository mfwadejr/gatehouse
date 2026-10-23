package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/crypt"
	"gatehouse/internal/store"
)

var (
	// ErrInvalid is returned for any failed login so the reason is never revealed.
	ErrInvalid     = errors.New("invalid credentials")
	ErrRateLimited = errors.New("too many attempts, try again later")
	ErrSession     = errors.New("session expired or invalid")
	ErrNeedMFA     = errors.New("two-factor code required")
)

// User is an account as the rest of the program sees it.
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	TOTPEnabled bool   `json:"totp_enabled"`
}

// Session is an authenticated browser session.
type Session struct {
	Token      string // raw token; only populated when the session is created
	ID         string // sha256 of the token, the database key
	UserID     int64
	CSRF       string
	ExpiresAt  time.Time
	MFA        bool // second factor satisfied
	NeedsEnrol bool // password ok but TOTP not enrolled yet: only enrolment calls allowed
}

// LoginResult is what a password check returns: either a session, or a ticket that must
// be exchanged for one together with a TOTP or recovery code.
type LoginResult struct {
	Session   *Session
	MFATicket string
}

type ticket struct {
	userID   int64
	expires  time.Time
	attempts int
	ip       string
}

// Service is the account and session store.
type Service struct {
	db  *store.DB
	box *crypt.Box
	now func() time.Time

	ipLim *Limiter

	mu      sync.Mutex
	tickets map[string]*ticket
	dummy   string // hash verified when the user does not exist, to even out timing
}

func New(db *store.DB, box *crypt.Box) *Service {
	d, _ := hashWith("dummy-password-for-timing", pbkdf2Iterations)
	return &Service{
		db: db, box: box, now: time.Now,
		ipLim:   NewLimiter(10, 10*time.Minute, 15*time.Minute),
		tickets: map[string]*ticket{},
		dummy:   d,
	}
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// HasUsers reports whether any account exists (used by first-run checks).
func (s *Service) HasUsers() bool {
	var n int
	s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return n > 0
}

// CreateUser adds an account after checking the password policy.
func (s *Service) CreateUser(username, password, role string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 {
		return User{}, errors.New("username must be 1 to 64 characters")
	}
	if err := CheckPasswordPolicy(password, username); err != nil {
		return User{}, err
	}
	h, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	res, err := s.db.Exec(`INSERT INTO users(username,pass_hash,role,created_at) VALUES(?,?,?,?)`,
		username, h, role, s.now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, errors.New("that username is taken")
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Username: username, Role: role}, nil
}

// SetPassword replaces a user's password.
func (s *Service) SetPassword(userID int64, password string) error {
	var name string
	if err := s.db.QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&name); err != nil {
		return err
	}
	if err := CheckPasswordPolicy(password, name); err != nil {
		return err
	}
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET pass_hash=?, failed_count=0, locked_until=0 WHERE id=?`, h, userID)
	return err
}

type userRow struct {
	User
	hash       string
	secretSeal string
	lastStep   int64
	failed     int
	lockedTill int64
}

func (s *Service) loadByName(name string) (*userRow, error) {
	var r userRow
	var en int
	err := s.db.QueryRow(`SELECT id,username,role,pass_hash,totp_secret,totp_enabled,totp_last_step,failed_count,locked_until
		FROM users WHERE username=?`, name).Scan(&r.ID, &r.Username, &r.Role, &r.hash, &r.secretSeal, &en, &r.lastStep, &r.failed, &r.lockedTill)
	if err != nil {
		return nil, err
	}
	r.TOTPEnabled = en == 1
	return &r, nil
}

func (s *Service) loadByID(id int64) (*userRow, error) {
	var r userRow
	var en int
	err := s.db.QueryRow(`SELECT id,username,role,pass_hash,totp_secret,totp_enabled,totp_last_step,failed_count,locked_until
		FROM users WHERE id=?`, id).Scan(&r.ID, &r.Username, &r.Role, &r.hash, &r.secretSeal, &en, &r.lastStep, &r.failed, &r.lockedTill)
	if err != nil {
		return nil, err
	}
	r.TOTPEnabled = en == 1
	return &r, nil
}

// accountFail counts a failure against the account and locks it after five, for a
// period that grows with repeated lockouts (5, 10, 15 minutes).
func (s *Service) accountFail(u *userRow) {
	failed := u.failed + 1
	var lock int64
	if failed%5 == 0 {
		mins := failed / 5 * 5
		if mins > 15 {
			mins = 15
		}
		lock = s.now().Add(time.Duration(mins) * time.Minute).Unix()
	}
	s.db.Exec(`UPDATE users SET failed_count=?, locked_until=CASE WHEN ?>0 THEN ? ELSE locked_until END WHERE id=?`, failed, lock, lock, u.ID)
}

// Login checks a password. Callers pass the client IP so repeated failures from one
// source are throttled, and accounts lock temporarily after repeated failures.
func (s *Service) Login(username, password, ip, ua string) (LoginResult, error) {
	if until, blocked := s.ipLim.Blocked("ip:" + ip); blocked {
		_ = until
		return LoginResult{}, ErrRateLimited
	}
	u, err := s.loadByName(strings.TrimSpace(username))
	if err != nil {
		VerifyPassword(password, s.dummy) // equalise timing
		s.ipLim.Fail("ip:" + ip)
		return LoginResult{}, ErrInvalid
	}
	if s.now().Unix() < u.lockedTill {
		VerifyPassword(password, s.dummy)
		return LoginResult{}, ErrRateLimited
	}
	if !VerifyPassword(password, u.hash) {
		s.accountFail(u)
		s.ipLim.Fail("ip:" + ip)
		return LoginResult{}, ErrInvalid
	}
	if !u.TOTPEnabled {
		sess, err := s.newSession(u.ID, ip, ua, false)
		if err != nil {
			return LoginResult{}, err
		}
		sess.NeedsEnrol = true
		return LoginResult{Session: sess}, nil
	}
	t := randToken(24)
	s.mu.Lock()
	s.tickets[t] = &ticket{userID: u.ID, expires: s.now().Add(5 * time.Minute), ip: ip}
	s.mu.Unlock()
	return LoginResult{MFATicket: t}, nil
}

// VerifyMFA completes a login with a TOTP code or an unused recovery code.
func (s *Service) VerifyMFA(tkt, code, ip, ua string) (*Session, error) {
	if _, blocked := s.ipLim.Blocked("ip:" + ip); blocked {
		return nil, ErrRateLimited
	}
	s.mu.Lock()
	t := s.tickets[tkt]
	if t == nil || s.now().After(t.expires) {
		delete(s.tickets, tkt)
		s.mu.Unlock()
		return nil, ErrInvalid
	}
	t.attempts++
	if t.attempts > 3 {
		delete(s.tickets, tkt)
		s.mu.Unlock()
		s.ipLim.Fail("ip:" + ip)
		return nil, ErrInvalid
	}
	userID := t.userID
	s.mu.Unlock()

	u, err := s.loadByID(userID)
	if err != nil {
		return nil, ErrInvalid
	}
	ok, err := s.checkSecondFactor(u, code)
	if err != nil || !ok {
		s.accountFail(u)
		s.ipLim.Fail("ip:" + ip)
		return nil, ErrInvalid
	}
	s.mu.Lock()
	delete(s.tickets, tkt)
	s.mu.Unlock()
	s.ipLim.Reset("ip:" + ip)
	s.db.Exec(`UPDATE users SET failed_count=0, locked_until=0, last_login=? WHERE id=?`, s.now().Unix(), u.ID)
	return s.newSession(u.ID, ip, ua, true)
}

// checkSecondFactor accepts a TOTP code (once per time step) or a recovery code (once).
func (s *Service) checkSecondFactor(u *userRow, code string) (bool, error) {
	secret, err := s.box.Open(u.secretSeal)
	if err != nil {
		return false, err
	}
	if step, ok := VerifyTOTP(secret, code, s.now(), u.lastStep); ok {
		_, err := s.db.Exec(`UPDATE users SET totp_last_step=? WHERE id=?`, step, u.ID)
		return err == nil, err
	}
	res, err := s.db.Exec(`UPDATE recovery_codes SET used_at=? WHERE user_id=? AND code_hash=? AND used_at=0`,
		s.now().Unix(), u.ID, hashRecovery(code))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// VerifyStepUp re-checks a TOTP code for a sensitive action such as applying firewall changes.
func (s *Service) VerifyStepUp(userID int64, code string) error {
	u, err := s.loadByID(userID)
	if err != nil {
		return ErrInvalid
	}
	if !u.TOTPEnabled {
		return nil
	}
	ok, err := s.checkSecondFactor(u, code)
	if err != nil || !ok {
		return ErrInvalid
	}
	return nil
}

// BeginEnrol creates a pending TOTP secret and returns it with the provisioning URI.
func (s *Service) BeginEnrol(userID int64, issuer string) (secret, uri string, err error) {
	u, err := s.loadByID(userID)
	if err != nil {
		return "", "", err
	}
	secret = NewTOTPSecret()
	_, err = s.db.Exec(`UPDATE users SET totp_secret=?, totp_enabled=0, totp_last_step=0 WHERE id=?`, s.box.Seal(secret), userID)
	if err != nil {
		return "", "", err
	}
	return secret, TOTPURI(issuer, u.Username, secret), nil
}

// ConfirmEnrol activates TOTP once the user proves the app works, and issues recovery codes.
func (s *Service) ConfirmEnrol(userID int64, code string) ([]string, error) {
	u, err := s.loadByID(userID)
	if err != nil {
		return nil, err
	}
	if u.secretSeal == "" {
		return nil, errors.New("no enrolment in progress")
	}
	secret, err := s.box.Open(u.secretSeal)
	if err != nil {
		return nil, err
	}
	step, ok := VerifyTOTP(secret, code, s.now(), 0)
	if !ok {
		return nil, ErrInvalid
	}
	codes := NewRecoveryCodes(10)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET totp_enabled=1, totp_last_step=? WHERE id=?`, step, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
		return nil, err
	}
	for _, c := range codes {
		if _, err := tx.Exec(`INSERT INTO recovery_codes(user_id,code_hash) VALUES(?,?)`, userID, hashRecovery(c)); err != nil {
			return nil, err
		}
	}
	return codes, tx.Commit()
}

// RecoveryCodesLeft counts unused recovery codes.
func (s *Service) RecoveryCodesLeft(userID int64) int {
	var n int
	s.db.QueryRow(`SELECT count(*) FROM recovery_codes WHERE user_id=? AND used_at=0`, userID).Scan(&n)
	return n
}

// ClearTOTP removes a user's second factor (used by the local recovery command).
func (s *Service) ClearTOTP(userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET totp_enabled=0, totp_secret='', totp_last_step=0 WHERE id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) idleAndAbsolute() (time.Duration, time.Duration) {
	idle := 8 * time.Hour
	if h := s.db.Setting("session_idle_hours", ""); h != "" {
		var n int
		if _, err := fmt.Sscanf(h, "%d", &n); err == nil && n > 0 && n <= 168 {
			idle = time.Duration(n) * time.Hour
		}
	}
	return idle, 7 * 24 * time.Hour
}

func (s *Service) newSession(userID int64, ip, ua string, mfa bool) (*Session, error) {
	tok := randToken(32)
	csrf := randToken(18)
	idle, _ := s.idleAndAbsolute()
	now := s.now()
	exp := now.Add(idle)
	m := 0
	if mfa {
		m = 1
	}
	if len(ua) > 200 {
		ua = ua[:200]
	}
	if _, err := s.db.Exec(`INSERT INTO sessions(id,user_id,created_at,last_seen,expires_at,ip,ua,mfa,csrf) VALUES(?,?,?,?,?,?,?,?,?)`,
		sha(tok), userID, now.Unix(), now.Unix(), exp.Unix(), ip, ua, m, csrf); err != nil {
		return nil, err
	}
	return &Session{Token: tok, ID: sha(tok), UserID: userID, CSRF: csrf, ExpiresAt: exp, MFA: mfa}, nil
}

// Authenticate resolves a session token to its session and user, enforcing the idle and
// absolute timeouts and sliding the idle window forward.
func (s *Service) Authenticate(token string) (*Session, *User, error) {
	if token == "" {
		return nil, nil, ErrSession
	}
	id := sha(token)
	var sess Session
	var created, last, exp int64
	var mfa int
	err := s.db.QueryRow(`SELECT user_id,created_at,last_seen,expires_at,mfa,csrf FROM sessions WHERE id=?`, id).
		Scan(&sess.UserID, &created, &last, &exp, &mfa, &sess.CSRF)
	if err != nil {
		return nil, nil, ErrSession
	}
	now := s.now()
	idle, abs := s.idleAndAbsolute()
	if now.Unix() > exp || now.Sub(time.Unix(created, 0)) > abs {
		s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
		return nil, nil, ErrSession
	}
	newExp := now.Add(idle)
	if lim := time.Unix(created, 0).Add(abs); newExp.After(lim) {
		newExp = lim
	}
	s.db.Exec(`UPDATE sessions SET last_seen=?, expires_at=? WHERE id=?`, now.Unix(), newExp.Unix(), id)
	u, err := s.loadByID(sess.UserID)
	if err != nil {
		return nil, nil, ErrSession
	}
	sess.ID = id
	sess.MFA = mfa == 1
	sess.NeedsEnrol = !u.TOTPEnabled
	sess.ExpiresAt = newExp
	return &sess, &u.User, nil
}

// Logout destroys a session.
func (s *Service) Logout(token string) {
	s.db.Exec(`DELETE FROM sessions WHERE id=?`, sha(token))
}

// SessionInfo describes a session for the sessions list.
type SessionInfo struct {
	ID       string `json:"id"`
	IP       string `json:"ip"`
	UA       string `json:"ua"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"last_seen"`
	Current  bool   `json:"current"`
}

// ListSessions returns a user's active sessions, marking the current one.
func (s *Service) ListSessions(userID int64, currentID string) ([]SessionInfo, error) {
	rows, err := s.db.Query(`SELECT id,ip,ua,created_at,last_seen FROM sessions WHERE user_id=? AND expires_at>? ORDER BY last_seen DESC`, userID, s.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.ID, &si.IP, &si.UA, &si.Created, &si.LastSeen); err != nil {
			return nil, err
		}
		si.Current = si.ID == currentID
		out = append(out, si)
	}
	return out, rows.Err()
}

// RevokeOthers signs out every session of the user except the current one.
func (s *Service) RevokeOthers(userID int64, currentID string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=? AND id<>?`, userID, currentID)
	return err
}

// Revoke signs out one session of the user.
func (s *Service) Revoke(userID int64, id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=? AND id=?`, userID, id)
	return err
}

// UserByName looks an account up (used by the recovery command).
func (s *Service) UserByName(name string) (User, error) {
	u, err := s.loadByName(name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, errors.New("no such user")
		}
		return User{}, err
	}
	return u.User, nil
}

// PurgeExpired removes expired sessions and tickets.
func (s *Service) PurgeExpired() {
	s.db.Exec(`DELETE FROM sessions WHERE expires_at<?`, s.now().Unix())
	s.mu.Lock()
	for k, t := range s.tickets {
		if s.now().After(t.expires) {
			delete(s.tickets, k)
		}
	}
	s.mu.Unlock()
}

// MarkMFA records that a session has satisfied the second factor (after enrolment).
func (s *Service) MarkMFA(sessionID string) {
	s.db.Exec(`UPDATE sessions SET mfa=1 WHERE id=?`, sessionID)
}

// ChangePassword verifies the current password and sets a new one.
func (s *Service) ChangePassword(userID int64, current, next string) error {
	u, err := s.loadByID(userID)
	if err != nil {
		return ErrInvalid
	}
	if !VerifyPassword(current, u.hash) {
		return ErrInvalid
	}
	return s.SetPassword(userID, next)
}

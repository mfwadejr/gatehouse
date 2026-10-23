package app

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gatehouse/internal/api"
)

// First-run setup. Until an account exists the console asks for a one-time code that is
// shown only on this machine's own screen (and kept in a root-readable file), so someone
// else on the network cannot claim a freshly installed box.
type setupGate struct {
	mu       sync.Mutex
	code     string
	fails    int
	lockedTo time.Time
}

func (a *App) setupCodePath() string { return filepath.Join(a.Cfg.DataDir, "setup-code") }

func newCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		panic(err)
	}
	s := fmt.Sprintf("%08d", n.Int64())
	return s[:4] + "-" + s[4:]
}

// prepareSetup creates the code while no account exists and removes it afterwards.
func (a *App) prepareSetup() {
	if a.Auth.HasUsers() {
		os.Remove(a.setupCodePath())
		return
	}
	a.setup.mu.Lock()
	a.setup.code = newCode()
	code := a.setup.code
	a.setup.mu.Unlock()
	os.WriteFile(a.setupCodePath(), []byte(code+"\n"), 0o600)
	fmt.Fprintf(os.Stderr, "\n  First-time setup code: %s\n  Open the console in a browser and enter it to create the administrator account.\n\n", code)
}

func (a *App) registerSetupRoutes() {
	s := a.API
	s.Public("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		api.JSON(w, 200, map[string]bool{"needed": !a.Auth.HasUsers()})
	})
	s.Public("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !api.SameOrigin(r) {
			api.Error(w, http.StatusForbidden, "request rejected")
			return
		}
		var in struct {
			Code     string `json:"code"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if a.Auth.HasUsers() {
			api.Error(w, http.StatusConflict, "this device is already set up")
			return
		}
		g := &a.setup
		g.mu.Lock()
		defer g.mu.Unlock()
		if time.Now().Before(g.lockedTo) {
			api.Error(w, http.StatusTooManyRequests, "too many wrong codes; wait a few minutes and try again")
			return
		}
		digits := func(s string) string {
			return strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, s)
		}
		code := digits(in.Code)
		if g.code == "" || subtle.ConstantTimeCompare([]byte(code), []byte(digits(g.code))) != 1 {
			g.fails++
			if g.fails >= 5 {
				g.fails, g.lockedTo = 0, time.Now().Add(10*time.Minute)
			}
			api.Error(w, http.StatusForbidden, "that setup code is not right; it is shown on the screen of this device")
			return
		}
		if _, err := a.Auth.CreateUser(in.Username, in.Password, "admin"); err != nil {
			api.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		g.code = ""
		os.Remove(a.setupCodePath())
		api.JSON(w, 200, map[string]string{"status": "ok"})
	})
}

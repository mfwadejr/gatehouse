package app

import (
	"errors"
	"net/http"

	"gatehouse/internal/api"
)

// Serious actions (reset, restore, revert to an earlier point, turning on SSH, downloading a plain
// backup) ask the person to prove it is them: the authenticator code when two-factor sign-in is on,
// and their password when it is not. Two-factor sign-in stays optional and is never forced.
// Everyday changes (applying network or firewall changes, updating, restarting) ask for nothing
// unless the person switched on "Ask for a code before changing the firewall".

var (
	errNeedCode = errors.New("enter a current code from your authenticator")
	errNeedPass = errors.New("type your password to confirm")
)

// serious checks the code or the password for the signed-in person.
func (a *App) serious(c *api.Ctx, code, password string) error {
	if c.User.TOTPEnabled {
		if code == "" || a.Auth.VerifyStepUp(c.User.ID, code) != nil {
			return errNeedCode
		}
		return nil
	}
	if password == "" {
		return errNeedPass
	}
	if err := a.Auth.CheckPassword(c.User.Username, password, c.IP); err != nil {
		return errors.New("that password is not right")
	}
	return nil
}

func (a *App) registerStepUpRoutes() {
	// What the screens should ask for: a code on routine changes only when the person switched that
	// on and uses two-factor sign-in; on serious ones a code (two-factor on) or the password.
	a.API.Handle("GET /api/stepup", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, map[string]any{
			"everyday": a.Config.Setting("fw.stepup", "0") == "1" && c.User.TOTPEnabled,
			"serious":  map[bool]string{true: "code", false: "password"}[c.User.TOTPEnabled],
		})
	})
}

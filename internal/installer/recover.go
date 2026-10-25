package installer

import (
	"errors"
	"io"
	"os"
	"strings"
)

// RecoveryEnv is what the recovery screen can do. The command that starts it connects these to the
// real account database and firewall.
type RecoveryEnv struct {
	Users         func() []string
	SetPassword   func(name, password string) error
	ClearTwoStep  func(name string) error
	ResetFirewall func() error
	Diagnose      func() string // what is running and the last error lines
	// ResetAccess puts the console back on port 443 and removes any limit on who may manage.
	ResetAccess func() error
	// ResetToDefault clears the settings to "network" or "factory" level, after saving a copy.
	// It returns where the copy was saved.
	ResetToDefault func(level string) (string, error)
}

// RunRecovery is the screen for someone who is locked out and is standing at the machine. Physical
// access is the proof: it asks nothing else. It offers the three things that unlock a device.
func RunRecovery(t *Term, env RecoveryEnv) error {
	for {
		t.heading("Recovery", "What do you need?")
		t.p("  1) Choose a new password for an administrator\n")
		t.p("  2) Turn off two-step sign-in for an administrator\n")
		t.p("  3) Reset the firewall to the safe starting rules\n")
		t.p("  4) Check what is running and why the console may be down\n")
		t.p("  5) Let any management network in, and put the console back on port 443\n")
		t.p("  6) Reset to default (network settings, or everything)\n")
		t.p("  7) Leave recovery\n\n")
		n, err := t.ask("Number", "7", func(s string) error {
			if s < "1" || s > "7" || len(s) != 1 {
				return errors.New("enter a number from 1 to 7")
			}
			return nil
		})
		if err != nil {
			if err == errRestart {
				continue
			}
			return err
		}
		switch n {
		case "1", "2":
			name, err := pickUser(t, env)
			if err != nil {
				if err == errRestart {
					continue
				}
				return err
			}
			if name == "" {
				continue
			}
			if n == "2" {
				if err := env.ClearTwoStep(name); err != nil {
					t.p("\n  %s%v%s\n", red, err, reset)
				} else {
					t.p("\n  %sTwo-step sign-in is off for %s.%s They can turn it on again after signing in.\n", green, name, reset)
				}
				pause(t)
				continue
			}
			var pw string
			for {
				p1, err := t.password("New password (at least 12 characters)")
				if err != nil {
					break
				}
				t.p("\n")
				if len([]rune(p1)) < 12 {
					t.p("  %sToo short: %d of the 12 characters needed.%s\n", red, len([]rune(p1)), reset)
					continue
				}
				p2, err := t.password("Type it again")
				if err != nil {
					break
				}
				t.p("\n")
				if p1 != p2 {
					t.p("  %sThe two passwords differ. Try again.%s\n", red, reset)
					continue
				}
				pw = p1
				break
			}
			if pw == "" {
				continue
			}
			if err := env.SetPassword(name, pw); err != nil {
				t.p("\n  %s%v%s\n", red, err, reset)
			} else {
				t.p("\n  %sPassword changed for %s.%s\n", green, name, reset)
			}
			pause(t)
		case "3":
			ok, err := t.yesno("Reset the firewall? Your own rules stay saved but stop applying until you apply again", false)
			if err != nil {
				if err == errRestart {
					continue
				}
				return err
			}
			if ok {
				if err := env.ResetFirewall(); err != nil {
					t.p("\n  %s%v%s\n", red, err, reset)
				} else {
					t.p("\n  %sThe firewall is back to the safe starting rules.%s\n  Sign in to the console and apply your firewall again when you are ready.\n", green, reset)
				}
				pause(t)
			}
		case "4":
			t.heading("Recovery", "What is running")
			if env.Diagnose != nil {
				for _, l := range strings.Split(env.Diagnose(), "\n") {
					t.p("  %s\n", trim(l, t.W-6))
				}
			}
			pause(t)
		case "5":
			ok, err := t.yesno("Remove the limit on who may manage, and put the console back on port 443? The console restarts", false)
			if err != nil {
				if err == errRestart {
					continue
				}
				return err
			}
			if ok {
				if env.ResetAccess == nil {
					t.p("\n  %sNot available on this machine.%s\n", red, reset)
				} else if err := env.ResetAccess(); err != nil {
					t.p("\n  %s%v%s\n", red, err, reset)
				} else {
					t.p("\n  %sDone.%s Open the console on port 443 from any management network. Wait about a minute for it to restart.\n", green, reset)
				}
				pause(t)
			}
		case "6":
			if env.ResetToDefault == nil {
				t.p("\n  %sNot available on this machine.%s\n", red, reset)
				pause(t)
				continue
			}
			i, err := t.choose("What should be reset?", []string{
				"Network settings: networks, firewall, DHCP, DNS, VPN and Wi-Fi (accounts stay)",
				"Everything: factory reset, the setup wizard opens again",
				"Cancel",
			}, 3)
			if err != nil {
				if err == errRestart {
					continue
				}
				return err
			}
			if i == 2 {
				continue
			}
			level := []string{"network", "factory"}[i]
			w, err := t.ask("Type RESET to continue", "", func(s string) error { return nil })
			if err != nil {
				if err == errRestart {
					continue
				}
				return err
			}
			if strings.TrimSpace(w) != "RESET" {
				t.p("\n  Nothing was changed.\n")
				pause(t)
				continue
			}
			if saved, err := env.ResetToDefault(level); err != nil {
				t.p("\n  %s%v%s\n", red, err, reset)
			} else {
				t.p("\n  %sDone.%s A copy of the old settings is saved at %s.\n  Gatehouse restarts and the machine goes back to its starting state. Wait about a minute, then open the address shown on this screen.\n", green, reset, saved)
			}
			pause(t)
		case "7":
			t.clear()
			return nil
		}
	}
}

func pickUser(t *Term, env RecoveryEnv) (string, error) {
	users := env.Users()
	if len(users) == 0 {
		t.p("\n  %sThere are no accounts yet.%s Open the console in a browser to create one.\n", amber, reset)
		pause(t)
		return "", nil
	}
	if len(users) == 1 {
		return users[0], nil
	}
	t.p("\n")
	i, err := t.choose("Which account?", users, 1)
	if err != nil {
		return "", err
	}
	return users[i], nil
}

func pause(t *Term) { t.p("\n  Press Enter to continue. "); t.readLine(true) }

// RunRecoveryConsole runs the recovery screen on this process's own terminal.
func RunRecoveryConsole(env RecoveryEnv) error {
	rawTTY("/dev/tty", 0)
	io.WriteString(os.Stdout, palette)
	t := NewTerm(os.Stdin, os.Stdout)
	t.W, t.H = consoleSize()
	if t.W < 40 {
		t.W, t.H = 80, 24
	}
	err := RunRecovery(t, env)
	if err == errClosed {
		select {}
	}
	return err
}

var _ = strings.TrimSpace

package netd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Remote login (SSH). The console's administrators can sign in over SSH with the same name and
// password. The passwords are not copied anywhere: sshd asks Gatehouse to check them (a PAM line
// that runs `gatehouse ssh-auth`), so a changed or removed password takes effect at once. Each
// administrator also needs a Linux login of the same name, which this keeps in step.

// SSHPlan says whether remote login is on and which administrators may use it.
type SSHPlan struct {
	Enabled bool     `json:"enabled"`
	Users   []string `json:"users"`
	// Binary is the Gatehouse program the PAM line runs.
	Binary string `json:"binary"`
}

const (
	sshGroup   = "gatehouse-ssh"
	sshDropIn  = "etc/ssh/sshd_config.d/10-gatehouse.conf"
	sshPamFile = "etc/pam.d/sshd"
	pamMarker  = "# gatehouse-ssh"
)

var reLinuxUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,30}$`)

// SSHUserName returns the Linux login for a console name, and whether it can have one.
func SSHUserName(name string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if !reLinuxUser.MatchString(n) {
		return "", false
	}
	switch n {
	case "root", "daemon", "bin", "sys", "sync", "games", "man", "lp", "mail", "news", "uucp", "proxy", "www-data", "backup", "list", "irc", "nobody", "gatehouse", "sshd", "systemd-network", "messagebus":
		return "", false
	}
	return n, true
}

const sshdConf = `# Written by Gatehouse. Remote login uses the console's administrator names and passwords.
Port 22
PasswordAuthentication yes
KbdInteractiveAuthentication no
PubkeyAuthentication yes
PermitRootLogin no
AllowGroups ` + sshGroup + `
MaxAuthTries 3
LoginGraceTime 20
MaxStartups 5:50:10
UsePAM yes
X11Forwarding no
AllowTcpForwarding no
AllowAgentForwarding no
PermitTunnel no
`

func (s *Service) rootPath(p string) string {
	root := s.Root
	if root == "" {
		root = "/"
	}
	return filepath.Join(root, p)
}

// pamLine is checked first, and stops the stack when the password is right.
func pamLine(binary string) string {
	return fmt.Sprintf("auth [success=done default=die] pam_exec.so expose_authtok quiet %s ssh-auth %s", binary, pamMarker)
}

// SSHApply puts remote login in the state asked for. Turning it off stops the service and removes
// the Linux logins it made; turning it on writes the settings, makes the logins and starts it.
func (s *Service) SSHApply(p SSHPlan) error {
	if p.Enabled && (p.Binary == "" || !filepath.IsAbs(p.Binary) || strings.ContainsAny(p.Binary, " \t\n\"'\\")) {
		return errors.New("the Gatehouse program path is not usable")
	}
	pam := s.rootPath(sshPamFile)
	drop := s.rootPath(sshDropIn)
	if !p.Enabled {
		wifiRun(30*time.Second, "systemctl", "disable", "--now", "ssh.socket")
		wifiRun(30*time.Second, "systemctl", "disable", "--now", "ssh.service")
		os.Remove(drop)
		// The logins stay (locked, with their files); nothing can reach them with the server off.
		return removePamLine(pam)
	}
	if _, err := os.Stat(pam); err != nil {
		return errors.New("the SSH server is not installed on this machine")
	}
	wifiRun(10*time.Second, "groupadd", "-f", sshGroup)
	if err := os.MkdirAll(filepath.Dir(drop), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(drop, []byte(sshdConf), 0o644); err != nil {
		return err
	}
	if err := setPamLine(pam, pamLine(p.Binary)); err != nil {
		return err
	}
	if err := s.syncSSHUsers(p.Users); err != nil {
		return err
	}
	// A configuration that sshd rejects must never be left in place: check it first.
	if out, err := wifiRun(10*time.Second, "sshd", "-t"); err != nil {
		os.Remove(drop)
		removePamLine(pam)
		return fmt.Errorf("the SSH server rejected its settings: %s", strings.TrimSpace(out))
	}
	wifiRun(30*time.Second, "systemctl", "disable", "--now", "ssh.socket")
	if out, err := wifiRun(30*time.Second, "systemctl", "enable", "ssh.service"); err != nil {
		return fmt.Errorf("could not enable the SSH server: %s", strings.TrimSpace(out))
	}
	if out, err := wifiRun(30*time.Second, "systemctl", "restart", "ssh.service"); err != nil {
		return fmt.Errorf("could not start the SSH server: %s", strings.TrimSpace(out))
	}
	return nil
}

// syncSSHUsers makes the Linux logins match the administrators: the ones that should exist do, and
// logins this made for people who are no longer administrators are removed.
func (s *Service) syncSSHUsers(want []string) error {
	have := map[string]bool{}
	if out, err := wifiRun(10*time.Second, "getent", "group", sshGroup); err == nil {
		parts := strings.Split(strings.TrimSpace(out), ":")
		if len(parts) == 4 && parts[3] != "" {
			for _, u := range strings.Split(parts[3], ",") {
				have[u] = true
			}
		}
	}
	wanted := map[string]bool{}
	for _, w := range want {
		n, ok := SSHUserName(w)
		if !ok {
			continue
		}
		wanted[n] = true
		if have[n] {
			continue
		}
		if _, err := wifiRun(10*time.Second, "id", "-u", n); err == nil {
			// A login of that name already exists and is not ours: never take it over.
			continue
		}
		if out, err := wifiRun(20*time.Second, "useradd", "-m", "-s", "/bin/bash", "-G", sshGroup+",systemd-journal", "-p", "!", n); err != nil {
			return fmt.Errorf("could not make the login %s: %s", n, strings.TrimSpace(out))
		}
	}
	for u := range have {
		if !wanted[u] && reLinuxUser.MatchString(u) {
			wifiRun(10*time.Second, "loginctl", "terminate-user", u)
			wifiRun(20*time.Second, "userdel", "-r", u)
		}
	}
	return nil
}

func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// setPamLine puts the Gatehouse check at the top of sshd's login rules.
func setPamLine(path, line string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.Contains(l, pamMarker) {
			keep = append(keep, l)
		}
	}
	out := line + "\n" + strings.Join(keep, "\n")
	return writeFileAtomic(path, []byte(out), 0o644)
}

func removePamLine(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil // no SSH server, so nothing to take out
	}
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.Contains(l, pamMarker) {
			keep = append(keep, l)
		}
	}
	return writeFileAtomic(path, []byte(strings.Join(keep, "\n")), 0o644)
}

// SSHLog returns the sign-in lines sshd has written since a time (unix seconds).
func (s *Service) SSHLog(since int64) ([]string, error) {
	if since < 0 {
		since = 0
	}
	out, err := wifiRun(10*time.Second, "journalctl", "-u", "ssh.service", "--no-pager", "-o", "short-unix", "--since", fmt.Sprintf("@%d", since))
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Accepted password for") || strings.Contains(l, "Failed password for") || strings.Contains(l, "Invalid user") || strings.Contains(l, "authentication failure") {
			lines = append(lines, l)
		}
	}
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	return lines, nil
}

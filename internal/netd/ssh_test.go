package netd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sshRoot(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc/pam.d"), 0o755)
	os.WriteFile(filepath.Join(root, "etc/pam.d/sshd"), []byte("# PAM configuration for the Secure Shell service\n@include common-auth\n"), 0o644)
	return &Service{Root: root}
}

func recordRun(t *testing.T, reply func(name string, args []string) (string, error)) *[]string {
	t.Helper()
	var calls []string
	old := wifiRun
	wifiRun = func(_ time.Duration, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if reply != nil {
			return reply(name, args)
		}
		return "", nil
	}
	t.Cleanup(func() { wifiRun = old })
	return &calls
}

func TestSSHOnWritesSettingsAndPutsTheCheckFirst(t *testing.T) {
	s := sshRoot(t)
	calls := recordRun(t, func(name string, args []string) (string, error) {
		switch name {
		case "getent":
			return "gatehouse-ssh:x:900:oldadmin\n", nil
		case "id":
			return "", os.ErrNotExist // no such login yet
		}
		return "", nil
	})
	if err := s.SSHApply(SSHPlan{Enabled: true, Users: []string{"Marvin", "bad name!"}, Binary: "/usr/local/bin/gatehouse"}); err != nil {
		t.Fatal(err)
	}
	pam, _ := os.ReadFile(filepath.Join(s.Root, "etc/pam.d/sshd"))
	if !strings.HasPrefix(string(pam), "auth [success=done default=die] pam_exec.so expose_authtok quiet /usr/local/bin/gatehouse ssh-auth") {
		t.Fatalf("the check must be the first line:\n%s", pam)
	}
	conf, _ := os.ReadFile(filepath.Join(s.Root, sshDropIn))
	for _, want := range []string{"PermitRootLogin no", "AllowGroups gatehouse-ssh", "MaxAuthTries 3"} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("missing %q", want)
		}
	}
	all := strings.Join(*calls, "\n")
	if !strings.Contains(all, "useradd -m -s /bin/bash -G gatehouse-ssh,systemd-journal -p ! marvin") {
		t.Errorf("the login was not made:\n%s", all)
	}
	if strings.Contains(all, "bad name") {
		t.Error("an unusable name reached useradd")
	}
	if !strings.Contains(all, "userdel -r oldadmin") {
		t.Error("a login for someone who is no longer an administrator was kept")
	}
	// Applying again does not stack a second line.
	s.SSHApply(SSHPlan{Enabled: true, Users: []string{"marvin"}, Binary: "/usr/local/bin/gatehouse"})
	pam, _ = os.ReadFile(filepath.Join(s.Root, "etc/pam.d/sshd"))
	if strings.Count(string(pam), "gatehouse-ssh") != 1 {
		t.Fatalf("the line was added twice:\n%s", pam)
	}
}

func TestSSHOffRemovesEverythingAndStopsTheServer(t *testing.T) {
	s := sshRoot(t)
	calls := recordRun(t, nil)
	s.SSHApply(SSHPlan{Enabled: true, Users: []string{"marvin"}, Binary: "/usr/local/bin/gatehouse"})
	if err := s.SSHApply(SSHPlan{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	pam, _ := os.ReadFile(filepath.Join(s.Root, "etc/pam.d/sshd"))
	if strings.Contains(string(pam), "gatehouse") {
		t.Fatalf("the check line was left behind:\n%s", pam)
	}
	if _, err := os.Stat(filepath.Join(s.Root, sshDropIn)); err == nil {
		t.Fatal("the settings file was left behind")
	}
	if !strings.Contains(strings.Join(*calls, "\n"), "systemctl disable --now ssh.service") {
		t.Fatal("the SSH server was not stopped")
	}
}

func TestSSHRejectedSettingsAreNeverLeftInPlace(t *testing.T) {
	s := sshRoot(t)
	recordRun(t, func(name string, args []string) (string, error) {
		if name == "sshd" {
			return "bad configuration", os.ErrInvalid
		}
		return "", nil
	})
	if err := s.SSHApply(SSHPlan{Enabled: true, Binary: "/usr/local/bin/gatehouse"}); err == nil {
		t.Fatal("expected an error")
	}
	pam, _ := os.ReadFile(filepath.Join(s.Root, "etc/pam.d/sshd"))
	if strings.Contains(string(pam), "gatehouse") {
		t.Fatal("the check line stayed after sshd rejected the settings")
	}
}

func TestSSHRefusesToRunWithoutAnUsableProgramPath(t *testing.T) {
	s := sshRoot(t)
	recordRun(t, nil)
	if err := s.SSHApply(SSHPlan{Enabled: true, Binary: "gatehouse; rm -rf /"}); err == nil {
		t.Fatal("a path with shell characters was accepted")
	}
}

func TestSSHNamesThatCannotHaveALogin(t *testing.T) {
	for _, n := range []string{"root", "Bad Name", "a.b", "", "gatehouse"} {
		if _, ok := SSHUserName(n); ok {
			t.Errorf("%q was accepted", n)
		}
	}
	if l, ok := SSHUserName("Marvin"); !ok || l != "marvin" {
		t.Fatalf("%q %v", l, ok)
	}
}

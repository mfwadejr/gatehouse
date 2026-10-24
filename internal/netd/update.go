package netd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gatehouse/internal/update"
)

// Where updates live. Downloads arrive in a folder the console can write; everything the helper
// trusts is copied into folders only root can write, and the outcome is written where the console
// can read it but not change it.
var (
	UpdateInbox  = "/var/lib/gatehouse/update"
	UpdateStage  = "/var/lib/gatehouse-update/stage"
	UpdateState  = "/var/lib/gatehouse-update"
	ReleaseFile  = "/etc/gatehouse-release"
	aptAutoFile  = "/etc/apt/apt.conf.d/99gatehouse-auto-upgrades"
	runUpdateCmd = func(self string) error {
		return exec.Command("systemd-run", "--unit=gatehouse-update", "--collect", self, "update-apply").Run()
	}
)

func currentRelease() string {
	b, err := os.ReadFile(ReleaseFile)
	if err != nil {
		return "0.0.0"
	}
	return strings.TrimSpace(string(b))
}

// UpdateStart checks a downloaded update again, as root, and starts installing it. The install
// runs as its own service, because it restarts the console and this helper.
func (s *Service) UpdateStart(path string) error {
	pub, err := update.EmbeddedKey()
	if err != nil {
		return err
	}
	clean := filepath.Clean(path)
	if filepath.Dir(clean) != filepath.Clean(UpdateInbox) || !strings.HasSuffix(clean, ".tar.gz") {
		return errors.New("the update file is not where updates are kept")
	}
	read := func(p string) ([]byte, error) {
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			return nil, errors.New("the update file is missing or not a plain file")
		}
		return os.ReadFile(p)
	}
	data, err := read(clean)
	if err != nil {
		return err
	}
	sig, err := read(clean + ".sig")
	if err != nil {
		return err
	}
	p, err := update.Open(data, string(sig), pub)
	if err != nil {
		return err
	}
	if update.Compare(p.Manifest.Version, currentRelease()) <= 0 {
		return fmt.Errorf("version %s is not newer than the installed version %s", p.Manifest.Version, currentRelease())
	}
	if st := update.ReadState(UpdateState); st != "idle" {
		return update.ErrBusy
	}
	if err := os.MkdirAll(UpdateState, 0o755); err != nil {
		return err
	}
	os.RemoveAll(UpdateStage)
	if err := os.MkdirAll(UpdateStage, 0o700); err != nil {
		return err
	}
	// The copies are the bytes that were just checked, so nothing can change them in between.
	if err := os.WriteFile(filepath.Join(UpdateStage, "package.tar.gz"), data, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(UpdateStage, "package.sig"), sig, 0o600); err != nil {
		return err
	}
	os.WriteFile(filepath.Join(UpdateState, "state"), []byte("starting\n"), 0o644)
	self, err := os.Executable()
	if err != nil {
		self = update.BinaryDest
	}
	if err := runUpdateCmd(self); err != nil {
		os.WriteFile(filepath.Join(UpdateState, "state"), []byte("idle\n"), 0o644)
		return fmt.Errorf("could not start the installer: %v", err)
	}
	return nil
}

// OSSecurity switches automatic installation of Debian security updates on or off.
func (s *Service) OSSecurity(enabled bool) error {
	v := "0"
	if enabled {
		v = "1"
	}
	conf := "// Written by Gatehouse. Switch this on the Updates page.\n" +
		"APT::Periodic::Update-Package-Lists \"1\";\n" +
		"APT::Periodic::Unattended-Upgrade \"" + v + "\";\n" +
		"Unattended-Upgrade::Automatic-Reboot \"false\";\n"
	if err := os.MkdirAll(filepath.Dir(aptAutoFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(aptAutoFile, []byte(conf), 0o644); err != nil {
		return err
	}
	if enabled {
		exec.Command("systemctl", "enable", "--now", "apt-daily.timer", "apt-daily-upgrade.timer").Run()
	}
	return nil
}

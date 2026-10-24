package netd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gatehouse/internal/update"
)

// The operating system half of an update. Gatehouse and Debian update together: a release names
// the Debian packages it needs, and the Debian updates for the release the machine is on come
// with it. The machine stays on Debian 13 ("trixie"): the installed image names that release in
// its apt sources, and nothing here runs unless every source still does, so a source that says
// "stable" (which would change meaning when Debian 14 comes out) stops the system step.

const debianRelease = "trixie"

var (
	osReleaseFile  = "/etc/os-release"
	aptSourceFiles = func() []string {
		files := []string{"/etc/apt/sources.list"}
		for _, g := range []string{"*.list", "*.sources"} {
			m, _ := filepath.Glob("/etc/apt/sources.list.d/" + g)
			files = append(files, m...)
		}
		return files
	}
	// aptRun runs apt or dpkg with no questions asked and returns what it printed.
	aptRun = func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C", "APT_LISTCHANGES_FRONTEND=none")
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		err := cmd.Run()
		return buf.Bytes(), err
	}
)

// SystemUpdates is what the operating system has waiting.
type SystemUpdates struct {
	Count        int      `json:"count"`
	Names        []string `json:"names"`
	RebootLikely bool     `json:"reboot_likely"`
}

func aptArgs(more ...string) []string {
	base := []string{"-y",
		"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold",
		"-o", "DPkg::Lock::Timeout=300"}
	return append(base, more...)
}

// sourcesStayOnTrixie checks that every apt source names the Debian 13 release by its name.
func sourcesStayOnTrixie() error {
	found := 0
	for _, f := range aptSourceFiles() {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			var suites []string
			switch {
			case strings.HasPrefix(l, "deb ") || strings.HasPrefix(l, "deb-src "):
				fields := strings.Fields(l)[1:]
				if len(fields) > 0 && strings.HasPrefix(fields[0], "[") { // options like [arch=amd64]
					for len(fields) > 0 && !strings.HasSuffix(fields[0], "]") {
						fields = fields[1:]
					}
					if len(fields) > 0 {
						fields = fields[1:]
					}
				}
				if len(fields) >= 2 {
					suites = fields[1:2]
				}
			case strings.HasPrefix(l, "Suites:"):
				suites = strings.Fields(strings.TrimPrefix(l, "Suites:"))
			}
			for _, st := range suites {
				found++
				if !strings.HasPrefix(st, debianRelease) {
					return fmt.Errorf("%s follows %q, which could move this machine off Debian 13, so system updates are not installed", f, st)
				}
			}
		}
	}
	if found == 0 {
		return errors.New("no Debian update sources are set up on this machine")
	}
	return nil
}

// onDebian13 refuses to touch a machine that is not on Debian 13.
func onDebian13() error {
	b, err := os.ReadFile(osReleaseFile)
	if err != nil {
		return errors.New("cannot tell which Debian release this machine runs")
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VERSION_ID=") {
			if strings.Trim(strings.TrimPrefix(l, "VERSION_ID="), `"`) != "13" {
				return errors.New("this machine is not on Debian 13, so system updates are not installed")
			}
			return sourcesStayOnTrixie()
		}
	}
	return errors.New("cannot tell which Debian release this machine runs")
}

var reInst = regexp.MustCompile(`^Inst (\S+)`)
var reSetup = regexp.MustCompile(`(?m)^Setting up (\S+)`)

// parseInst lists the packages a simulated upgrade would install or upgrade.
func parseInst(out []byte) []string {
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if m := reInst.FindStringSubmatch(l); m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

func rebootLikely(names []string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, "linux-image") || n == "libc6" || strings.HasPrefix(n, "systemd") {
			return true
		}
	}
	return false
}

// noAutoUpgrade makes sure Debian's own unattended upgrades are off, so the machine has one
// update path and it is this one.
func noAutoUpgrade() {
	conf := "// Written by Gatehouse. System updates are installed together with Gatehouse updates.\n" +
		"APT::Periodic::Update-Package-Lists \"1\";\n" +
		"APT::Periodic::Unattended-Upgrade \"0\";\n" +
		"Unattended-Upgrade::Automatic-Reboot \"false\";\n"
	os.MkdirAll(filepath.Dir(aptAutoFile), 0o755)
	os.WriteFile(aptAutoFile, []byte(conf), 0o644)
}

// SystemCheck refreshes the package lists and says what the operating system has waiting.
func (s *Service) SystemCheck() (SystemUpdates, error) {
	var out SystemUpdates
	if err := onDebian13(); err != nil {
		return out, err
	}
	if b, err := aptRun(3*time.Minute, "apt-get", aptArgs("update")...); err != nil {
		return out, fmt.Errorf("could not reach Debian's update servers: %s", lastLine(b, err))
	}
	b, err := aptRun(2*time.Minute, "apt-get", aptArgs("-s", "--no-remove", "full-upgrade")...)
	if err != nil {
		return out, fmt.Errorf("could not work out the system updates: %s", lastLine(b, err))
	}
	names := parseInst(b)
	out.Count, out.RebootLikely = len(names), rebootLikely(names)
	if len(names) > 25 {
		names = names[:25]
	}
	out.Names = names
	return out, nil
}

func lastLine(b []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// SystemUpgrade is the system step of an update run, as root, inside the update service. It
// installs the packages a release needs, then applies the Debian updates for the release the
// machine is on. If Debian's servers cannot be reached it skips the updates and says so, unless
// the release needs a package, in which case it stops before anything is changed.
func SystemUpgrade(required []string) (update.SystemResult, error) {
	var res update.SystemResult
	if err := onDebian13(); err != nil {
		if len(required) > 0 {
			return res, err
		}
		res.Skipped = err.Error()
		return res, nil
	}
	noAutoUpgrade()
	aptRun(5*time.Minute, "dpkg", "--configure", "-a") // finish anything an earlier run left half done
	if b, err := aptRun(3*time.Minute, "apt-get", aptArgs("update")...); err != nil {
		why := "Debian's update servers could not be reached: " + lastLine(b, err)
		if len(required) > 0 {
			return res, fmt.Errorf("this release needs %s, but %s", strings.Join(required, ", "), why)
		}
		res.Skipped = why
		return res, nil
	}
	if len(required) > 0 {
		b, err := aptRun(30*time.Minute, "apt-get", aptArgs(append([]string{"install", "--no-install-recommends"}, required...)...)...)
		if err != nil {
			return res, fmt.Errorf("could not install %s: %s", strings.Join(required, ", "), lastLine(b, err))
		}
		res.Packages += len(reSetup.FindAllSubmatch(b, -1))
	}
	b, err := aptRun(60*time.Minute, "apt-get", aptArgs("--no-remove", "full-upgrade")...)
	if err != nil {
		return res, fmt.Errorf("the Debian updates failed: %s", lastLine(b, err))
	}
	res.Packages += len(reSetup.FindAllSubmatch(b, -1))
	if err := onDebian13(); err != nil {
		return res, errors.New("the machine no longer reports Debian 13 after the updates; check it before restarting")
	}
	res.RebootNeeded = update.RebootNeeded()
	return res, nil
}

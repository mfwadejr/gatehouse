package netd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const simulated = `Reading package lists...
Inst libc6 [2.41-12] (2.41-12+deb13u1 Debian-Security:13/stable-security [amd64])
Inst openssl [3.5.1-1] (3.5.1-1+deb13u1 Debian-Security:13/stable-security [amd64])
Inst linux-image-6.12.57+deb13-amd64 (6.12.57-1 Debian:13.2/stable [amd64])
Conf libc6 (2.41-12+deb13u1 Debian-Security:13/stable-security [amd64])
`

func TestParseInstAndRebootHint(t *testing.T) {
	names := parseInst([]byte(simulated))
	if strings.Join(names, ",") != "libc6,openssl,linux-image-6.12.57+deb13-amd64" {
		t.Fatalf("%v", names)
	}
	if !rebootLikely(names) || rebootLikely([]string{"openssl", "curl"}) {
		t.Fatal("reboot hint wrong")
	}
}

type aptLog struct {
	calls []string
	fail  string // substring of a command that fails
	out   map[string]string
}

func fakeApt(t *testing.T, l *aptLog, release string) {
	dir := t.TempDir()
	osReleaseFile = filepath.Join(dir, "os-release")
	src := filepath.Join(dir, "sources.list")
	os.WriteFile(src, []byte("deb http://deb.debian.org/debian trixie main\ndeb [arch=amd64 signed-by=/x] http://security.debian.org/debian-security trixie-security main\ndeb http://deb.debian.org/debian trixie-updates main\n"), 0o644)
	aptSourceFiles = func() []string { return []string{src} }
	os.WriteFile(osReleaseFile, []byte("PRETTY_NAME=\"x\"\nVERSION_ID=\""+release+"\"\n"), 0o644)
	aptAutoFile = filepath.Join(dir, "99auto")
	old := aptRun
	t.Cleanup(func() { aptRun = old })
	aptRun = func(_ time.Duration, name string, args ...string) ([]byte, error) {
		c := name + " " + strings.Join(args, " ")
		l.calls = append(l.calls, c)
		if l.fail != "" && strings.Contains(c, l.fail) {
			return []byte("E: it broke\n"), errors.New("exit 100")
		}
		for k, v := range l.out {
			if strings.Contains(c, k) {
				return []byte(v), nil
			}
		}
		return nil, nil
	}
}

func TestSystemCheckCountsWaitingUpdates(t *testing.T) {
	l := &aptLog{out: map[string]string{"-s": simulated}}
	fakeApt(t, l, "13")
	su, err := (&Service{}).SystemCheck()
	if err != nil || su.Count != 3 || !su.RebootLikely || len(su.Names) != 3 {
		t.Fatalf("%+v %v", su, err)
	}
	for _, c := range l.calls {
		if !strings.Contains(c, "force-confold") {
			t.Fatalf("apt could stop to ask a question: %s", c)
		}
	}
}

func TestSystemCheckRefusesOtherDebianReleases(t *testing.T) {
	l := &aptLog{}
	fakeApt(t, l, "14")
	if _, err := (&Service{}).SystemCheck(); err == nil || len(l.calls) != 0 {
		t.Fatalf("looked for updates on the wrong release: %v %v", err, l.calls)
	}
	if res, err := SystemUpgrade(nil); err != nil || res.Skipped == "" || len(l.calls) != 0 {
		t.Fatalf("%+v %v %v", res, err, l.calls)
	}
	if _, err := SystemUpgrade([]string{"keepalived"}); err == nil {
		t.Fatal("a release that needs packages must stop on the wrong Debian release")
	}
}

func TestSystemUpgradeOrderAndCounts(t *testing.T) {
	l := &aptLog{out: map[string]string{"install --no-install-recommends": "Setting up keepalived (1)\n", "full-upgrade": "Setting up libc6 (2)\nSetting up openssl (3)\n"}}
	fakeApt(t, l, "13")
	res, err := SystemUpgrade([]string{"keepalived"})
	if err != nil || res.Packages != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	joined := strings.Join(l.calls, "\n")
	iUpdate, iInstall, iUpgrade := strings.Index(joined, " update"), strings.Index(joined, "install --no-install-recommends keepalived"), strings.Index(joined, "--no-remove full-upgrade")
	if !(iUpdate >= 0 && iUpdate < iInstall && iInstall < iUpgrade) {
		t.Fatalf("order wrong:\n%s", joined)
	}
	if b, _ := os.ReadFile(aptAutoFile); !strings.Contains(string(b), `Unattended-Upgrade "0"`) {
		t.Fatal("Debian's own unattended upgrades were not switched off")
	}
}

func TestSystemUpgradeWhenDebianIsUnreachable(t *testing.T) {
	l := &aptLog{fail: "update"}
	fakeApt(t, l, "13")
	res, err := SystemUpgrade(nil)
	if err != nil || !strings.Contains(res.Skipped, "could not be reached") {
		t.Fatalf("the Gatehouse part should still go ahead: %+v %v", res, err)
	}
	if _, err := SystemUpgrade([]string{"keepalived"}); err == nil || !strings.Contains(err.Error(), "keepalived") {
		t.Fatalf("a release that needs a package must not go ahead without it: %v", err)
	}
}

func TestSystemUpgradeFailureIsReported(t *testing.T) {
	l := &aptLog{fail: "full-upgrade"}
	fakeApt(t, l, "13")
	if _, err := SystemUpgrade(nil); err == nil || !strings.Contains(err.Error(), "it broke") {
		t.Fatalf("%v", err)
	}
}

func TestSourcesMustNameTrixie(t *testing.T) {
	l := &aptLog{}
	fakeApt(t, l, "13")
	bad := filepath.Join(t.TempDir(), "x.list")
	for _, line := range []string{"deb http://deb.debian.org/debian stable main", "deb [arch=amd64] http://deb.debian.org/debian forky main", "deb http://deb.debian.org/debian testing main"} {
		os.WriteFile(bad, []byte(line+"\n"), 0o644)
		aptSourceFiles = func() []string { return []string{bad} }
		if err := onDebian13(); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
	os.WriteFile(bad, []byte("Types: deb\nURIs: http://deb.debian.org/debian\nSuites: trixie trixie-updates\nComponents: main\n"), 0o644)
	if err := onDebian13(); err != nil {
		t.Fatalf("refused a good .sources file: %v", err)
	}
	os.WriteFile(bad, []byte("Suites: trixie stable\n"), 0o644)
	if err := onDebian13(); err == nil {
		t.Fatal("accepted a .sources file with a stable suite")
	}
}

func TestNoSourcesIsAnError(t *testing.T) {
	l := &aptLog{}
	fakeApt(t, l, "13")
	aptSourceFiles = func() []string { return nil }
	if err := onDebian13(); err == nil || !strings.Contains(err.Error(), "no Debian update sources") {
		t.Fatalf("%v", err)
	}
}

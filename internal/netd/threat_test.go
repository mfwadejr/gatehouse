package netd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gatehouse/internal/threatcfg"
)

// fakeThreatBox points the service at a temporary root and fakes the commands it would run.
func fakeThreatBox(t *testing.T) (*Service, *[]string) {
	t.Helper()
	root := t.TempDir()
	s := &Service{Root: root}
	var calls []string
	fakeApt(t, &aptLog{}, "13") // the Debian 13 guard reads these files
	oldRun, oldApt := wifiRun, aptRun
	t.Cleanup(func() { wifiRun, aptRun = oldRun, oldApt; setThreat("idle", "") })
	wifiRun = func(_ time.Duration, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "suricata-update" {
			os.MkdirAll(filepath.Join(root, "var/lib/suricata/rules"), 0o755)
			os.WriteFile(filepath.Join(root, threatRules), []byte("# comment\nalert tcp any any -> any any (msg:\"a\"; sid:1;)\nalert tcp any any -> any any (msg:\"b\"; sid:2;)\n"), 0o644)
		}
		return "", nil
	}
	aptRun = func(_ time.Duration, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte("After this operation, 87.5 MB of additional disk space will be used.\nNeed to get 21.4 MB of archives.\n"), nil
	}
	return s, &calls
}

func installFakeBinary(t *testing.T, s *Service) {
	t.Helper()
	p := s.rootPath(threatBinary)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("x"), 0o755)
}

func TestThreatApplyWritesLimitedWatchOnlyService(t *testing.T) {
	s, calls := fakeThreatBox(t)
	plan := threatcfg.Plan{Enabled: true, Ifaces: []string{"lan0", "lan1.20"}, Preset: "balanced", CPUPercent: 40, MemoryMB: 512}
	if err := s.ThreatApply(plan); err == nil {
		t.Fatal("applied without the program installed")
	}
	installFakeBinary(t, s)
	if err := s.ThreatApply(threatcfg.Plan{Enabled: true, Ifaces: []string{"lan0; rm -rf /"}}); err == nil {
		t.Fatal("a bad port name was accepted")
	}
	if err := s.ThreatApply(plan); err != nil {
		t.Fatal(err)
	}
	unit := readOrEmpty(s.rootPath(threatcfg.UnitFile))
	for _, want := range []string{"-i lan0 -i lan1.20", "CPUQuota=40%", "MemoryMax=512M", "--include /etc/gatehouse/suricata.yaml"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "-q ") || strings.Contains(unit, "nfqueue") {
		t.Error("the engine must only watch, never sit in the packet path")
	}
	dis := readOrEmpty(s.rootPath(threatcfg.DisableConf))
	if !strings.Contains(dis, "group:emerging-p2p.rules") || strings.Contains(dis, "group:emerging-malware.rules") {
		t.Errorf("balanced preset groups wrong:\n%s", dis)
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "suricata-update --no-reload --disable-conf") || !strings.Contains(joined, "systemctl restart gatehouse-suricata.service") {
		t.Errorf("expected a rule update and a start:\n%s", joined)
	}
	// Turning it off stops it and leaves the program installed.
	*calls = nil
	if err := s.ThreatApply(threatcfg.Plan{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(*calls, "\n"), "disable --now gatehouse-suricata.service") {
		t.Errorf("not stopped: %v", *calls)
	}
	if !s.threatInstalled() {
		t.Fatal("turning it off removed the program")
	}
}

func waitThreatIdle(t *testing.T, want string) {
	t.Helper()
	for i := 0; i < 300; i++ {
		threatState.Lock()
		st := threatState.state
		threatState.Unlock()
		if st == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never reached %q", want)
}

func TestThreatInstallShowsSizeThenInstallsWithoutStartingItsOwnService(t *testing.T) {
	s, calls := fakeThreatBox(t)
	p, err := s.ThreatInstallPreview()
	if err != nil || p.DiskMB != 87.5 || p.DownloadMB != 21.4 || len(p.Packages) != 2 {
		t.Fatalf("preview: %+v %v", p, err)
	}
	if strings.Contains(strings.Join(*calls, "\n"), "apt-get -y") && strings.Contains(strings.Join(*calls, "\n"), "install --no-install-recommends suricata suricata-update") && !strings.Contains(strings.Join(*calls, "\n"), " -s ") {
		t.Fatal("the preview must not install anything")
	}
	// the install itself: the fake apt "installs" the program
	aptRun = func(_ time.Duration, name string, args ...string) ([]byte, error) {
		c := name + " " + strings.Join(args, " ")
		*calls = append(*calls, c)
		if strings.Contains(c, "install --no-install-recommends suricata") && !strings.Contains(c, " -s ") {
			installFakeBinary(t, s)
		}
		return nil, nil
	}
	*calls = nil
	if err := s.ThreatInstallStart(); err != nil {
		t.Fatal(err)
	}
	waitThreatIdle(t, "idle")
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "disable --now suricata.service") {
		t.Errorf("the package's own service must be switched off:\n%s", joined)
	}
	if !s.threatInstalled() {
		t.Fatal("not installed")
	}
	if err := s.ThreatInstallStart(); err == nil {
		t.Fatal("installed twice")
	}
	// removing it
	if err := s.ThreatRemove(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(*calls, "\n"), "remove suricata suricata-update") {
		t.Errorf("not removed: %v", *calls)
	}
	waitThreatIdle(t, "idle")
}

func TestThreatInstallFailureIsPlainAndHarmless(t *testing.T) {
	s, _ := fakeThreatBox(t)
	aptRun = func(_ time.Duration, name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "install") && !strings.Contains(strings.Join(args, " "), "-s") {
			return []byte("E: Unable to locate package suricata\n"), errors.New("exit 100")
		}
		return nil, nil
	}
	if err := s.ThreatInstallStart(); err != nil {
		t.Fatal(err)
	}
	waitThreatIdle(t, "failed")
	info := s.ThreatStatus()
	if info.State != "failed" || !strings.Contains(info.Message, "Unable to locate package") || info.Installed {
		t.Fatalf("%+v", info)
	}
	// a second try is allowed after a failure
	aptRun = func(_ time.Duration, name string, args ...string) ([]byte, error) { return nil, nil }
	if err := s.ThreatInstallStart(); err != nil {
		t.Fatalf("could not retry: %v", err)
	}
	// the retry runs in the background; let it finish before the test folder is removed
	waitThreatIdle(t, "failed")
}

func TestThreatInstallRefusedOffDebian13(t *testing.T) {
	s, _ := fakeThreatBox(t)
	os.WriteFile(osReleaseFile, []byte("VERSION_ID=\"12\"\n"), 0o644)
	if _, err := s.ThreatInstallPreview(); err == nil {
		t.Fatal("preview allowed off Debian 13")
	}
	if err := s.ThreatInstallStart(); err == nil {
		t.Fatal("install allowed off Debian 13")
	}
}

func TestThreatRulesRollBackAndKeepOldOnFailure(t *testing.T) {
	s, _ := fakeThreatBox(t)
	installFakeBinary(t, s)
	rules := s.rootPath(threatRules)
	os.MkdirAll(filepath.Dir(rules), 0o755)
	os.WriteFile(rules, []byte("alert tcp any any -> any any (msg:\"old\"; sid:7;)\n"), 0o644)
	// a failing update keeps the old rules
	wifiRun = func(_ time.Duration, name string, args ...string) (string, error) {
		if name == "suricata-update" {
			os.WriteFile(rules, []byte("broken"), 0o644)
			return "download failed", os.ErrInvalid
		}
		return "", nil
	}
	if err := s.ThreatUpdateRules(); err == nil {
		t.Fatal("a failed update said it worked")
	}
	if got := readOrEmpty(rules); !strings.Contains(got, "sid:7") {
		t.Fatalf("old rules lost: %q", got)
	}
	// a good update, then roll back to the one before
	wifiRun = func(_ time.Duration, name string, args ...string) (string, error) {
		if name == "suricata-update" {
			os.WriteFile(rules, []byte("alert tcp any any -> any any (msg:\"new\"; sid:8;)\n"), 0o644)
		}
		return "", nil
	}
	if err := s.ThreatUpdateRules(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readOrEmpty(rules), "sid:8") {
		t.Fatal("new rules not in place")
	}
	if err := s.ThreatRollback(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readOrEmpty(rules), "sid:7") {
		t.Fatal("rollback did not restore the old rules")
	}
	info := s.ThreatStatus()
	if !info.HasPrevious || info.RulesCount != 1 {
		t.Fatalf("status: %+v", info)
	}
}

// Debian's simulated install may print no totals; the sizes then come from the package list.
func TestThreatPreviewWorksOutSizesWhenAptPrintsNoTotals(t *testing.T) {
	s, _ := fakeThreatBox(t)
	aptRun = func(_ time.Duration, name string, args ...string) ([]byte, error) {
		c := name + " " + strings.Join(args, " ")
		switch {
		case strings.Contains(c, "--print-uris"):
			return []byte("'http://deb.debian.org/debian/pool/main/s/suricata/suricata_7.deb' suricata_7.deb 10485760 SHA256:aa\n'http://deb.debian.org/debian/pool/main/s/suricata-update/su.deb' su.deb 5242880 SHA256:bb\n"), nil
		case strings.HasPrefix(c, "apt-cache show"):
			return []byte("Package: suricata\nInstalled-Size: 51200\n\nPackage: suricata-update\nInstalled-Size: 10240\n"), nil
		}
		return []byte("Inst suricata (7 Debian:13/stable [amd64])\n"), nil
	}
	p, err := s.ThreatInstallPreview()
	if err != nil || p.DownloadMB != 15 || p.DiskMB != 60 {
		t.Fatalf("%+v %v", p, err)
	}
}

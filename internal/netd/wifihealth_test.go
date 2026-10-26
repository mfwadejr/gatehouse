package netd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeWifi stands in for the real tools. fail makes the access point look hung.
type fakeWifi struct {
	fail bool
	log  []string
}

func (f *fakeWifi) run(_ time.Duration, name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.log = append(f.log, cmd)
	switch {
	case name == "systemctl" && args[0] == "is-active":
		return "active\n", nil
	case name == "iw" && len(args) > 2 && args[2] == "info":
		return "type AP\n", nil
	case name == "hostapd_cli":
		return "state=ENABLED\n", nil
	case name == "iw" && len(args) > 2 && args[2] == "survey":
		if f.fail {
			return "", errors.New("command failed: Device or resource busy (-16)")
		}
		return "frequency: 2412 MHz\n", nil
	case name == "iw" && len(args) > 2 && args[2] == "station":
		return "Station aa:bb:cc:dd:ee:01 (on wlp3s0)\n", nil
	case name == "iw" && len(args) > 2 && args[2] == "get":
		return "Power save: on\n", nil
	}
	return "", nil
}

func withFakeWifi(t *testing.T) (*fakeWifi, *Service) {
	t.Helper()
	f := &fakeWifi{}
	oldRun, oldSleep := wifiRun, wifiSleep
	wifiRun, wifiSleep = f.run, func(time.Duration) {}
	whStore = map[string]*whState{}
	t.Cleanup(func() { wifiRun, wifiSleep = oldRun, oldSleep })
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sys/class/net/wlp3s0/device/driver"), 0o755)
	os.Symlink("../../../../module/rtw88_8822ce", filepath.Join(root, "sys/class/net/wlp3s0/device/driver/module"))
	return f, &Service{Root: root}
}

func TestHealthyCardIsLeftAloneAndKeptAwake(t *testing.T) {
	f, s := withFakeWifi(t)
	st, _ := s.WifiHealth([]string{"wlp3s0"}, true)
	if len(st) != 1 || !st[0].OK || st[0].Stations != 1 || st[0].Action != "" {
		t.Fatalf("%+v", st)
	}
	joined := strings.Join(f.log, "\n")
	if !strings.Contains(joined, "set power_save off") || strings.Contains(joined, "restart") {
		t.Fatalf("calls:\n%s", joined)
	}
}

func TestHungCardIsRestartedThenItsDriverReloaded(t *testing.T) {
	f, s := withFakeWifi(t)
	f.fail = true
	var acts []string
	for i := 0; i < 20; i++ {
		st, _ := s.WifiHealth([]string{"wlp3s0"}, false)
		if st[0].Action != "" {
			acts = append(acts, st[0].Action)
		}
	}
	if len(acts) < 2 || !strings.Contains(acts[0], "restarted the Wi-Fi service") || strings.Contains(acts[0], "driver (") {
		t.Fatalf("first step must only restart the service: %v", acts)
	}
	if !strings.Contains(acts[1], "reloaded the card's driver (rtw88_8822ce)") {
		t.Fatalf("second step must reload the driver: %v", acts)
	}
	if !strings.Contains(strings.Join(f.log, "\n"), "modprobe -r rtw88_8822ce") {
		t.Fatal("the driver was not unloaded")
	}
}

func TestRecoveryGivesUpAfterFourStepsAnHour(t *testing.T) {
	f, s := withFakeWifi(t)
	f.fail = true
	var gaveUp bool
	steps := 0
	for i := 0; i < 80; i++ {
		st, _ := s.WifiHealth([]string{"wlp3s0"}, false)
		if st[0].GaveUp {
			gaveUp = true
		} else if st[0].Action != "" {
			steps++
		}
	}
	if !gaveUp || steps != whMaxPerHour {
		t.Fatalf("gaveUp=%v steps=%d", gaveUp, steps)
	}
}

func TestPowerSaveOptionsOnlyForOptionsTheDriverHas(t *testing.T) {
	_, s := withFakeWifi(t)
	os.MkdirAll(filepath.Join(s.Root, "sys/module/rtw88_pci/parameters"), 0o755)
	os.WriteFile(filepath.Join(s.Root, "sys/module/rtw88_pci/parameters/disable_aspm"), []byte("N\n"), 0o644)
	if err := s.WifiPowerSave("wlp3s0", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Root, "etc/modprobe.d/gatehouse-wifi.conf"))
	if !strings.Contains(string(b), "options rtw88_pci disable_aspm=y") || strings.Contains(string(b), "disable_lps_deep") {
		t.Fatalf("%s", b)
	}
	if err := s.WifiPowerSave("wlp3s0", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "etc/modprobe.d/gatehouse-wifi.conf")); err == nil {
		t.Fatal("the options were not removed when switched off")
	}
}

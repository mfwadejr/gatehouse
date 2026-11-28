package app

import (
	"os"
	"path/filepath"
	"testing"
)

func hwWrite(t *testing.T, root, dir string, files map[string]string) {
	t.Helper()
	d := filepath.Join(root, dir)
	os.MkdirAll(d, 0o755)
	for n, v := range files {
		os.WriteFile(filepath.Join(d, n), []byte(v+"\n"), 0o644)
	}
}

// The reading from the real box: coretemp, the board's ACPI zones and a Winbond chip with stray alarms,
// an odd cold sensor, working fans and empty headers.
func TestHardwareReading(t *testing.T) {
	root := t.TempDir()
	old := hwmonRoot
	hwmonRoot = root
	defer func() { hwmonRoot = old }()
	hwWrite(t, root, "hwmon0", map[string]string{"name": "acpitz", "temp1_input": "27800", "temp2_input": "29800"})
	hwWrite(t, root, "hwmon1", map[string]string{"name": "coretemp", "temp1_input": "30000", "temp1_label": "Package id 0", "temp1_max": "80000", "temp1_crit": "100000",
		"temp2_input": "30000", "temp2_label": "Core 0", "temp3_input": "31000", "temp3_label": "Core 1"})
	hwWrite(t, root, "hwmon2", map[string]string{"name": "w83627dhg", "temp1_input": "31000", "temp1_max": "15000", "temp2_input": "17500", "temp2_max": "80000", "temp3_input": "31500",
		"fan1_input": "11250", "fan2_input": "0", "fan3_input": "10227", "in0_input": "1750"})
	a := &App{}
	r := a.readHardware()
	if !r.HasFans {
		t.Fatal("a fan chip was not noticed")
	}
	var cpu, board int
	for _, x := range r.Temps {
		if x.Kind == "cpu" {
			cpu++
			if x.C != 30 || x.High != 80 || x.Crit != 100 {
				t.Fatalf("processor: %+v", x)
			}
		} else {
			board++
			if x.C < 20 {
				t.Fatalf("a cold sensor was shown: %+v", x)
			}
			if x.High != 80 { // the chip's 15 degree limit is an unset default
				t.Fatalf("board limit: %+v", x)
			}
		}
	}
	if cpu != 1 || board != 4 {
		t.Fatalf("temps cpu=%d board=%d: %+v", cpu, board, r.Temps)
	}
	if len(r.Fans) != 2 { // the fan reading zero that never turned is an empty header
		t.Fatalf("fans: %+v", r.Fans)
	}
	// A fan that was turning and now reads zero is a stopped fan.
	os.WriteFile(filepath.Join(root, "hwmon2", "fan1_input"), []byte("0\n"), 0o644)
	r = a.readHardware()
	stopped := 0
	for _, f := range r.Fans {
		if f.State == "stopped" {
			stopped++
		}
	}
	if stopped != 1 {
		t.Fatalf("a stopped fan was not reported: %+v", r.Fans)
	}
}

// With no fan chip nothing about fans appears; with no sensors at all the reading is empty.
func TestHardwareWithoutFanChip(t *testing.T) {
	root := t.TempDir()
	old := hwmonRoot
	hwmonRoot = root
	defer func() { hwmonRoot = old }()
	hwWrite(t, root, "hwmon0", map[string]string{"name": "coretemp", "temp1_input": "45000", "temp1_label": "Package id 0"})
	r := (&App{}).readHardware()
	if r.HasFans || len(r.Fans) != 0 || len(r.Temps) != 1 {
		t.Fatalf("%+v", r)
	}
	os.RemoveAll(filepath.Join(root, "hwmon0"))
	r = (&App{}).readHardware()
	if len(r.Temps) != 0 || len(r.Fans) != 0 {
		t.Fatalf("%+v", r)
	}
}

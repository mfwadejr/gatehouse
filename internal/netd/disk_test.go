package netd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parseSmart(t *testing.T, s string) smartJSON {
	t.Helper()
	var j smartJSON
	if err := json.Unmarshal([]byte(s), &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestDiskVerdicts(t *testing.T) {
	cases := []struct {
		name, json, word, level string
		hdd                     bool
	}{
		{"healthy nvme", `{"smart_status":{"passed":true},"temperature":{"current":41},"nvme_smart_health_information_log":{"percentage_used":3,"available_spare":100,"available_spare_threshold":10,"power_on_hours":900}}`, "Healthy", "", false},
		{"worn nvme", `{"smart_status":{"passed":true},"nvme_smart_health_information_log":{"percentage_used":84,"available_spare":100,"available_spare_threshold":10}}`, "Worn", "info", false},
		{"very worn nvme", `{"smart_status":{"passed":true},"nvme_smart_health_information_log":{"percentage_used":95,"available_spare":100,"available_spare_threshold":10}}`, "Worn", "warn", false},
		{"failing", `{"smart_status":{"passed":false},"temperature":{"current":30}}`, "Warning", "bad", true},
		{"media errors", `{"smart_status":{"passed":true},"nvme_smart_health_information_log":{"media_errors":2,"available_spare":100,"available_spare_threshold":10}}`, "Warning", "warn", false},
		{"pending sectors", `{"smart_status":{"passed":true},"ata_smart_attributes":{"table":[{"id":197,"raw":{"value":4}}]}}`, "Warning", "warn", true},
		{"moved sectors", `{"smart_status":{"passed":true},"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":8}}]}}`, "Warning", "warn", true},
		{"hot hdd", `{"smart_status":{"passed":true},"temperature":{"current":62}}`, "Warning", "warn", true},
		{"same heat fine on ssd", `{"smart_status":{"passed":true},"temperature":{"current":62}}`, "Healthy", "", false},
		{"sata ssd wear from attribute", `{"smart_status":{"passed":true},"ata_smart_attributes":{"table":[{"id":233,"value":5,"raw":{"value":0}}]}}`, "Worn", "warn", false},
	}
	for _, c := range cases {
		d := Drive{}
		judge(&d, parseSmart(t, c.json), c.hdd)
		if d.Word != c.word || d.Level != c.level {
			t.Errorf("%s: got %s/%s (%s) want %s/%s", c.name, d.Word, d.Level, d.Reason, c.word, c.level)
		}
	}
	// A drive that reports nothing about temperature or wear says so.
	d := Drive{}
	judge(&d, parseSmart(t, `{"smart_status":{"passed":true}}`), false)
	if strings.Join(d.Missing, "|") != "temperature|time powered on|wear" {
		t.Errorf("missing = %v", d.Missing)
	}
}

func TestDiskHealthReport(t *testing.T) {
	root := t.TempDir()
	mk := func(name, model, vendor, rot, size string) {
		b := filepath.Join(root, "sys", "block", name)
		os.MkdirAll(filepath.Join(b, "device"), 0o755)
		os.MkdirAll(filepath.Join(b, "queue"), 0o755)
		os.WriteFile(filepath.Join(b, "device", "model"), []byte(model), 0o644)
		os.WriteFile(filepath.Join(b, "device", "vendor"), []byte(vendor), 0o644)
		os.WriteFile(filepath.Join(b, "queue", "rotational"), []byte(rot), 0o644)
		os.WriteFile(filepath.Join(b, "size"), []byte(size), 0o644)
	}
	mk("nvme0n1", "Samsung SSD 980", "", "0", "1000215216")
	mk("sda", "WDC WD10", "ATA", "1", "1953525168")
	mk("vda", "", "", "1", "20971520")
	os.MkdirAll(filepath.Join(root, "sys", "block", "loop0"), 0o755)
	s := &Service{Root: root}

	oldLook, oldRun := lookPath, sysRun
	t.Cleanup(func() { lookPath, sysRun = oldLook, oldRun })
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	r := s.DiskHealth()
	if r.Tool || r.CanInstall || len(r.Drives) != 3 {
		t.Fatalf("no tool: %+v", r)
	}
	for _, d := range r.Drives {
		if d.Word != "Not reported" {
			t.Errorf("%s: %s", d.Name, d.Word)
		}
	}
	kinds := map[string]string{}
	for _, d := range r.Drives {
		kinds[d.Name] = d.Kind
	}
	if kinds["nvme0n1"] != "NVMe SSD" || kinds["sda"] != "Hard disk" || kinds["vda"] != "Virtual disk" {
		t.Fatalf("kinds = %v", kinds)
	}

	lookPath = func(n string) (string, error) { return "/usr/sbin/" + n, nil }
	sysRun = func(_ time.Duration, name string, args ...string) (string, error) {
		if strings.HasSuffix(args[len(args)-1], "nvme0n1") {
			return `{"model_name":"Samsung SSD 980 1TB","smart_status":{"passed":true},"temperature":{"current":38},"nvme_smart_health_information_log":{"percentage_used":2,"available_spare":100,"available_spare_threshold":10,"power_on_hours":4000}}`, nil
		}
		return `{"smartctl":{"messages":[{"string":"unsupported"}]}}`, nil // sda says nothing useful
	}
	r = s.DiskHealth()
	got := map[string]Drive{}
	for _, d := range r.Drives {
		got[d.Name] = d
	}
	if d := got["nvme0n1"]; d.Word != "Healthy" || d.Temp != 38 || d.Wear != 2 || d.Model != "Samsung SSD 980 1TB" || len(d.Lines) < 4 {
		t.Fatalf("nvme = %+v", d)
	}
	if d := got["sda"]; d.Word != "Not reported" || !strings.Contains(d.Reason, "does not report") {
		t.Fatalf("sda = %+v", d)
	}
	if d := got["vda"]; d.Word != "Not reported" || !strings.Contains(d.Reason, "virtual") {
		t.Fatalf("vda = %+v", d)
	}
}

package netd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Disk health: what each drive on the machine says about itself. NopeWarden reads it with smartctl
// when that is installed and says "Not reported" plainly when a drive (or a virtual disk) has nothing
// to say. Nothing here writes to a drive or runs a test on it.

type DiskLine struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Drive struct {
	Name    string     `json:"name"`
	Model   string     `json:"model"`
	Kind    string     `json:"kind"` // NVMe SSD, SATA SSD, Hard disk, Virtual disk, Disk
	Bytes   int64      `json:"bytes"`
	Word    string     `json:"word"`  // Healthy, Worn, Warning or Not reported
	Level   string     `json:"level"` // "", info, warn or bad
	Reason  string     `json:"reason"`
	Lines   []DiskLine `json:"lines"`
	Missing []string   `json:"missing"` // what this drive did not report
	Temp    int        `json:"temp"`    // degrees C, 0 when not reported
	Wear    int        `json:"wear"`    // percent used, -1 when not reported
	// Counters are the numbers worth watching over time: moved sectors ("realloc"), sectors waiting
	// ("pending"), sectors that could not be read ("uncorrectable"), cable errors ("crc"), media
	// errors ("media") and wear. Only the ones the drive reports are present.
	Counters  map[string]int64 `json:"counters,omitempty"`
	Test      *SelfTest        `json:"test,omitempty"`       // the drive's latest self-test
	Trend     []string         `json:"trend,omitempty"`      // plain lines about counters that rose, added by the app
	Gone      bool             `json:"gone,omitempty"`       // the drive was seen before and is no longer there
	LogErrors []string         `json:"log_errors,omitempty"` // disk errors in the system log, last 24 hours
}

// SelfTest is the result of the drive's most recent self-test.
type SelfTest struct {
	Kind    string `json:"kind"`   // Short or Extended
	Result  string `json:"result"` // Passed, Failed or Interrupted
	Hours   int    `json:"hours"`  // drive's power-on hours when it ran
	Running bool   `json:"running"`
}

type DiskReport struct {
	Tool       bool    `json:"tool"`        // smartctl is installed
	CanInstall bool    `json:"can_install"` // apt-get is there to install it
	Drives     []Drive `json:"drives"`
	At         int64   `json:"at"`
}

// smartJSON is the part of smartctl's JSON that is used.
type smartJSON struct {
	ModelName    string `json:"model_name"`
	UserCapacity struct {
		Bytes int64 `json:"bytes"`
	} `json:"user_capacity"`
	RotationRate int `json:"rotation_rate"`
	SmartStatus  *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature *struct {
		Current int `json:"current"`
	} `json:"temperature"`
	PowerOnTime *struct {
		Hours int `json:"hours"`
	} `json:"power_on_time"`
	PowerCycle *int `json:"power_cycle_count"`
	NVMe       *struct {
		CriticalWarning int `json:"critical_warning"`
		Temperature     int `json:"temperature"`
		AvailableSpare  int `json:"available_spare"`
		SpareThreshold  int `json:"available_spare_threshold"`
		PercentageUsed  int `json:"percentage_used"`
		MediaErrors     int `json:"media_errors"`
		UnsafeShutdowns int `json:"unsafe_shutdowns"`
		PowerOnHours    int `json:"power_on_hours"`
	} `json:"nvme_smart_health_information_log"`
	ATATests *struct {
		Standard struct {
			Table []struct {
				Type struct {
					String string `json:"string"`
				} `json:"type"`
				Status struct {
					String string `json:"string"`
					Passed bool   `json:"passed"`
				} `json:"status"`
				Hours int `json:"lifetime_hours"`
			} `json:"table"`
		} `json:"standard"`
	} `json:"ata_smart_self_test_log"`
	NVMeTests *struct {
		Table []struct {
			Code struct {
				String string `json:"string"`
			} `json:"self_test_code"`
			Result struct {
				String string `json:"string"`
			} `json:"self_test_result"`
			Hours int `json:"power_on_hours"`
		} `json:"table"`
	} `json:"nvme_self_test_log"`
	ATA *struct {
		Table []struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Value int    `json:"value"`
			Raw   struct {
				Value int64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
}

// judge turns what a drive reported into one plain word, a level and a sentence. It colours the drive
// amber or red only when the drive itself reports a problem.
func judge(d *Drive, j smartJSON, hdd bool) {
	d.Wear = -1
	d.Word, d.Level, d.Reason = "Healthy", "", "The drive reports no problems."
	warn := func(level, why string) {
		rank := map[string]int{"": 0, "info": 1, "warn": 2, "bad": 3}
		if rank[level] > rank[d.Level] || d.Word == "Healthy" {
			d.Word, d.Level, d.Reason = "Warning", level, why
		}
	}
	line := func(l, v string) { d.Lines = append(d.Lines, DiskLine{l, v}) }
	miss := func(s string) { d.Missing = append(d.Missing, s) }

	if j.SmartStatus != nil {
		if j.SmartStatus.Passed {
			line("Drive's own verdict", "Passed")
		} else {
			line("Drive's own verdict", "Failing")
			warn("bad", "The drive says it is failing. Back up now and plan to replace it.")
		}
	} else {
		miss("the drive's own verdict")
	}
	temp := 0
	if j.Temperature != nil {
		temp = j.Temperature.Current
	} else if j.NVMe != nil && j.NVMe.Temperature > 0 {
		temp = j.NVMe.Temperature
	}
	if temp > 0 {
		d.Temp = temp
		line("Temperature", fmt.Sprintf("%d °C", temp))
		limit := 70
		if hdd {
			limit = 60
		}
		if temp >= limit {
			warn("warn", fmt.Sprintf("The drive is running hot (%d °C). Check the airflow around it.", temp))
		}
	} else {
		miss("temperature")
	}
	hours := -1
	if j.PowerOnTime != nil {
		hours = j.PowerOnTime.Hours
	} else if j.NVMe != nil {
		hours = j.NVMe.PowerOnHours
	}
	if hours >= 0 {
		line("Powered on", fmt.Sprintf("%s (%d days)", fmtHours(hours), hours/24))
	} else {
		miss("time powered on")
	}
	if j.PowerCycle != nil {
		line("Power-ups", strconv.Itoa(*j.PowerCycle))
	}
	d.Counters = map[string]int64{}
	if n := j.NVMe; n != nil {
		d.Counters["media"] = int64(n.MediaErrors)
		d.Wear = n.PercentageUsed
		line("Wear", fmt.Sprintf("%d%% of its rated life used", n.PercentageUsed))
		line("Spare area left", fmt.Sprintf("%d%%", n.AvailableSpare))
		line("Media errors", strconv.Itoa(n.MediaErrors))
		line("Unsafe shutdowns", strconv.Itoa(n.UnsafeShutdowns))
		if n.CriticalWarning != 0 {
			warn("bad", "The drive has raised a critical warning. Back up now.")
		}
		if n.MediaErrors > 0 {
			warn("warn", fmt.Sprintf("The drive has logged %d media error%s.", n.MediaErrors, plural(n.MediaErrors)))
		}
		if n.AvailableSpare < n.SpareThreshold {
			warn("warn", "The drive is low on spare area. Plan to replace it.")
		}
	}
	if j.ATA != nil {
		var realloc, pending, uncorr, crc int64 = -1, -1, -1, -1
		for _, a := range j.ATA.Table {
			switch a.ID {
			case 5:
				realloc = a.Raw.Value
			case 197:
				pending = a.Raw.Value
			case 198:
				uncorr = a.Raw.Value
			case 199:
				crc = a.Raw.Value
			case 177, 202, 231, 233:
				if d.Wear < 0 && a.Value > 0 && a.Value <= 100 {
					d.Wear = 100 - a.Value // these report what is left
				}
			}
		}
		if d.Wear >= 0 {
			line("Wear", fmt.Sprintf("%d%% of its rated life used", d.Wear))
		} else if !hdd {
			miss("wear")
		}
		for k, v := range map[string]int64{"realloc": realloc, "pending": pending, "uncorrectable": uncorr, "crc": crc} {
			if v >= 0 {
				d.Counters[k] = v
			}
		}
		if realloc >= 0 {
			line("Sectors moved to spare", strconv.FormatInt(realloc, 10))
		}
		if pending >= 0 {
			line("Sectors waiting to be moved", strconv.FormatInt(pending, 10))
		}
		if uncorr >= 0 {
			line("Sectors that could not be read", strconv.FormatInt(uncorr, 10))
		}
		if pending > 0 || uncorr > 0 {
			warn("warn", "The drive has sectors it cannot read reliably. Back up and plan to replace it.")
		} else if realloc > 0 {
			warn("warn", fmt.Sprintf("The drive has moved %d sector%s to its spare area. Keep an eye on it.", realloc, plural(int(realloc))))
		}
	}
	if j.NVMe == nil && j.ATA == nil && !hdd {
		miss("wear")
	}
	if d.Wear >= 0 {
		d.Counters["wear"] = int64(d.Wear)
	}
	d.Test = lastSelfTest(j)
	if d.Test != nil {
		when := ""
		if d.Test.Running {
			line("Self-test", d.Test.Kind+" test running now")
		} else {
			if d.Test.Hours > 0 && hours >= d.Test.Hours {
				when = ", " + fmtHours(hours-d.Test.Hours) + " ago"
			}
			line("Last self-test", d.Test.Kind+": "+d.Test.Result+when)
		}
		if d.Test.Result == "Failed" {
			warn("bad", "The drive's own self-test failed. Back up now and plan to replace it.")
		}
	}
	if d.Word == "Healthy" && d.Wear >= 80 {
		d.Word, d.Level = "Worn", "info"
		d.Reason = fmt.Sprintf("%d%% of the drive's rated life is used. It still works; plan a replacement.", d.Wear)
		if d.Wear >= 90 {
			d.Level = "warn"
		}
	}
}

// lastSelfTest reads the newest entry of the drive's self-test log.
func lastSelfTest(j smartJSON) *SelfTest {
	if j.ATATests != nil && len(j.ATATests.Standard.Table) > 0 {
		e := j.ATATests.Standard.Table[0] // newest first
		t := &SelfTest{Hours: e.Hours}
		low := strings.ToLower(e.Type.String)
		t.Kind = "Short"
		if strings.Contains(low, "extended") || strings.Contains(low, "long") {
			t.Kind = "Extended"
		}
		st := strings.ToLower(e.Status.String)
		switch {
		case strings.Contains(st, "progress"):
			t.Running = true
		case e.Status.Passed || strings.Contains(st, "without error"):
			t.Result = "Passed"
		case strings.Contains(st, "abort") || strings.Contains(st, "interrupt") || strings.Contains(st, "host reset"):
			t.Result = "Interrupted"
		default:
			t.Result = "Failed"
		}
		return t
	}
	if j.NVMeTests != nil && len(j.NVMeTests.Table) > 0 {
		e := j.NVMeTests.Table[0]
		t := &SelfTest{Hours: e.Hours, Kind: "Short"}
		if strings.Contains(strings.ToLower(e.Code.String), "extended") {
			t.Kind = "Extended"
		}
		r := strings.ToLower(e.Result.String)
		switch {
		case strings.Contains(r, "completed without"):
			t.Result = "Passed"
		case strings.Contains(r, "abort"):
			t.Result = "Interrupted"
		default:
			t.Result = "Failed"
		}
		return t
	}
	return nil
}

// scanDiskLog picks out the lines of the kernel log that mean a drive had trouble: failed reads and
// writes, and the link to a SATA drive being reset. Lines naming a drive go to that drive; link
// resets name only a port, so they go to the machine's one SATA drive when there is exactly one.
func scanDiskLog(text string, drives map[string]string) map[string][]string {
	out := map[string][]string{}
	sata := ""
	n := 0
	for name, kind := range drives {
		if kind == "SATA SSD" || kind == "Hard disk" {
			sata = name
			n++
		}
	}
	for _, ln := range strings.Split(text, "\n") {
		low := strings.ToLower(ln)
		hit := ""
		switch {
		case strings.Contains(low, "i/o error, dev "), strings.Contains(low, "blk_update_request"), strings.Contains(low, "buffer i/o error on dev"):
			for name := range drives {
				if strings.Contains(low, "dev "+strings.ToLower(name)) {
					hit = name
				}
			}
		case strings.Contains(low, "nvme") && (strings.Contains(low, "timeout") || strings.Contains(low, "controller is down") || strings.Contains(low, "resetting controller")):
			for name := range drives {
				if strings.HasPrefix(name, "nvme") && strings.Contains(low, name[:strings.Index(name+"n", "n")]+":") {
					hit = name
				}
			}
		case strings.Contains(low, "hard resetting link"), strings.Contains(low, "exception emask"), strings.Contains(low, "failed command: "), strings.Contains(low, "sata link down"):
			if n == 1 {
				hit = sata
			}
		}
		if hit != "" {
			out[hit] = append(out[hit], strings.TrimSpace(ln))
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func fmtHours(h int) string {
	if h >= 24*365 {
		return fmt.Sprintf("%.1f years", float64(h)/(24*365))
	}
	if h >= 24 {
		return fmt.Sprintf("%d days", h/24)
	}
	return fmt.Sprintf("%d hours", h)
}

var lookPath = exec.LookPath

func readSys(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// diskKind works out what a block device is from what sysfs says about it.
func diskKind(name, model, vendor string, rotational bool) (kind string, virtual bool) {
	m := strings.ToLower(model + " " + vendor)
	switch {
	case strings.HasPrefix(name, "vd"), strings.HasPrefix(name, "xvd"),
		strings.Contains(m, "qemu"), strings.Contains(m, "virtual"), strings.Contains(m, "vbox"), strings.Contains(m, "vmware"), strings.Contains(m, "msft"):
		return "Virtual disk", true
	case strings.HasPrefix(name, "nvme"):
		return "NVMe SSD", false
	case rotational:
		return "Hard disk", false
	case strings.HasPrefix(name, "mmcblk"):
		return "Memory card", false
	}
	return "SATA SSD", false
}

// DiskHealth reads every drive's health. It is quick when smartctl is missing.
func (s *Service) DiskHealth() DiskReport {
	rep := DiskReport{At: time.Now().Unix()}
	_, err := lookPath("smartctl")
	rep.Tool = err == nil
	_, aerr := lookPath("apt-get")
	rep.CanInstall = aerr == nil
	ents, _ := os.ReadDir(filepath.Join(s.sys(), "block"))
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") || strings.HasPrefix(n, "dm-") || strings.HasPrefix(n, "sr") ||
			strings.HasPrefix(n, "zram") || strings.HasPrefix(n, "nbd") || strings.HasPrefix(n, "md") || strings.HasPrefix(n, "fd") {
			continue
		}
		base := filepath.Join(s.sys(), "block", n)
		model := readSys(filepath.Join(base, "device", "model"))
		if model == "" {
			model = readSys(filepath.Join(base, "device", "name"))
		}
		vendor := readSys(filepath.Join(base, "device", "vendor"))
		rot := readSys(filepath.Join(base, "queue", "rotational")) == "1"
		sectors, _ := strconv.ParseInt(readSys(filepath.Join(base, "size")), 10, 64)
		d := Drive{Name: n, Model: strings.TrimSpace(vendor + " " + model), Bytes: sectors * 512, Wear: -1}
		var virtual bool
		d.Kind, virtual = diskKind(n, model, vendor, rot)
		if d.Model == "" {
			d.Model = d.Kind
		}
		switch {
		case virtual:
			d.Word, d.Reason = "Not reported", "This is a virtual disk. Its health belongs to the machine that provides it."
		case !rep.Tool:
			d.Word, d.Reason = "Not reported", "NopeWarden needs the SMART tool to read this drive."
		default:
			out, _ := sysRun(20*time.Second, "smartctl", "-j", "-a", "/dev/"+n)
			var j smartJSON
			if json.Unmarshal([]byte(out), &j) != nil || (j.SmartStatus == nil && j.NVMe == nil && j.ATA == nil && j.Temperature == nil) {
				d.Word, d.Reason = "Not reported", "This drive does not report its health to the machine."
			} else {
				if j.ModelName != "" {
					d.Model = j.ModelName
				}
				if j.UserCapacity.Bytes > 0 {
					d.Bytes = j.UserCapacity.Bytes
				}
				judge(&d, j, d.Kind == "Hard disk")
			}
		}
		rep.Drives = append(rep.Drives, d)
	}
	sort.Slice(rep.Drives, func(i, k int) bool { return rep.Drives[i].Name < rep.Drives[k].Name })
	kinds := map[string]string{}
	for _, d := range rep.Drives {
		kinds[d.Name] = d.Kind
	}
	if len(kinds) > 0 {
		if txt, err := sysRun(15*time.Second, "journalctl", "-k", "--since", "-24h", "--no-pager", "-o", "cat"); err == nil {
			found := scanDiskLog(txt, kinds)
			for i := range rep.Drives {
				if ls := found[rep.Drives[i].Name]; len(ls) > 0 {
					if len(ls) > 5 {
						ls = append(ls[:2:2], ls[len(ls)-3:]...)
					}
					rep.Drives[i].LogErrors = ls
				}
			}
		}
	}
	return rep
}

// DiskSelfTest asks a drive to run its own short or extended self-test. The drive does the work
// itself, in the background, and keeps working normally meanwhile.
func (s *Service) DiskSelfTest(name, kind string) error {
	if kind != "short" && kind != "long" {
		return fmt.Errorf("the test is short or long")
	}
	if name == "" || strings.ContainsAny(name, "/.") {
		return fmt.Errorf("that is not a drive")
	}
	base := filepath.Join(s.sys(), "block", name)
	if _, err := os.Stat(base); err != nil {
		return fmt.Errorf("that drive is not here")
	}
	model := readSys(filepath.Join(base, "device", "model"))
	vendor := readSys(filepath.Join(base, "device", "vendor"))
	if _, virtual := diskKind(name, model, vendor, false); virtual {
		return fmt.Errorf("a virtual disk has no self-test")
	}
	if _, err := lookPath("smartctl"); err != nil {
		return fmt.Errorf("NopeWarden needs the SMART tool to test a drive")
	}
	out, err := sysRun(30*time.Second, "smartctl", "-t", kind, "/dev/"+name)
	if err != nil && !strings.Contains(out, "Testing has begun") && !strings.Contains(out, "Self-test has begun") {
		return fmt.Errorf("the drive did not start the test: %s", smartLastLine(out))
	}
	return nil
}

// DiskToolInstall installs smartmontools, the small program that reads drive health.
func (s *Service) DiskToolInstall() error {
	if _, err := lookPath("apt-get"); err != nil {
		return fmt.Errorf("this machine cannot install programs by itself")
	}
	if b, err := aptRun(10*time.Minute, "apt-get", aptArgs("install", "--no-install-recommends", "smartmontools")...); err != nil {
		return fmt.Errorf("the SMART tool did not install: %s", smartLastLine(string(b)))
	}
	return nil
}

func smartLastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

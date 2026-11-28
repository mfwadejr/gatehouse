package app

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nopewarden/internal/api"
)

// Temperatures and fans, read from the kernel's hardware monitor. Only what can be trusted is shown:
// the processor's temperature, the board's temperatures, and whether each fan is turning. The chips'
// own alarm limits and voltage readings are left out: they are often unset defaults or need a
// board-specific scale. A box with no fan chip says nothing about fans.

var hwmonRoot = "/sys/class/hwmon"

type hwTemp struct {
	Name  string  `json:"name"`
	Kind  string  `json:"kind"` // cpu, board
	C     float64 `json:"c"`
	High  float64 `json:"high"`
	Crit  float64 `json:"crit,omitempty"`
	State string  `json:"state"` // ok, warm, hot
}

type hwFan struct {
	Name  string `json:"name"`
	RPM   int    `json:"rpm"`
	State string `json:"state"` // turning, stopped
}

type hwReading struct {
	Temps   []hwTemp `json:"temps"`
	Fans    []hwFan  `json:"fans"`
	HasFans bool     `json:"has_fans"` // a fan chip was found
}

type hwState struct {
	mu      sync.Mutex
	seen    map[string]bool // fans that have turned since start
	hot     bool
	stopped map[string]bool
}

func hwNum(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	return v, err == nil
}

func hwText(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

// readHardware reads every sensor chip once. Fans that have never turned are left out (an empty
// header), and a fan that was turning and now reads zero is reported as stopped.
func (a *App) readHardware() hwReading {
	var r hwReading
	dirs, _ := filepath.Glob(filepath.Join(hwmonRoot, "hwmon*"))
	sort.Strings(dirs)
	var board []hwTemp
	cpu := -1.0
	var cpuT []hwTemp
	a.hw.mu.Lock()
	defer a.hw.mu.Unlock()
	if a.hw.seen == nil {
		a.hw.seen = map[string]bool{}
	}
	for _, d := range dirs {
		chip := hwText(filepath.Join(d, "name"))
		isCPU := chip == "coretemp" || chip == "k10temp"
		ins, _ := filepath.Glob(filepath.Join(d, "temp*_input"))
		sort.Strings(ins)
		for i, f := range ins {
			v, ok := hwNum(f)
			if !ok || v <= 0 {
				continue
			}
			c := v / 1000
			base := strings.TrimSuffix(f, "_input")
			label := hwText(base + "_label")
			high, _ := hwNum(base + "_max")
			crit, _ := hwNum(base + "_crit")
			high, crit = high/1000, crit/1000
			if high < 40 || high > 130 {
				high = 80 // an unset or silly limit on the chip: use the usual one
			}
			if crit < high || crit > 140 {
				crit = 0
			}
			t := hwTemp{C: math.Round(c*10) / 10, High: high, Crit: crit, State: "ok"}
			if c >= high {
				t.State = "hot"
			} else if c >= high-10 {
				t.State = "warm"
			}
			if isCPU {
				t.Kind, t.Name = "cpu", "Processor"
				if label != "" && !strings.HasPrefix(label, "Package") {
					t.Name = "Processor " + strings.ToLower(label)
				}
				if strings.HasPrefix(label, "Package") || (label == "" && cpu < 0) {
					cpuT = append(cpuT, t)
				}
				if c > cpu {
					cpu = c
				}
				if label == "" || strings.HasPrefix(label, "Package") {
					continue
				}
				continue
			}
			t.Kind = "board"
			t.Name = fmt.Sprintf("Board %d", len(board)+1)
			if chip == "acpitz" {
				t.Name = fmt.Sprintf("Board zone %d", i+1)
			}
			board = append(board, t)
		}
		fans, _ := filepath.Glob(filepath.Join(d, "fan*_input"))
		sort.Strings(fans)
		if len(fans) > 0 && !isCPU && chip != "acpitz" {
			r.HasFans = true
		}
		for _, f := range fans {
			v, ok := hwNum(f)
			if !ok {
				continue
			}
			key := chip + ":" + filepath.Base(f)
			n := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "fan"), "_input")
			if v > 0 {
				a.hw.seen[key] = true
				r.Fans = append(r.Fans, hwFan{Name: "Fan " + n, RPM: int(v), State: "turning"})
			} else if a.hw.seen[key] {
				r.Fans = append(r.Fans, hwFan{Name: "Fan " + n, State: "stopped"})
			}
		}
	}
	if len(cpuT) > 0 {
		r.Temps = append(r.Temps, cpuT[0])
	} else if cpu > 0 {
		r.Temps = append(r.Temps, hwTemp{Name: "Processor", Kind: "cpu", C: math.Round(cpu*10) / 10, High: 80, State: "ok"})
	}
	for _, t := range board {
		if cpu > 0 && t.C < cpu-10 { // a sensor far below the rest is not on anything warm
			continue
		}
		r.Temps = append(r.Temps, t)
	}
	if r.Temps == nil {
		r.Temps = []hwTemp{}
	}
	if r.Fans == nil {
		r.Fans = []hwFan{}
	}
	return r
}

// hwLoop loads the sensor drivers once, then watches: a hot processor or a stopped fan is said once
// in the events and again only after it has recovered.
func (a *App) hwLoop(ctx context.Context) {
	if a.Net != nil {
		a.Net.HwLoad()
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		r := a.readHardware()
		a.hwWatch(r)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *App) hwWatch(r hwReading) {
	a.hw.mu.Lock()
	if a.hw.stopped == nil {
		a.hw.stopped = map[string]bool{}
	}
	var say [][2]string
	hot, cool := false, true
	for _, t := range r.Temps {
		if t.Kind == "cpu" && t.State == "hot" {
			hot = true
		}
		if t.Kind == "cpu" && t.C > t.High-5 {
			cool = false
		}
	}
	if hot && !a.hw.hot {
		a.hw.hot = true
		say = append(say, [2]string{"The processor is running hot", fmt.Sprintf("%.0f°C. It slows itself down near its limit; check the fans and the room.", r.Temps[0].C)})
	} else if a.hw.hot && cool {
		a.hw.hot = false
	}
	for _, f := range r.Fans {
		if f.State == "stopped" && !a.hw.stopped[f.Name] {
			a.hw.stopped[f.Name] = true
			say = append(say, [2]string{f.Name + " has stopped", "It was turning and now reads zero. Check it is plugged in and not blocked."})
		} else if f.State == "turning" {
			delete(a.hw.stopped, f.Name)
		}
	}
	a.hw.mu.Unlock()
	for _, s := range say {
		a.event("system", "warning", s[0], s[1])
	}
}

// cpuTemp is the processor temperature in whole degrees for the front panel, or 0 when unknown.
func (a *App) cpuTemp() int {
	for _, t := range a.readHardware().Temps {
		if t.Kind == "cpu" {
			return int(math.Round(t.C))
		}
	}
	return 0
}

func (a *App) registerHardwareRoutes() {
	a.API.Handle("GET /api/hardware", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.readHardware())
	})
}

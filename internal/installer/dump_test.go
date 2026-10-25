package installer

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gatehouse/internal/gfx"
)

// TestDumpScreens draws every machine screen as it appears on the graphical display and writes PNGs, for
// design review. It only runs when GATEHOUSE_DUMP names a directory.
func TestDumpScreens(t *testing.T) {
	dir := os.Getenv("GATEHOUSE_DUMP")
	if dir == "" {
		t.Skip("set GATEHOUSE_DUMP to write screens")
	}
	os.MkdirAll(dir, 0o755)
	const PW, PH = 1366, 768
	cw, ch := gfx.Metrics(1)
	W, H := PW/cw, PH/ch
	w := func(name, s string) {
		e := gfx.NewEmu(W, H)
		e.Write([]byte(s))
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		defer f.Close()
		png.Encode(f, e.Render(PW, PH, 1))
	}
	ports := []Port{{Name: "enp1s0", Link: true, Speed: "1000"}, {Name: "enp2s0"}, {Name: "enx00e04c680a1b", USB: true}}
	one := []Port{{Name: "enp1s0", Link: true, Speed: "1000"}}
	w("10-first-boot-no-network", RenderStatus("gatehouse", []Port{{Name: "enp1s0"}, {Name: "enp2s0"}}, nil, "account", W, H))
	w("11-first-boot-setup", RenderStatus("gatehouse", ports, []string{"https://192.168.1.20"}, "account", W, H))
	w("12-first-boot-wizard", RenderStatus("gatehouse", one, []string{"https://192.168.1.1"}, "wizard", W, H))
	w("13-ready", RenderStatus("gatehouse", one, []string{"https://192.168.1.1"}, "ready", W, H))
	w("14-ready-two-networks", RenderStatus("gatehouse", ports, []string{"https://192.168.1.1     on port enp1s0", "https://10.20.0.1        on port enp2s0"}, "ready", W, H))
	w("20-installer-web-waiting", RenderScreen([]Port{{Name: "enp1s0"}, {Name: "enp2s0"}}, nil, "1234-5678", W, H))
	w("21-installer-web-ready", RenderScreen(ports, []string{"https://192.168.1.20"}, "1234-5678", W, H))

	in, _ := newTest(t)
	script := strings.Join([]string{"", "1", "", "/dev/nvme0n1", ""}, "\r") + "\r"
	var out strings.Builder
	tm := NewTerm(strings.NewReader(script), &out)
	tm.W, tm.H = W, H
	env := testEnv(in)
	env.Poll = 30e6
	RunText(tm, env)
	frames := strings.Split(out.String(), "\x1b[2J")
	for i, f := range frames[1:] {
		w("t"+string(rune('a'+i/26))+string(rune('a'+i%26)), "\x1b[2J"+f)
	}
}

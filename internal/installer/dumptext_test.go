package installer

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestDumpText writes the text installer's screens as plain text, for the website pictures.
func TestDumpText(t *testing.T) {
	if os.Getenv("NW_TEXT") == "" {
		t.Skip("picture-taking tool; set NW_TEXT=1")
	}
	inputs := []string{"", "1", "", "", "1", "/dev/nvme0n1", ""}
	for k := 0; k <= len(inputs); k++ {
		in, _ := newTest(t)
		script := strings.Join(inputs[:k], "\r")
		if k > 0 {
			script += "\r"
		}
		var out strings.Builder
		tm := NewTerm(strings.NewReader(script), &out)
		tm.W, tm.H = 80, 24
		env := testEnv(in)
		env.Serial = ""
		env.Poll = 30e6
		RunText(tm, env)
		os.WriteFile(fmt.Sprintf("/tmp/text_out_%d.txt", k), []byte(out.String()), 0o644)
	}
	ports := []Port{{Name: "enp1s0", Link: true, Speed: "1000"}, {Name: "enp2s0"}, {Name: "enx00e04c680a1b", USB: true}}
	os.WriteFile("/tmp/text_out_status.txt", []byte(RenderStatus("nopewarden", ports, []string{"https://192.168.1.20"}, "account", 80, 24)), 0o644)
}

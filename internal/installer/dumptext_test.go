package installer

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDumpText writes the text installer's screens as plain text, for the website pictures.
func TestDumpText(t *testing.T) {
	if os.Getenv("NW_TEXT") == "" {
		t.Skip("picture-taking tool; set NW_TEXT=1")
	}
	inputs := []string{"", "1", "", "", "", "1", "/dev/nvme0n1", ""}
	for k := 0; k <= len(inputs); k++ {
		in, _ := newTest(t)
		script := strings.Join(inputs[:k], "\r")
		if k > 0 {
			script += "\r"
		}
		var out strings.Builder
		tm := NewTerm(slowReader{strings.NewReader(script)}, &out)
		tm.W, tm.H = 80, 24
		env := testEnv(in)
		env.Serial = ""
		env.Poll = 30e6
		// Two cables in: the internet side hears its provider's DHCP server, the LAN side hears nothing.
		env.Ports = func() []Port {
			return []Port{{Name: "enp1s0", Link: true, Speed: "1000", MAC: "52:54:00:00:00:01"}, {Name: "enp2s0", Link: true, Speed: "1000", MAC: "52:54:00:00:00:02"}, {Name: "enx00e04c680a1b", USB: true, MAC: "00:e0:4c:68:0a:1b"}}
		}
		env.Probe = func(p Port) ProbeResult {
			if p.Name == "enp1s0" {
				return ProbeResult{Port: p.Name, Offers: []Offer{{Server: netip.MustParseAddr("203.0.113.1"), Given: netip.MustParsePrefix("203.0.113.9/24")}}}
			}
			return ProbeResult{Port: p.Name}
		}
		RunText(tm, env)
		os.WriteFile(fmt.Sprintf("/tmp/text_out_%d.txt", k), []byte(out.String()), 0o644)
	}
	ports := []Port{{Name: "enp1s0", Link: true, Speed: "1000"}, {Name: "enp2s0"}, {Name: "enx00e04c680a1b", USB: true}}
	os.WriteFile("/tmp/text_out_status.txt", []byte(RenderStatus("nopewarden", ports, []string{"https://192.168.1.20"}, "account", 80, 24)), 0o644)
}

// slowReader hands over input a byte at a time with a short wait, so screens that fill in by themselves
// (the DHCP check) have drawn their final state before the next key arrives.
type slowReader struct{ r *strings.Reader }

func (s slowReader) Read(p []byte) (int, error) {
	time.Sleep(100 * time.Millisecond)
	if len(p) > 1 {
		p = p[:1]
	}
	return s.r.Read(p)
}

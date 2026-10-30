package netd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNDPIApply(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root}
	var calls []string
	old := wifiRun
	wifiRun = func(_ time.Duration, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	t.Cleanup(func() { wifiRun = old })

	// Not installed: refuse in plain words, start nothing.
	if err := s.NDPIApply(NDPIPlan{Enabled: true, Ifaces: []string{"lan0"}}); err == nil || !strings.Contains(err.Error(), "fresh install image") {
		t.Fatalf("want a plain refusal, got %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("nothing should run: %v", calls)
	}
	bin := filepath.Join(root, ndpiBinary)
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("x"), 0o755)
	if !s.NDPIInstalled() {
		t.Fatal("should see the program")
	}
	if err := s.NDPIApply(NDPIPlan{Enabled: true, Ifaces: []string{"lan0; rm -rf /"}}); err == nil {
		t.Fatal("a bad port name must be refused")
	}
	if err := s.NDPIApply(NDPIPlan{Enabled: true, Ifaces: []string{"lan0", "lan1.20"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, ndpiConf))
	if string(b) != "# Written by Gatehouse\nIFACES=lan0 lan1.20\n" {
		t.Fatalf("conf: %q", b)
	}
	if !strings.Contains(strings.Join(calls, "|"), "restart "+ndpiUnit) {
		t.Fatalf("should restart: %v", calls)
	}
	if err := s.NDPIApply(NDPIPlan{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ndpiConf)); err == nil {
		t.Fatal("conf should be gone")
	}
	if !strings.Contains(strings.Join(calls, "|"), "disable --now "+ndpiUnit) {
		t.Fatalf("should stop: %v", calls)
	}
}

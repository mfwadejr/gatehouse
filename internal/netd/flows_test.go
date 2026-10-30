package netd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowAcctAndDump(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root}
	os.MkdirAll(filepath.Join(root, "proc/sys/net/netfilter"), 0o755)
	os.MkdirAll(filepath.Join(root, "proc/net"), 0o755)
	if err := s.FlowAcct(true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, flowAcctProc)); string(b) != "1" {
		t.Fatalf("acct = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, flowAcctSysctl)); !strings.Contains(string(b), "nf_conntrack_acct = 1") {
		t.Fatalf("sysctl file = %q", b)
	}
	if err := s.FlowAcct(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, flowAcctSysctl)); err == nil {
		t.Fatal("the setting file should be gone")
	}
	if _, err := s.ConntrackDump(); err == nil {
		t.Fatal("expected an error with no list")
	}
	os.WriteFile(filepath.Join(root, conntrackProc), []byte("ipv4 2 udp 17 29 src=1.1.1.1\n"), 0o644)
	out, err := s.ConntrackDump()
	if err != nil || !strings.Contains(out, "udp") {
		t.Fatalf("%q %v", out, err)
	}
}

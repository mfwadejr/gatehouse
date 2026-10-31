package netd

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLookupThroughOneConnection(t *testing.T) {
	old := lookupVia
	defer func() { lookupVia = old }()
	var gotDev string
	lookupVia = func(dev, name string) (time.Duration, string, error) {
		gotDev = dev
		if dev == "eth1" {
			return 0, "", errors.New("i/o timeout")
		}
		return 23 * time.Millisecond, "1.1.1.1", nil
	}
	s := &Service{}
	out, err := s.Diag("lookup:eth0.20", "example.com")
	if err != nil || out != "ok|23|1.1.1.1" || gotDev != "eth0.20" {
		t.Fatalf("%q %v dev=%q", out, err, gotDev)
	}
	if out, _ := s.Diag("lookup:eth1", "example.com"); out != "fail|i/o timeout" {
		t.Fatalf("%q", out)
	}
	// A device name that could be an option or a path is refused before anything is asked.
	for _, bad := range []string{"-I", "../x", "eth0 -x", "a/b", ""} {
		if out, _ := s.Diag("lookup:"+bad, "example.com"); !strings.HasPrefix(out, "fail|") {
			t.Errorf("device %q was accepted: %q", bad, out)
		}
	}
}

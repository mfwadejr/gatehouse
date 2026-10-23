package privsep

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gatehouse/internal/fw"
)

type fake struct {
	applied string
	window  time.Duration
	pending bool
}

func (f *fake) Check(s string) error {
	if s == "bad" {
		return errors.New("nft: syntax")
	}
	return nil
}
func (f *fake) Apply(s string, w time.Duration, a string) (string, time.Time, error) {
	if f.pending {
		return "", time.Time{}, fw.ErrPending
	}
	f.applied, f.window, f.pending = s, w, true
	return "tok", time.Now().Add(w), nil
}
func (f *fake) Confirm(t string) error {
	if t != "tok" {
		return errors.New("mismatch")
	}
	f.pending = false
	return nil
}
func (f *fake) Rollback() error { f.pending = false; return nil }
func (f *fake) Pending() (string, time.Time, bool) {
	return "tok", time.Now().Add(time.Minute), f.pending
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netd.sock")
	l, err := Listen(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	f := &fake{}
	go (&Server{FW: f}).Serve(l)
	c := &Client{Path: path}

	if err := c.Check("bad"); err == nil || err.Error() != "nft: syntax" {
		t.Fatalf("check: %v", err)
	}
	if _, _, err := c.Apply("x", time.Second, "a"); err == nil {
		t.Fatal("a one second window must be refused")
	}
	tok, _, err := c.Apply("x", 30*time.Second, "alice")
	if err != nil || tok != "tok" || f.applied != "x" {
		t.Fatalf("apply: %q %v", tok, err)
	}
	if _, _, err := c.Apply("y", 30*time.Second, "alice"); err != fw.ErrPending {
		t.Fatalf("second apply: %v", err)
	}
	if _, _, ok := c.Pending(); !ok {
		t.Fatal("pending not reported")
	}
	if err := c.Confirm("nope"); err == nil {
		t.Fatal("bad token accepted")
	}
	if err := c.Confirm("tok"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Pending(); ok {
		t.Fatal("still pending after confirm")
	}
}

func TestMissingHelper(t *testing.T) {
	c := &Client{Path: filepath.Join(t.TempDir(), "none.sock")}
	if err := c.Check("x"); err == nil {
		t.Fatal("expected an error")
	}
}

package netd

import (
	"os"
	"strings"
	"testing"
	"time"
)

func readUntil(t *testing.T, s *Service, id, want string) string {
	t.Helper()
	var all []byte
	next := 0
	for i := 0; i < 60; i++ {
		d, n, closed, err := s.TermRead(id, next, 500*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		all, next = append(all, d...), n
		if strings.Contains(string(all), want) {
			return string(all)
		}
		if closed {
			break
		}
	}
	t.Fatalf("never saw %q in %q", want, all)
	return ""
}

func termBox(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Fatal("this machine has no /dev/ptmx")
	}
	old := termShell
	termShell = []string{"/bin/sh"}
	t.Cleanup(func() { termShell = old })
}

func TestTerminalRunsACommandAndCloses(t *testing.T) {
	termBox(t)
	s := &Service{}
	id, err := s.TermOpen(30, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TermWrite(id, []byte("echo gate$((20+22))house\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, s, id, "gate42house")
	if err := s.TermResize(id, 40, 120); err != nil {
		t.Fatal(err)
	}
	sum, err := s.TermClose(id)
	if err != nil || sum.BytesIn == 0 || sum.BytesOut == 0 {
		t.Fatalf("%+v %v", sum, err)
	}
	if err := s.TermWrite(id, []byte("x")); err == nil {
		t.Fatal("wrote to a closed terminal")
	}
}

func TestTerminalLimitsAndIdleEnd(t *testing.T) {
	termBox(t)
	oldIdle, oldMax := termIdle, termMax
	termIdle, termMax = 1500*time.Millisecond, 1
	t.Cleanup(func() { termIdle, termMax = oldIdle, oldMax })
	s := &Service{}
	id, err := s.TermOpen(24, 80)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TermOpen(24, 80); err == nil {
		t.Fatal("opened more terminals than allowed")
	}
	// nobody types: the session ends by itself
	next := 0
	for i := 0; i < 50; i++ {
		_, n, closed, _ := s.TermRead(id, next, 200*time.Millisecond)
		next = n
		if closed {
			s.TermClose(id)
			return
		}
	}
	t.Fatal("an idle terminal was not ended")
}

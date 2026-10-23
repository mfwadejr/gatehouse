package crypt

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secret.key")
	b, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	s := b.Seal("hunter2")
	if s == "hunter2" {
		t.Fatal("not encrypted")
	}
	got, err := b.Open(s)
	if err != nil || got != "hunter2" {
		t.Fatalf("got %q err %v", got, err)
	}
	b2, _ := LoadOrCreate(p) // same key reloaded
	if got, err := b2.Open(s); err != nil || got != "hunter2" {
		t.Fatalf("reload failed: %q %v", got, err)
	}
	if _, err := b.Open(s[:len(s)-3] + "AAA"); err == nil {
		t.Fatal("tampering not detected")
	}
}

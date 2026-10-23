package tlsutil

import (
	"crypto/tls"
	"testing"
)

func TestEnsureCreatesAndReuses(t *testing.T) {
	d := t.TempDir()
	c, k, err := Ensure(d, "box")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.LoadX509KeyPair(c, k); err != nil {
		t.Fatal(err)
	}
	before, _ := tls.LoadX509KeyPair(c, k)
	c2, k2, _ := Ensure(d, "box")
	after, _ := tls.LoadX509KeyPair(c2, k2)
	if string(before.Certificate[0]) != string(after.Certificate[0]) {
		t.Fatal("certificate regenerated although still valid")
	}
}

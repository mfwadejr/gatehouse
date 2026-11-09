package installer

import "testing"

func TestCPRFilterPicksOutTheReport(t *testing.T) {
	var f cprFilter
	out, r, c := f.Feed([]byte("a\x1b[48;132Rb"))
	if string(out) != "ab" || r != 48 || c != 132 {
		t.Fatalf("got %q %d %d", out, r, c)
	}
}

func TestCPRFilterAcrossReads(t *testing.T) {
	var f cprFilter
	var all []byte
	var rows, cols int
	for _, piece := range []string{"x\x1b", "[2", "4;8", "0", "Ry"} {
		o, r, c := f.Feed([]byte(piece))
		all = append(all, o...)
		if r != 0 {
			rows, cols = r, c
		}
	}
	if string(all) != "xy" || rows != 24 || cols != 80 {
		t.Fatalf("got %q %d %d", all, rows, cols)
	}
}

func TestCPRFilterLeavesKeysAlone(t *testing.T) {
	var f cprFilter
	for _, k := range []string{"\x1b[A", "\x1b[B", "\x1bOA", "\x1b", "hello\r"} {
		var g cprFilter
		out, r, _ := g.Feed([]byte(k))
		// A lone ESC may be held back in case a report follows; everything else passes straight through.
		if r != 0 || (k != "\x1b" && string(out) != k) {
			t.Fatalf("%q: got %q rows %d", k, out, r)
		}
	}
	_ = f
}

func TestSane(t *testing.T) {
	if sane(1, 1) || sane(24, 20) || !sane(24, 80) || !sane(50, 200) {
		t.Fatal("sane() is wrong")
	}
}

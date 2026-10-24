package app

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIDRSplit(t *testing.T) {
	// 768 addresses starting on a 256 boundary: a /23 would not be aligned, so /24 + /23.
	got := strings.Join(cidrsOf(netip.MustParseAddr("5.8.1.0"), 768), " ")
	if got != "5.8.1.0/24 5.8.2.0/23" {
		t.Fatal(got)
	}
	if got := strings.Join(cidrsOf(netip.MustParseAddr("10.0.0.0"), 65536), " "); got != "10.0.0.0/16" {
		t.Fatal(got)
	}
	if got := strings.Join(cidrsOf(netip.MustParseAddr("0.0.0.0"), 1<<32), " "); got != "0.0.0.0/0" {
		t.Fatal(got)
	}
}

func TestDelegationParsing(t *testing.T) {
	src := `2|ripencc|20261023|1000|19830705|20261023|+0100
ripencc|*|ipv4|*|2|summary
# comment
ripencc|RU|ipv4|5.8.0.0|65536|20120101|allocated
ripencc|RU|ipv4|9.9.9.0|256|20120101|reserved
ripencc|DE|ipv6|2a00:1000::|32|20120101|assigned
ripencc|XX|ipv4|garbage|1|20120101|allocated
`
	var got []string
	if err := parseDelegations(strings.NewReader(src), func(cc string, fam int, p string) { got = append(got, cc+" "+istr(fam)+" "+p) }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "RU 4 5.8.0.0/16|DE 6 2a00:1000::/32" {
		t.Fatalf("%q", got)
	}
}

func TestGeoPrefixLookup(t *testing.T) {
	a := &App{Cfg: Config{DataDir: t.TempDir()}}
	os.MkdirAll(filepath.Join(a.Cfg.DataDir, "geo"), 0o755)
	os.WriteFile(a.geoFile(), []byte("RU 4 5.8.0.0/16\nRU 6 2a00::/32\nDE 4 1.2.3.0/24\n"), 0o644)
	got := a.geoPrefixes(map[string]bool{"ru_4": true, "de_4": true})
	if len(got["ru_4"]) != 1 || got["de_4"][0] != "1.2.3.0/24" || len(got["ru_6"]) != 0 {
		t.Fatalf("%v", got)
	}
}

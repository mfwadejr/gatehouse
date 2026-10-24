package appid

import "testing"

func TestLookup(t *testing.T) {
	for name, want := range map[string]string{
		"r4---sn-abc.googlevideo.com": "YouTube", "www.NETFLIX.com.": "Netflix", "ipv4-c001.nflxvideo.net": "Netflix",
		"api.spotify.com": "Spotify", "example.org": "",
	} {
		a, ok := Lookup(name)
		if a.Name != want || ok != (want != "") {
			t.Errorf("%s: %+v %v, want %q", name, a, ok, want)
		}
	}
	if len(Domains("youtube")) == 0 || len(Names()) < 50 {
		t.Fatal("catalogue lists look wrong")
	}
	// a suffix must not match by accident: notnetflix.com is not Netflix
	if _, ok := Lookup("notnetflix.com"); ok {
		t.Fatal("matched inside a longer name")
	}
}

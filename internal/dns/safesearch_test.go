package dnssrv

import "testing"

func TestSafeSearchTarget(t *testing.T) {
	for in, want := range map[string]string{
		"www.google.com.":      "forcesafesearch.google.com.",
		"google.co.uk.":        "forcesafesearch.google.com.",
		"www.google.de.":       "forcesafesearch.google.com.",
		"WWW.BING.COM.":        "strict.bing.com.",
		"duckduckgo.com.":      "safe.duckduckgo.com.",
		"www.youtube.com.":     "restrictmoderate.youtube.com.",
		"mail.google.com.":     "",
		"example.com.":         "",
		"www.google.com.evil.": "",
		"notgoogle.com.":       "",
	} {
		if got := SafeSearchTarget(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

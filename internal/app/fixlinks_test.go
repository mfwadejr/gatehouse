package app

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every "Fix" button in the Security check and the connection tester opens a page and tab of the
// console. This keeps those targets real, so a button never lands on the wrong page.
func TestFixButtonsOpenRealPagesAndTabs(t *testing.T) {
	tabs := map[string][]string{
		"firewall": {"networks", "rules", "groups", "ports", "countries", "discovery", "protection", "history"},
		"settings": {"general", "account", "alerts", "backup", "advanced"},
		"profiles": nil, "networks": nil, "internet": nil, "dns": nil, "protection": nil,
	}
	re := regexp.MustCompile(`Page: "([a-z]+)(?:/([a-z]+))?[?"]`)
	n := 0
	for _, f := range []string{"security.go", "diag_why.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			n++
			list, ok := tabs[m[1]]
			if !ok {
				t.Errorf("%s: a button opens the %q page, which the test does not know", f, m[1])
				continue
			}
			if m[2] != "" && !strings.Contains(" "+strings.Join(list, " ")+" ", " "+m[2]+" ") {
				t.Errorf("%s: a button opens %s/%s, but %s has no such tab", f, m[1], m[2], m[1])
			}
		}
	}
	if n < 10 {
		t.Fatalf("only %d buttons found; the test cannot tell whether the list is right", n)
	}
}

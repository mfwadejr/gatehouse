package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A screen that asks to stream a topic the server does not know gets its whole stream refused
// (every topic on it, not just the unknown one), so live updates quietly stop.
func TestEveryTopicAScreenStreamsIsRegistered(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	src := filepath.Join("..", "..", "web", "src")
	if _, err := os.Stat(src); err != nil {
		t.Skip("no web sources here")
	}
	re := regexp.MustCompile(`topics:\s*\[([^\]]*)\]`)
	q := regexp.MustCompile(`'([a-z.]+)'`)
	seen := map[string]string{}
	filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".jsx") && !strings.HasSuffix(p, ".js") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			for _, n := range q.FindAllStringSubmatch(m[1], -1) {
				seen[n[1]] = p
			}
		}
		return nil
	})
	if len(seen) == 0 {
		t.Fatal("found no topics; the pattern is stale")
	}
	for name, file := range seen {
		if !c.app.API.HasTopic(name) {
			t.Errorf("%s asks to stream %q, which the server does not register", file, name)
		}
	}
}

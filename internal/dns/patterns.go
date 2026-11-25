package dnssrv

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Pattern rules. In "Always allow or block" a rule can be a name (which also covers everything under
// it) or a pattern that is matched against the whole name:
//
//	glob:ads*.example.com    * is any run of letters, digits, dots and dashes; ? is one of them
//	re:^track[0-9]+\.        a regular expression, for people who know them (advanced)
//
// Go's expression engine runs in time proportional to the name, so even a badly written pattern
// cannot make a lookup slow. Allow still beats block, and a plain-name allow beats a pattern block.

const (
	maxPatternLen   = 200
	maxPatternRules = 100
	patternPrefixG  = "glob:"
	patternPrefixR  = "re:"
)

// patternProbes are ordinary names. A block pattern that matches nearly all of them would block the
// whole internet, so it is refused.
var patternProbes = []string{"example.com", "www.google.com", "mail.example.org", "a.b.c.d.example.net", "localhost"}

type patRule struct {
	text string // what the person typed, for the log and the list
	re   *regexp.Regexp
}

// IsPattern says whether a stored rule is a pattern.
func IsPattern(stored string) bool {
	return strings.HasPrefix(stored, patternPrefixG) || strings.HasPrefix(stored, patternPrefixR)
}

// PatternText returns the pattern as typed, without its stored prefix, and its kind ("glob" or "re").
func PatternText(stored string) (kind, text string) {
	switch {
	case strings.HasPrefix(stored, patternPrefixG):
		return "glob", strings.TrimPrefix(stored, patternPrefixG)
	case strings.HasPrefix(stored, patternPrefixR):
		return "re", strings.TrimPrefix(stored, patternPrefixR)
	}
	return "", stored
}

// StorePattern checks a pattern and returns the form that is kept in the rules table.
func StorePattern(kind, text string) (string, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if _, err := CompilePattern(kind, text); err != nil {
		return "", err
	}
	if kind == "re" {
		return patternPrefixR + text, nil
	}
	return patternPrefixG + text, nil
}

// CompilePattern turns what a person typed into something that can be matched. The errors are in
// plain words.
func CompilePattern(kind, text string) (*regexp.Regexp, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil, errors.New("type a pattern")
	}
	if len(text) > maxPatternLen {
		return nil, fmt.Errorf("that pattern is too long (the limit is %d characters)", maxPatternLen)
	}
	var src string
	switch kind {
	case "glob":
		var b strings.Builder
		b.WriteString("^")
		for _, c := range text {
			switch {
			case c == '*':
				b.WriteString("[a-z0-9._-]*")
			case c == '?':
				b.WriteString("[a-z0-9_-]")
			case c == '.':
				b.WriteString(`\.`)
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
				b.WriteRune(c)
			default:
				return nil, fmt.Errorf("%q cannot be used in a name pattern. Use letters, digits, dots, dashes, * and ?", string(c))
			}
		}
		b.WriteString("$")
		src = b.String()
	case "re":
		src = text
	default:
		return nil, errors.New("choose a name pattern or a regular expression")
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("that is not a valid pattern: %s", cleanReErr(err))
	}
	if re.MatchString("") {
		return nil, errors.New("that pattern matches an empty name, so it would match everything")
	}
	hit := 0
	for _, p := range patternProbes {
		if re.MatchString(p) {
			hit++
		}
	}
	if hit >= len(patternProbes)-1 {
		return nil, errors.New("that pattern matches almost every name on the internet. Make it more specific")
	}
	return re, nil
}

func cleanReErr(err error) string {
	s := err.Error()
	if i := strings.Index(s, "error parsing regexp: "); i >= 0 {
		s = s[i+len("error parsing regexp: "):]
	}
	return s
}

// TryPattern says which of the given names a pattern would catch.
func TryPattern(kind, text string, names []string) ([]string, error) {
	re, err := CompilePattern(kind, text)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if re.MatchString(strings.ToLower(strings.TrimSuffix(n, "."))) {
			out = append(out, n)
		}
	}
	return out, nil
}

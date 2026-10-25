package installer

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// asciiMap turns the drawing characters the screens use into plain ASCII.
var asciiMap = map[rune]string{
	'·': ".", '…': "...",
	'─': "-", '│': "|", '╭': "+", '╮': "+", '╰': "+", '╯': "+",
	'█': "#", '▄': "_", '▀': "^", '░': ".", '▒': ":", '▓': "#",
	'●': "*", '○': "o",
}

// ASCII returns s with every character outside plain ASCII replaced by a look-alike.
func ASCII(s string) string {
	ok := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ok = false
			break
		}
	}
	if ok {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case asciiMap[r] != "":
			b.WriteString(asciiMap[r])
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

type asciiWriter struct{ w io.Writer }

// Write sends p as plain ASCII. It reports len(p) so callers see a complete write.
func (a asciiWriter) Write(p []byte) (int, error) {
	if !utf8.Valid(p) {
		// A torn character: keep the plain bytes and drop the rest.
		q := make([]byte, 0, len(p))
		for _, c := range p {
			if c < 0x80 {
				q = append(q, c)
			}
		}
		_, err := a.w.Write(q)
		return len(p), err
	}
	_, err := io.WriteString(a.w, ASCII(string(p)))
	return len(p), err
}

// plainSerial wraps a serial port so that nothing but ASCII reaches it. Serial terminals
// (screen, minicom, PuTTY) are often not set to UTF-8, and a UTF-8 box-drawing character then
// switches them into line-drawing mode and every lowercase letter turns into a glyph.
func plainSerial(w io.Writer) io.Writer { return asciiWriter{w} }

// isSerialStdout says whether this process's output is a serial port.
func isSerialStdout() bool {
	p, err := os.Readlink("/proc/self/fd/1")
	return err == nil && strings.HasPrefix(p, "/dev/ttyS")
}

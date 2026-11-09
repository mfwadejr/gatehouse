package installer

import (
	"fmt"
	"nopewarden/internal/gfx"
	"strings"
	"unicode/utf8"
)

// The look of the machine's own screens. Everything the person sees on the device itself,
// from the installer to the status screen, is drawn from the helpers in this file so that
// the screens match each other and the boot menu and splash before them.
//
// Layouts are designed for 80x24, the smallest screen the large console font gives on a
// laptop display, and centre themselves on anything bigger.

// asciiReset puts a terminal back on plain ASCII: shift-in, then ASCII for both character
// sets. A serial terminal can be left in line-drawing mode by earlier boot output, which turns
// every lowercase letter into a box glyph.
const asciiReset = "\x0f\x1b(B\x1b)B"

// Colours come from the design tokens (tokens/tokens.json, console.*). Only the sixteen console colours
// exist, so each meaning gets one slot, and "bold" is never used for colour (the console would swap in a
// default colour for it).
const palette = gfx.PaletteEscapes

const (
	reset = "\x1b[0m"
	// dim is the secondary text (notes, hints, step lines). It is the normal text colour, not "bright black"
	// (colour 90): many terminal themes draw that very dark, and a serial terminal ignores our palette, so
	// grey notes could not be read. Headings stay brighter (bold), which keeps the order of importance.
	dim   = "\x1b[0;37m"
	fg    = "\x1b[0;37m" // text
	bold  = "\x1b[0;97m" // bright text
	blue  = "\x1b[0;34m"
	green = "\x1b[0;32m"
	amber = "\x1b[0;33m"
	red   = "\x1b[0;31m"
)

// visible is the width of s on screen, ignoring colour codes.
func visible(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			i = j + 1
			continue
		}
		_, w := utf8.DecodeRuneInString(s[i:])
		n++
		i += w
	}
	return n
}

// centre pads s so it sits in the middle of a line w columns wide.
func centre(s string, w int) string {
	pad := (w - visible(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s
}

var shield = gfx.Shield

// PlainHeader leaves the block-character badge out of the headers. The badge is a stand-in that only the
// graphical screen turns into the real logo; on a serial terminal it arrives as raw block characters and
// does not draw cleanly, so the installer sets this whenever a serial console is attached. The name stays.
var PlainHeader bool

// badgeMark tags the badge's lines (and the blank line under them) so a screen that is too tall for the
// display can drop them first. The tag never reaches the display.
const badgeMark = "\x00badge"

// Screen collects lines and turns them into one frame.
type Screen struct {
	W, H  int
	lines []string
}

func NewScreen(w, h int) *Screen {
	if w < 60 {
		w = 80
	}
	if h < 20 {
		h = 24
	}
	return &Screen{W: w, H: h}
}

func (s *Screen) add(l string) { s.lines = append(s.lines, l) }
func (s *Screen) blank()       { s.lines = append(s.lines, "") }
func (s *Screen) mid(l string) { s.lines = append(s.lines, centre(l, s.W)) }
func (s *Screen) midBox(ls []string) {
	// A rounded box around ls, in muted blue, centred.
	w := 0
	for _, l := range ls {
		if v := visible(l); v > w {
			w = v
		}
	}
	w += 6
	b := blue
	s.mid(b + "╭" + strings.Repeat("─", w) + "╮" + reset)
	for _, l := range ls {
		pad := w - 6 - visible(l)
		left := pad / 2
		s.mid(b + "│" + reset + "   " + strings.Repeat(" ", left) + l + strings.Repeat(" ", pad-left) + "   " + b + "│" + reset)
	}
	s.mid(b + "╰" + strings.Repeat("─", w) + "╯" + reset)
}

// header draws the shield, the name and what this screen is for.
func (s *Screen) header(sub string) {
	if !PlainHeader {
		for _, l := range shield {
			s.lines = append(s.lines, badgeMark+centre(blue+l+reset, s.W))
		}
		s.lines = append(s.lines, badgeMark)
	}
	s.mid(bold + "N O P E W A R D E N" + reset)
	if sub != "" {
		s.mid(dim + sub + reset)
	}
	s.blank()
}

// portsLine is one line of dots, one per port: lit when a cable is connected.
func portsLine(ports []Port) string {
	var parts []string
	for _, p := range ports {
		name := p.Name
		if p.USB {
			name += " (USB)"
		}
		if p.Link {
			sp := ""
			if p.Speed != "" {
				sp = " " + p.Speed + " Mb/s"
			}
			parts = append(parts, green+"●"+reset+fg+" "+name+dim+sp+reset)
		} else {
			parts = append(parts, dim+"○ "+name+reset)
		}
	}
	return strings.Join(parts, "    ")
}

// String lays the lines out: centred vertically on tall screens, the last line pinned to the
// bottom (the footer), and never taller than the screen.
func (s *Screen) String() string {
	lines := append([]string(nil), s.lines...)
	footer := ""
	if n := len(lines); n > 0 && strings.HasPrefix(lines[n-1], "\x00footer") {
		footer = strings.TrimPrefix(lines[n-1], "\x00footer")
		lines = lines[:n-1]
	}
	room := s.H - 1 // the last row is left alone so the screen never scrolls
	avail := room
	if footer != "" {
		avail = room - 1
	}
	// Too tall for the display: the badge goes first, then blank lines from the bottom up, so the name and
	// the choices are never the part that is cut off.
	if len(lines) > avail {
		kept := lines[:0:0]
		for _, l := range lines {
			if !strings.HasPrefix(l, badgeMark) {
				kept = append(kept, l)
			}
		}
		lines = kept
	}
	for len(lines) > avail {
		i := len(lines) - 1
		for i >= 0 && lines[i] != "" {
			i--
		}
		if i < 0 {
			lines = lines[:avail]
			break
		}
		lines = append(lines[:i], lines[i+1:]...)
	}
	// Centred top to bottom in the space above the footer.
	top := (avail - len(lines)) / 2
	if top < 0 {
		top = 0
	}
	var b strings.Builder
	b.WriteString(asciiReset + "\x1b[?25l\x1b[0m\x1b[2J\x1b[H")
	used := 0
	for i := 0; i < top; i++ {
		b.WriteString("\r\n")
		used++
	}
	for _, l := range lines {
		b.WriteString(strings.TrimPrefix(l, badgeMark) + reset + "\r\n")
		used++
	}
	if footer != "" {
		for used < room-1 {
			b.WriteString("\r\n")
			used++
		}
		b.WriteString(centre(footer, s.W) + reset)
	}
	return b.String()
}

// footer sets the line pinned to the bottom of the screen.
func (s *Screen) footer(l string) { s.lines = append(s.lines, "\x00footer"+l) }

// bar is a progress bar w cells wide.
func bar(done, total, w int) string {
	if total < 1 {
		total = 1
	}
	n := w * done / total
	return blue + strings.Repeat("█", n) + dim + strings.Repeat("░", w-n) + reset
}

func fmtf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

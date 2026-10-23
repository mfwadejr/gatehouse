package installer

import (
	"fmt"
	"gatehouse/internal/gfx"
	"strings"
	"unicode/utf8"
)

// The look of the machine's own screens. Everything the person sees on the device itself,
// from the installer to the status screen, is drawn from the helpers in this file so that
// the screens match each other and the boot menu and splash before them.
//
// Layouts are designed for 80x24, the smallest screen the large console font gives on a
// laptop display, and centre themselves on anything bigger.

// Colours come from the design tokens. Only the sixteen console colours exist, so each
// meaning gets one slot, and "bold" is never used for colour (the console would swap in a
// default colour for it).
const palette = "" +
	"\x1b]P0121211" + // background
	"\x1b]P1e5484d" + // red
	"\x1b]P24cb06a" + // green
	"\x1b]P3e5b43a" + // amber
	"\x1b]P43987e5" + // blue
	"\x1b]P5a78bfa" +
	"\x1b]P622b8cf" +
	"\x1b]P7c3c2b7" + // text
	"\x1b]P8898781" + // muted
	"\x1b]P9e5484d\x1b]PA4cb06a\x1b]PBe5b43a\x1b]PC3987e5\x1b]PDa78bfa\x1b]PE22b8cf" +
	"\x1b]PFf0efec" // bright text

const (
	reset = "\x1b[0m"
	dim   = "\x1b[0;90m" // muted
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
	for _, l := range shield {
		s.mid(blue + l + reset)
	}
	s.blank()
	s.mid(bold + "G A T E H O U S E" + reset)
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
	lines := s.lines
	footer := ""
	if n := len(lines); n > 0 && strings.HasPrefix(lines[n-1], "\x00footer") {
		footer = strings.TrimPrefix(lines[n-1], "\x00footer")
		lines = lines[:n-1]
	}
	top := (s.H - 24) / 3
	if top < 0 {
		top = 0
	}
	room := s.H - 1
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[0m\x1b[2J\x1b[H")
	used := 0
	for i := 0; i < top; i++ {
		b.WriteString("\r\n")
		used++
	}
	for _, l := range lines {
		if used >= room-1 {
			break
		}
		b.WriteString(l + reset + "\r\n")
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

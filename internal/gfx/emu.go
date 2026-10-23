// Package gfx draws the machine's own screens as graphics instead of console text: smooth type and the
// real shield, with the same colours as the web console. It is a small terminal emulator that paints
// onto the frame buffer, so the installer and status screens need no changes to be drawn this way.
package gfx

import (
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Palette is the console's 16 colours: the design tokens, as in the web console.
var Palette = [16]color.RGBA{
	{0x12, 0x12, 0x11, 255}, {0xe5, 0x48, 0x4d, 255}, {0x4c, 0xb0, 0x6a, 255}, {0xe5, 0xb4, 0x3a, 255},
	{0x39, 0x87, 0xe5, 255}, {0xa7, 0x8b, 0xfa, 255}, {0x22, 0xb8, 0xcf, 255}, {0xc3, 0xc2, 0xb7, 255},
	{0x89, 0x87, 0x81, 255}, {0xe5, 0x48, 0x4d, 255}, {0x4c, 0xb0, 0x6a, 255}, {0xe5, 0xb4, 0x3a, 255},
	{0x39, 0x87, 0xe5, 255}, {0xa7, 0x8b, 0xfa, 255}, {0x22, 0xb8, 0xcf, 255}, {0xf0, 0xef, 0xec, 255},
}

type cell struct {
	r      rune
	fg, bg uint8
}

// Emu is the screen's contents.
type Emu struct {
	Cols, Rows int
	cells      []cell
	x, y       int
	fg, bg     uint8
	rev        bool
	Cursor     bool
	pal        [16]color.RGBA

	pend  []byte
	state int // 0 ground, 1 esc, 2 csi, 3 osc, 4 osc-palette, 5 string (apc/dcs), 6 esc-in-string
	csi   []byte
	osc   []byte
}

// NewEmu makes a blank screen.
func NewEmu(cols, rows int) *Emu {
	e := &Emu{Cols: cols, Rows: rows, fg: 7, bg: 0, pal: Palette}
	e.cells = make([]cell, cols*rows)
	e.clear()
	return e
}

func (e *Emu) blank() cell { return cell{' ', 7, 0} }

func (e *Emu) clear() {
	for i := range e.cells {
		e.cells[i] = e.blank()
	}
	e.x, e.y = 0, 0
}

func (e *Emu) scroll() {
	copy(e.cells, e.cells[e.Cols:])
	for i := len(e.cells) - e.Cols; i < len(e.cells); i++ {
		e.cells[i] = e.blank()
	}
}

func (e *Emu) put(r rune) {
	if e.x >= e.Cols {
		e.x = 0
		e.lf()
	}
	fg, bg := e.fg, e.bg
	if e.rev {
		fg, bg = bg, fg
	}
	e.cells[e.y*e.Cols+e.x] = cell{r, fg, bg}
	e.x++
}

func (e *Emu) lf() {
	e.y++
	if e.y >= e.Rows {
		e.scroll()
		e.y = e.Rows - 1
	}
}

// Write feeds terminal output to the screen.
func (e *Emu) Write(p []byte) (int, error) {
	e.pend = append(e.pend, p...)
	for len(e.pend) > 0 {
		c := e.pend[0]
		switch e.state {
		case 0:
			switch {
			case c == 0x1b:
				e.state = 1
				e.pend = e.pend[1:]
			case c == '\r':
				e.x = 0
				e.pend = e.pend[1:]
			case c == '\n':
				e.lf()
				e.pend = e.pend[1:]
			case c == '\b':
				if e.x > 0 {
					e.x--
				}
				e.pend = e.pend[1:]
			case c == '\t':
				e.x = (e.x/8 + 1) * 8
				e.pend = e.pend[1:]
			case c < 0x20 || c == 0x7f:
				e.pend = e.pend[1:]
			default:
				if !utf8.FullRune(e.pend) {
					return len(p), nil
				}
				r, n := utf8.DecodeRune(e.pend)
				e.put(r)
				e.pend = e.pend[n:]
			}
		case 1:
			e.pend = e.pend[1:]
			switch c {
			case '[':
				e.state, e.csi = 2, e.csi[:0]
			case ']':
				e.state, e.osc = 3, e.osc[:0]
			case '_', 'P', '^', 'X':
				e.state = 5
			default:
				e.state = 0
			}
		case 2:
			e.pend = e.pend[1:]
			if c >= 0x40 && c <= 0x7e {
				e.doCSI(c)
				e.state = 0
			} else {
				e.csi = append(e.csi, c)
			}
		case 3:
			// The console's palette command has no terminator: ESC ] P n rrggbb.
			if len(e.osc) == 0 && c == 'P' {
				if len(e.pend) < 8 {
					return len(p), nil
				}
				if i, err := strconv.ParseUint(string(e.pend[1:2]), 16, 8); err == nil {
					if v, err := strconv.ParseUint(string(e.pend[2:8]), 16, 32); err == nil {
						e.pal[i] = color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
					}
				}
				e.pend = e.pend[8:]
				e.state = 0
				continue
			}
			e.pend = e.pend[1:]
			if c == 7 {
				e.state = 0
			} else if c == 0x1b {
				e.state = 6
			} else {
				e.osc = append(e.osc, c)
			}
		case 5:
			e.pend = e.pend[1:]
			if c == 0x1b {
				e.state = 6
			} else if c == 7 {
				e.state = 0
			}
		case 6:
			e.pend = e.pend[1:]
			e.state = 0
		}
	}
	return len(p), nil
}

func params(b []byte) []int {
	s := strings.TrimLeft(string(b), "?>=")
	if s == "" {
		return nil
	}
	var out []int
	for _, f := range strings.Split(s, ";") {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	return out
}

func (e *Emu) doCSI(final byte) {
	private := len(e.csi) > 0 && e.csi[0] == '?'
	ps := params(e.csi)
	arg := func(i, def int) int {
		if i < len(ps) && ps[i] > 0 {
			return ps[i]
		}
		return def
	}
	switch final {
	case 'm':
		if len(ps) == 0 {
			ps = []int{0}
		}
		for _, p := range ps {
			switch {
			case p == 0:
				e.fg, e.bg, e.rev = 7, 0, false
			case p == 7:
				e.rev = true
			case p == 27:
				e.rev = false
			case p >= 30 && p <= 37:
				e.fg = uint8(p - 30)
			case p == 39:
				e.fg = 7
			case p >= 40 && p <= 47:
				e.bg = uint8(p - 40)
			case p == 49:
				e.bg = 0
			case p >= 90 && p <= 97:
				e.fg = uint8(p-90) + 8
			case p >= 100 && p <= 107:
				e.bg = uint8(p-100) + 8
			}
		}
	case 'h', 'l':
		if private && len(ps) > 0 && ps[0] == 25 {
			e.Cursor = final == 'h'
		}
	case 'H', 'f':
		e.y, e.x = clamp(arg(0, 1)-1, 0, e.Rows-1), clamp(arg(1, 1)-1, 0, e.Cols-1)
	case 'A':
		e.y = clamp(e.y-arg(0, 1), 0, e.Rows-1)
	case 'B':
		e.y = clamp(e.y+arg(0, 1), 0, e.Rows-1)
	case 'C':
		e.x = clamp(e.x+arg(0, 1), 0, e.Cols-1)
	case 'D':
		e.x = clamp(e.x-arg(0, 1), 0, e.Cols-1)
	case 'G':
		e.x = clamp(arg(0, 1)-1, 0, e.Cols-1)
	case 'J':
		switch {
		case len(ps) == 0 || ps[0] == 0:
			for i := e.y*e.Cols + e.x; i < len(e.cells); i++ {
				e.cells[i] = e.blank()
			}
		case ps[0] == 1:
			for i := 0; i <= e.y*e.Cols+e.x && i < len(e.cells); i++ {
				e.cells[i] = e.blank()
			}
		default:
			x, y := e.x, e.y
			e.clear()
			e.x, e.y = x, y
		}
	case 'K':
		from, to := e.x, e.Cols
		if len(ps) > 0 {
			switch ps[0] {
			case 1:
				from, to = 0, e.x+1
			case 2:
				from, to = 0, e.Cols
			}
		}
		for x := from; x < to && x < e.Cols; x++ {
			e.cells[e.y*e.Cols+x] = e.blank()
		}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Text returns the screen as plain text, for tests.
func (e *Emu) Text() string {
	var b strings.Builder
	for y := 0; y < e.Rows; y++ {
		line := make([]rune, e.Cols)
		for x := range line {
			line[x] = e.cells[y*e.Cols+x].r
		}
		b.WriteString(strings.TrimRight(string(line), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

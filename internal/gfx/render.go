package gfx

import (
	"image"
	"image/color"
	"math"
	"sync"
)

// Shield is the logo drawn in block characters. On a plain console it is shown as is; on the graphical
// screen the same rows are recognised and replaced by the real, smooth logo.
var Shield = []string{
	"  ▄▄███████▄▄  ",
	" ██▀▀     ▀▀██ ",
	" ██   ▄▄▄   ██ ",
	"  ▀█▄     ▄█▀  ",
	"    ▀█▄ ▄█▀    ",
	"      ▀▀▀      ",
}

// Metrics tells how big a cell is at a scale (1 for ordinary screens, 2 for very sharp ones).
func Metrics(scale int) (cw, ch int) {
	if scale >= 2 {
		return large.cw, large.ch
	}
	return small.cw, small.ch
}

// ScaleFor picks the cell size that suits a screen width.
func ScaleFor(width int) int {
	if width >= 2560 {
		return 2
	}
	return 1
}

var (
	logoOnce sync.Once
	logoImg  *image.NRGBA
	logoMu   sync.Mutex
	logoMemo = map[int]*image.NRGBA{}
)

// logoAt is the logo scaled to size by averaging, which keeps its edges smooth.
func logoAt(size int) *image.NRGBA {
	logoOnce.Do(func() { logoImg = loadLogo() })
	logoMu.Lock()
	defer logoMu.Unlock()
	if m, ok := logoMemo[size]; ok {
		return m
	}
	src := logoImg
	sw := src.Bounds().Dx()
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0, y1 := y*sw/size, (y+1)*sw/size
		for x := 0; x < size; x++ {
			x0, x1 := x*sw/size, (x+1)*sw/size
			var r, g, b, a, n int
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					p := src.NRGBAAt(xx, yy)
					r += int(p.R) * int(p.A)
					g += int(p.G) * int(p.A)
					b += int(p.B) * int(p.A)
					a += int(p.A)
					n++
				}
			}
			if a > 0 {
				out.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / n)})
			}
		}
	}
	logoMemo[size] = out
	return out
}

// findShield looks for the block-character shield and returns where its first row starts.
func (e *Emu) findShield() (x, y int, ok bool) {
	w := len([]rune(Shield[0]))
	for y = 0; y+len(Shield) <= e.Rows; y++ {
		for x = 0; x+w <= e.Cols; x++ {
			if e.shieldAt(x, y) {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

func (e *Emu) shieldAt(x, y int) bool {
	for j, row := range Shield {
		for i, r := range []rune(row) {
			c := e.cells[(y+j)*e.Cols+x+i]
			if c.r != r || (r != ' ' && c.fg != 4) {
				return false
			}
		}
	}
	return true
}

func blend(dst *image.RGBA, x, y int, c color.RGBA, a float64) {
	if a <= 0 || !(image.Pt(x, y).In(dst.Rect)) {
		return
	}
	if a > 1 {
		a = 1
	}
	o := dst.PixOffset(x, y)
	p := dst.Pix[o : o+4]
	p[0] = uint8(float64(p[0])*(1-a) + float64(c.R)*a)
	p[1] = uint8(float64(p[1])*(1-a) + float64(c.G)*a)
	p[2] = uint8(float64(p[2])*(1-a) + float64(c.B)*a)
	p[3] = 255
}

// Render draws the screen into a new w by h image. Cells are centred in whatever is left over.
func (e *Emu) Render(w, h, scale int) *image.RGBA {
	at := small
	if scale >= 2 {
		at = large
	}
	cw, ch := at.cw, at.ch
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := e.pal[0]
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bg.R, bg.G, bg.B, 255
	}
	ox, oy := (w-e.Cols*cw)/2, (h-e.Rows*ch)/2
	sx, sy, hasLogo := e.findShield()
	if hasLogo {
		for j := range Shield {
			for i := range []rune(Shield[j]) {
				e.cells[(sy+j)*e.Cols+sx+i].r = ' '
			}
		}
	}
	th := 2 * scale
	for y := 0; y < e.Rows; y++ {
		for x := 0; x < e.Cols; x++ {
			c := e.cells[y*e.Cols+x]
			px, py := ox+x*cw, oy+y*ch
			if c.bg != 0 {
				fill(img, px, py, cw, ch, e.pal[c.bg&15])
			}
			if c.r == ' ' {
				continue
			}
			fg := e.pal[c.fg&15]
			if !special(img, c.r, px, py, cw, ch, th, fg, e.pal[c.bg&15]) {
				gx, gy := at.glyph(c.r)
				for j := 0; j < ch; j++ {
					for i := 0; i < cw; i++ {
						if v := at.img.GrayAt(gx+i, gy+j).Y; v > 0 {
							blend(img, px+i, py+j, fg, float64(v)/255)
						}
					}
				}
			}
		}
	}
	if hasLogo {
		size := int(float64(ch) * 6.6)
		lg := logoAt(size)
		cx := ox + (sx+len([]rune(Shield[0]))/2)*cw + cw/2*(len([]rune(Shield[0]))%2)
		cy := oy + sy*ch + 3*ch + ch/3
		x0, y0 := cx-size/2, cy-size/2
		for j := 0; j < size; j++ {
			for i := 0; i < size; i++ {
				p := lg.NRGBAAt(i, j)
				if p.A > 0 {
					blend(img, x0+i, y0+j, color.RGBA{p.R, p.G, p.B, 255}, float64(p.A)/255)
				}
			}
		}
	}
	if e.Cursor && e.x < e.Cols {
		fill(img, ox+e.x*cw, oy+e.y*ch+ch-3*scale-2, cw, 2*scale, e.pal[7])
	}
	return img
}

func fill(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			blend(img, i, j, c, 1)
		}
	}
}

// special draws characters that are shapes rather than letters: blocks, dots and box lines.
func special(img *image.RGBA, r rune, px, py, cw, ch, th int, fg, bg color.RGBA) bool {
	cx, cy := cw/2, ch/2
	line := func(x0, y0, x1, y1 int) { fill(img, px+x0, py+y0, x1-x0, y1-y0, fg) }
	switch r {
	case '█':
		line(0, 0, cw, ch)
	case '▄':
		line(0, ch/2, cw, ch)
	case '▀':
		line(0, 0, cw, ch/2)
	case '░':
		fill(img, px, py+ch/4, cw, ch/2, color.RGBA{uint8((int(bg.R)*3 + int(fg.R)) / 4), uint8((int(bg.G)*3 + int(fg.G)) / 4), uint8((int(bg.B)*3 + int(fg.B)) / 4), 255})
	case '●', '○':
		rad := float64(cw) * 0.36
		for j := 0; j < ch; j++ {
			for i := 0; i < cw; i++ {
				d := math.Hypot(float64(i)-float64(cw)/2+0.5, float64(j)-float64(ch)/2+0.5)
				a := rad + 0.5 - d
				if r == '○' {
					a = math.Min(a, float64(th)/2+0.5-math.Abs(d-rad+float64(th)/2-0.5))
				}
				blend(img, px+i, py+j, fg, a)
			}
		}
	case '─':
		line(0, cy-th/2, cw, cy+th/2)
	case '│':
		line(cx-th/2, 0, cx+th/2, ch)
	case '╭', '╮', '╰', '╯':
		rad := float64(cw) / 2
		// the arc's centre sits inward of the corner, on the cell's middle lines
		acx, acy := float64(cx), float64(cy)
		var dx, dy float64 // direction toward the arc centre from the corner point
		switch r {
		case '╭':
			dx, dy = 1, 1
		case '╮':
			dx, dy = -1, 1
		case '╰':
			dx, dy = 1, -1
		case '╯':
			dx, dy = -1, -1
		}
		ccx, ccy := acx+dx*rad, acy+dy*rad
		for j := 0; j < ch; j++ {
			for i := 0; i < cw; i++ {
				fx, fy := float64(i)+0.5, float64(j)+0.5
				qx, qy := (fx-acx)*dx, (fy-acy)*dy // inside the quadrant that holds the arc
				if qx >= 0 && qy >= 0 && qx <= rad && qy <= rad {
					d := math.Hypot(fx-ccx, fy-ccy)
					blend(img, px+i, py+j, fg, float64(th)/2+0.5-math.Abs(d-rad))
				}
			}
		}
		// the straight parts leave the arc toward the cell's edges
		if dy > 0 {
			line(cx-th/2, cy+int(rad), cx+th/2, ch)
		} else {
			line(cx-th/2, 0, cx+th/2, cy-int(rad))
		}
		if dx > 0 {
			line(cx+int(rad), cy-th/2, cw, cy+th/2)
		} else {
			line(0, cy-th/2, cx-int(rad), cy+th/2)
		}
		_ = dx
	default:
		return false
	}
	return true
}

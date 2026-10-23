package gfx

import (
	"strings"
	"testing"
	"time"
)

func TestEmuTextColoursAndCursor(t *testing.T) {
	e := NewEmu(20, 5)
	e.Write([]byte("\x1b[?25l\x1b[0m\x1b[2J\x1b[H\x1b[0;32mhi\x1b[0m there\r\nsecond\x1b[3;5H!\x1b[K"))
	txt := e.Text()
	if !strings.HasPrefix(txt, "hi there\nsecond\n    !") {
		t.Fatalf("%q", txt)
	}
	if e.cells[0].fg != 2 || e.cells[3].fg != 7 {
		t.Fatalf("colours: %d %d", e.cells[0].fg, e.cells[3].fg)
	}
	if e.Cursor {
		t.Fatal("cursor should be hidden")
	}
	e.Write([]byte("\x1b[?25h"))
	if !e.Cursor {
		t.Fatal("cursor should show")
	}
}

func TestEmuEscapesSplitAcrossWritesAndPalette(t *testing.T) {
	e := NewEmu(20, 3)
	e.Write([]byte("\x1b]P1ff0000\x1b[3"))
	e.Write([]byte("1mR"))
	if e.pal[1].R != 255 || e.pal[1].G != 0 {
		t.Fatalf("palette: %+v", e.pal[1])
	}
	if e.cells[0].r != 'R' || e.cells[0].fg != 1 {
		t.Fatalf("split sequence mishandled: %+v", e.cells[0])
	}
	// Multi-byte characters split between writes survive.
	e.Write([]byte("\xe2\x97"))
	e.Write([]byte("\x8f"))
	if e.cells[1].r != '●' {
		t.Fatalf("got %q", e.cells[1].r)
	}
}

func TestEmuScrolls(t *testing.T) {
	e := NewEmu(10, 2)
	e.Write([]byte("a\r\nb\r\nc"))
	if got := e.Text(); got != "b\nc\n" {
		t.Fatalf("%q", got)
	}
}

func TestShieldBecomesTheLogo(t *testing.T) {
	e := NewEmu(60, 14)
	var b strings.Builder
	for _, l := range Shield {
		b.WriteString(strings.Repeat(" ", 20) + "\x1b[0;34m" + l + "\x1b[0m\r\n")
	}
	e.Write([]byte(b.String()))
	img := e.Render(60*16, 14*32, 1)
	if _, _, ok := e.findShield(); ok {
		t.Fatal("the block shield should have been replaced")
	}
	// The middle of the shield is the dash, drawn in the logo's blue; the corner of the screen is background.
	c := img.RGBAAt(27*16+8, 3*32+10)
	if c.B < 150 || c.R > 120 {
		t.Fatalf("no blue logo pixels at the centre: %+v", c)
	}
	if bg := img.RGBAAt(2, 2); bg.R != 0x12 {
		t.Fatalf("background %+v", bg)
	}
}

func TestRenderIsFastEnoughToType(t *testing.T) {
	e := NewEmu(85, 24)
	e.Write([]byte("hello world, this is a screen of text"))
	start := time.Now()
	for i := 0; i < 5; i++ {
		e.Render(1366, 768, 1)
	}
	if per := time.Since(start) / 5; per > 250*time.Millisecond {
		t.Fatalf("a redraw takes %v", per)
	}
}

func TestBlitPacksPixelsForTheFrameBuffer(t *testing.T) {
	img := NewEmu(2, 1).Render(4, 2, 1)
	img.SetRGBA(1, 0, Palette[4]) // 3987e5
	d := &Display{w: 4, h: 2, bpp: 32, stride: 16, mem: make([]byte, 32), r: channel{16, 8}, g: channel{8, 8}, b: channel{0, 8}}
	d.blit(img)
	if got := d.mem[4:8]; got[0] != 0xe5 || got[1] != 0x87 || got[2] != 0x39 {
		t.Fatalf("32-bit BGRX: % x", got)
	}
	d = &Display{w: 4, h: 2, bpp: 16, stride: 8, mem: make([]byte, 16), r: channel{11, 5}, g: channel{5, 6}, b: channel{0, 5}}
	d.blit(img)
	want := uint16(0x39>>3)<<11 | uint16(0x87>>2)<<5 | uint16(0xe5>>3)
	if got := uint16(d.mem[2]) | uint16(d.mem[3])<<8; got != want {
		t.Fatalf("16-bit: %04x want %04x", got, want)
	}
}

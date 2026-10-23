package gfx

import (
	"bytes"
	_ "embed"
	"image"
	"image/png"
	"strings"
)

//go:embed atlas1.png
var atlas1PNG []byte

//go:embed atlas2.png
var atlas2PNG []byte

//go:embed logo.png
var logoPNG []byte

// chars is the order of glyphs in the atlases. It must match brand/make_gfx.py.
const chars = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~·—–…’“”→←↑↓✓×°é"

const perRow = 16

type atlas struct {
	img    *image.Gray
	cw, ch int
}

func mustAtlas(b []byte, cw, ch int) *atlas {
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	g, ok := im.(*image.Gray)
	if !ok {
		panic("font atlas is not grey")
	}
	return &atlas{g, cw, ch}
}

var (
	small = mustAtlas(atlas1PNG, 16, 32)
	large = mustAtlas(atlas2PNG, 32, 64)
)

// glyph returns the top-left corner of r in the atlas; unknown characters become a question mark.
func (a *atlas) glyph(r rune) (int, int) {
	i := strings.IndexRune(chars, r)
	if i < 0 {
		i = strings.IndexRune(chars, '?')
	}
	return (i % perRow) * a.cw, (i / perRow) * a.ch
}

func loadLogo() *image.NRGBA {
	im, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		panic(err)
	}
	n := image.NewNRGBA(im.Bounds())
	for y := 0; y < im.Bounds().Dy(); y++ {
		for x := 0; x < im.Bounds().Dx(); x++ {
			n.Set(x, y, im.At(x, y))
		}
	}
	return n
}

// Code generated from tokens/tokens.json by tokens/build-brand.mjs. DO NOT EDIT.

package gfx

import "image/color"

// Palette is the console's 16 colours: the design tokens, as in the web console.
var Palette = [16]color.RGBA{
	{0x12, 0x12, 0x11, 255}, {0xe5, 0x48, 0x4d, 255}, {0x4c, 0xb0, 0x6a, 255}, {0xe5, 0xb4, 0x3a, 255},
	{0x39, 0x87, 0xe5, 255}, {0xa7, 0x8b, 0xfa, 255}, {0x22, 0xb8, 0xcf, 255}, {0xc3, 0xc2, 0xb7, 255},
	{0x89, 0x87, 0x81, 255}, {0xe5, 0x48, 0x4d, 255}, {0x4c, 0xb0, 0x6a, 255}, {0xe5, 0xb4, 0x3a, 255},
	{0x39, 0x87, 0xe5, 255}, {0xa7, 0x8b, 0xfa, 255}, {0x22, 0xb8, 0xcf, 255}, {0xf0, 0xef, 0xec, 255},
}

// PaletteEscapes sets the same 16 colours on a Linux console (ESC ] P n rrggbb).
const PaletteEscapes = "" +
	"\x1b]P0121211" +
	"\x1b]P1e5484d" +
	"\x1b]P24cb06a" +
	"\x1b]P3e5b43a" +
	"\x1b]P43987e5" +
	"\x1b]P5a78bfa" +
	"\x1b]P622b8cf" +
	"\x1b]P7c3c2b7" +
	"\x1b]P8898781" +
	"\x1b]P9e5484d" +
	"\x1b]PA4cb06a" +
	"\x1b]PBe5b43a" +
	"\x1b]PC3987e5" +
	"\x1b]PDa78bfa" +
	"\x1b]PE22b8cf" +
	"\x1b]PFf0efec"

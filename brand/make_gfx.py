#!/usr/bin/env python3
"""Draws what the machine's own screen needs: a smooth font atlas at two sizes and the shield.

Run from the repository root:  python3 brand/make_gfx.py
The PNGs are committed (internal/gfx), so building does not need Pillow. The font is DejaVu Sans
Mono, whose licence allows this. CHARS must match internal/gfx/font.go.
"""
import sys
sys.path.insert(0, "brand")
from PIL import Image, ImageDraw, ImageFont
import make_brand

CHARS = "".join(chr(c) for c in range(32, 127)) + "·—–…’“”→←↑↓✓×°é"
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
PER_ROW = 16

def atlas(cw, ch, size, path):
    f = ImageFont.truetype(FONT, size)
    rows = (len(CHARS) + PER_ROW - 1) // PER_ROW
    im = Image.new("L", (PER_ROW * cw, rows * ch), 0)
    d = ImageDraw.Draw(im)
    asc, desc = f.getmetrics()
    top = (ch - (asc + desc)) // 2
    for i, c in enumerate(CHARS):
        x, y = (i % PER_ROW) * cw, (i // PER_ROW) * ch
        w = f.getlength(c)
        d.text((x + (cw - w) / 2, y + top), c, font=f, fill=255)
    im.save(path, optimize=True)

atlas(16, 32, 27, "internal/gfx/atlas1.png")
atlas(32, 64, 54, "internal/gfx/atlas2.png")
make_brand.logo(512).save("internal/gfx/logo.png", optimize=True)
print("ok")

#!/usr/bin/env python3
"""Draws the boot-time artwork: menu backgrounds and the boot splash pieces.

Run from the repository root:  python3 brand/make_brand.py
The PNGs it writes are committed, so building the ISO does not need Pillow.
"""
import math, os, shutil
from PIL import Image, ImageDraw, ImageFont

BG = (18, 18, 17)        # tokens neutral 925
TEXT = (240, 239, 236)   # neutral 100
MUTED = (195, 194, 183)  # neutral 300
DIM = (137, 135, 129)    # neutral 500
BLUE = (57, 135, 229)    # blue 400
FONTS = ["/usr/share/fonts/opentype/inter/Inter-Regular.otf", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"]
FONTS_B = ["/usr/share/fonts/opentype/inter/Inter-SemiBold.otf", "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"]

def font(size, bold=False):
    for p in (FONTS_B if bold else FONTS):
        if os.path.exists(p):
            return ImageFont.truetype(p, size)
    return ImageFont.load_default()

def bez(p0, p1, p2, p3, n=24):
    out = []
    for i in range(1, n + 1):
        t = i / n
        a = (1 - t) ** 3; b = 3 * (1 - t) ** 2 * t; c = 3 * (1 - t) * t * t; d = t ** 3
        out.append((a * p0[0] + b * p1[0] + c * p2[0] + d * p3[0], a * p0[1] + b * p1[1] + c * p2[1] + d * p3[1]))
    return out

def shield_points():
    # The console's logo, in a 24 unit box: M12 3 l8 3 v6 c0 5 -3.5 8 -8 9 c-4.5 -1 -8 -4 -8 -9 V6 z
    pts = [(12, 3), (20, 6), (20, 12)]
    pts += bez((20, 12), (20, 17), (16.5, 20), (12, 21))
    pts += bez((12, 21), (7.5, 20), (4, 17), (4, 12))
    pts += [(4, 6), (12, 3)]
    return pts

def logo(size, color=BLUE, width=1.6):
    """The shield as a transparent RGBA image, drawn at 4x and reduced for smooth edges."""
    s = 4
    k = size * s / 24.0
    im = Image.new("RGBA", (size * s, size * s), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    pts = [(x * k, y * k) for x, y in shield_points()]
    d.line(pts, fill=color, width=int(width * k), joint="curve")
    for x, y in pts[:: max(1, len(pts) // 40)]:
        r = width * k / 2
        d.ellipse((x - r, y - r, x + r, y + r), fill=color)
    d.line([(9 * k, 12 * k), (15 * k, 12 * k)], fill=color, width=int(width * k))
    for x in (9, 15):
        r = width * k / 2
        d.ellipse((x * k - r, 12 * k - r, x * k + r, 12 * k + r), fill=color)
    return im.resize((size, size), Image.LANCZOS)

def centered(d, w, y, text, f, fill):
    box = d.textbbox((0, 0), text, font=f)
    d.text(((w - (box[2] - box[0])) / 2 - box[0], y), text, font=f, fill=fill)

def background(w, h, path, menu_top):
    """Dark background, shield and name in the upper part, leaving the lower part for the menu."""
    im = Image.new("RGB", (w, h), BG)
    d = ImageDraw.Draw(im)
    u = h / 720.0
    sz = int(150 * u)
    mark = logo(sz)
    top = int(h * 0.17)
    im.paste(mark, ((w - sz) // 2, top), mark)
    centered(d, w, top + sz + int(18 * u), "Gatehouse", font(int(46 * u), True), TEXT)
    centered(d, w, top + sz + int(80 * u), "Firewall, DNS and DHCP in one box", font(int(20 * u)), DIM)
    im.save(path, optimize=True)

def splash(outdir):
    os.makedirs(outdir, exist_ok=True)
    mark = logo(168)
    mark.save(os.path.join(outdir, "logo.png"), optimize=True)
    # Messages are drawn here, not by the boot splash, so it needs no font or text plugin.
    def text_img(name, lines):
        fs = [font(34, True), font(20)]
        cols = [TEXT, DIM]
        ws, hs = [], []
        probe = ImageDraw.Draw(Image.new("RGBA", (10, 10)))
        for t, f in zip(lines, fs):
            b = probe.textbbox((0, 0), t, font=f)
            ws.append(b[2] - b[0] + 4); hs.append(b[3] - b[1] + 14)
        im = Image.new("RGBA", (max(ws) + 8, sum(hs) + 10), (0, 0, 0, 0))
        d = ImageDraw.Draw(im)
        y = 0
        for t, f, c, w_, h_ in zip(lines, fs, cols, ws, hs):
            b = d.textbbox((0, 0), t, font=f)
            d.text(((im.width - (b[2] - b[0])) / 2 - b[0], y - b[1] + 2), t, font=f, fill=c)
            y += h_
        im.save(os.path.join(outdir, name), optimize=True)
    text_img("msg.png", ["Starting Gatehouse", "This can take a minute when starting from a USB stick."])
    # Progress track and the light that travels along it.
    track = Image.new("RGBA", (360, 6), (0, 0, 0, 0))
    ImageDraw.Draw(track).rounded_rectangle((0, 0, 359, 5), radius=3, fill=(44, 44, 42, 255))
    track.save(os.path.join(outdir, "track.png"))
    glow = Image.new("RGBA", (120, 6), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    for x in range(120):
        a = int(255 * math.sin(math.pi * x / 119) ** 1.5)
        gd.line([(x, 0), (x, 5)], fill=BLUE + (a,))
    glow.save(os.path.join(outdir, "glow.png"))

def selection(d):
    """The highlight for the selected menu entry: a rounded blue pill cut into nine slices."""
    W = H = 24; r = 8
    im = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    ImageDraw.Draw(im).rounded_rectangle((0, 0, W - 1, H - 1), radius=r, fill=BLUE + (255,))
    xs = [0, r, W - r, W]; names = [["nw", "n", "ne"], ["w", "c", "e"], ["sw", "s", "se"]]
    for j in range(3):
        for i in range(3):
            im.crop((xs[i], xs[j], xs[i + 1], xs[j + 1])).save(os.path.join(d, f"select_{names[j][i]}.png"))

if __name__ == "__main__":
    root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "iso", "config")
    # Kept under a name of our own: the build copies these into place after the stock boot
    # loader files, so nothing of the stock artwork can win.
    bi = os.path.join(root, "includes.binary", "gatehouse-boot")
    os.makedirs(os.path.join(bi, "theme"), exist_ok=True)
    background(640, 480, os.path.join(bi, "splash.png"), 0)
    background(1920, 1080, os.path.join(bi, "theme", "background.png"), 0)
    shutil.copy(os.path.join(os.path.dirname(os.path.abspath(__file__)), "grub-theme.txt"), os.path.join(bi, "theme", "theme.txt"))
    selection(os.path.join(bi, "theme"))
    splash(os.path.join(root, "includes.chroot", "usr", "share", "plymouth", "themes", "gatehouse"))
    print("ok")

    # The same menu theme is kept inside the system image so the installer can put it on the disk.
    src = os.path.join(bi, "theme")
    dst = os.path.join(root, "includes.chroot", "usr", "share", "gatehouse", "grub-theme")
    shutil.rmtree(dst, ignore_errors=True)
    shutil.copytree(src, dst)

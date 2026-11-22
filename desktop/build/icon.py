"""Draws the app icon: desktop/build/icon.png (1024x1024).

electron-builder makes the Windows .ico and macOS .icns from this PNG, and
the Linux AppImage uses it as is. web/static/icon.svg is the same design for
the browser tab; change both together.

The design: four bars on a dark rounded square, shrinking left to right (a
debt going down). The last, nearly gone, is green: paid off. Colors are the
app's tokens (--bg, --accent, --good).

Run from the repo root:  python3 desktop/build/icon.py   (needs Pillow)
"""

from pathlib import Path

from PIL import Image, ImageDraw

SIZE = 1024
SCALE = 4  # draw 4x larger, then shrink, for smooth edges

BG = (20, 26, 34, 255)          # a touch lighter than --bg #0f1318
BORDER = (42, 49, 59, 255)      # --border
ACCENT = (110, 160, 255)        # --accent #6ea0ff
GOOD = (76, 195, 138, 255)      # --good #4cc38a

# Rounded square inset like macOS app icons (824 of 1024).
TILE = (100, 100, 924, 924)
TILE_RADIUS = 185

# Bars: width 120, gap 56, sitting on y = 744.
BARS = [
    # x, height, color
    (188, 440, ACCENT + (255,)),
    (364, 320, ACCENT + (220,)),
    (540, 200, ACCENT + (185,)),
    (716, 80, GOOD),
]
BAR_W, BASE, BAR_RADIUS = 120, 744, 28


def s(*v):
    return tuple(int(x * SCALE) for x in v)


def main():
    img = Image.new("RGBA", s(SIZE, SIZE), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle(s(*TILE), radius=TILE_RADIUS * SCALE, fill=BG, outline=BORDER, width=6 * SCALE)

    for x, h, color in BARS:
        layer = Image.new("RGBA", img.size, (0, 0, 0, 0))
        ImageDraw.Draw(layer).rounded_rectangle(s(x, BASE - h, x + BAR_W, BASE), radius=BAR_RADIUS * SCALE, fill=color)
        img = Image.alpha_composite(img, layer)

    out = Path(__file__).with_name("icon.png")
    img.resize((SIZE, SIZE), Image.LANCZOS).save(out)
    print(f"wrote {out}")


if __name__ == "__main__":
    main()

"""Draws the F1 Party logo: a white, heavy "F1P" whose letters overlap into one shape, separated only by a
thin dark line, on a red rounded square (the style of MultiViewer and Ultimate F1 Viewer's icons).
The letters are drawn from angular geometric shapes, no font needed.
Usage: python make_logo.py <out_dir>   (needs Pillow)"""
import os
import sys
from PIL import Image, ImageDraw, ImageChops, ImageFilter

S = 4096  # drawn large, scaled down for smooth edges

# Letter geometry in design units (letter height 400).
H = 400      # cap height
T = 118      # stroke thickness
R = 0       # corner radius of the strokes
LINE = 9     # thin dark line between overlapping letters
DARK = (28, 18, 20, 255)


def rect(d, box, e, r=R):
    """A rounded rectangle grown by e on every side (e < 0 shrinks it)."""
    x0, y0, x1, y1 = box
    d.rounded_rectangle([x0 - e, y0 - e, x1 + e, y1 + e], radius=0, fill=255)


def letter_F(d, x, y, k, e):
    w = 282
    rect(d, sc((x, y, x + T, y + H), k), e * k, R * k)                      # stem
    rect(d, sc((x, y, x + w, y + T), k), e * k, R * k)                      # top bar
    rect(d, sc((x, y + 188, x + 218, y + 188 + T * 0.86), k), e * k, R * k)  # middle bar
    return w


def letter_1(d, x, y, k, e):
    rect(d, sc((x, y, x + T, y + H), k), e * k, R * k)                      # stem
    # Flag: one solid parallelogram from the top of the stem down to the left, with a vertical cut at its
    # end. Nothing rises above the cap line; its bottom stays clear of the F's middle bar.
    reach, drop, thick = 84, 62, 100
    pts = [(x + T * 0.45, y), (x - reach, y + drop), (x - reach, y + drop + thick), (x + T * 0.45, y + thick)]
    poly = [(px * k, py * k) for px, py in pts]
    d.polygon(poly, fill=255)
    if e > 0:  # the dark outline pass: grow the shape by e on every side
        d.line(poly + [poly[0]], fill=255, width=int(2 * e * k), joint="curve")
    return T


def letter_P(d, x, y, k, e, hole):
    w, bh, bt = 320, 236, 78  # width, bowl height, bowl thickness
    rect(d, sc((x, y, x + T, y + H), k), e * k, R * k)                      # stem
    bowl = Image.new("L", d.im.size, 0)
    bd = ImageDraw.Draw(bowl)
    rect(bd, sc((x, y, x + w, y + bh), k), e * k, 0)                  # outer bowl
    inner = Image.new("L", d.im.size, 0)
    rect(ImageDraw.Draw(inner), sc((x + T, y + bt, x + w - 94, y + bh - bt), k), -e * k, 0)
    hole.append((bowl, inner))
    return w


def sc(box, k):
    return tuple(v * k for v in box)


def letters_layer(k, ox, oy):
    """White letters with the thin dark line where they overlap, as an RGBA layer."""
    layer = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    # placement (design units): F, then P, then the 1 on top, overlapping both
    xF = 0
    x1 = xF + 258
    xP = x1 + T - 30
    pieces = [
        lambda d, e, hole: letter_F(d, ox + xF, oy, k, e),
        lambda d, e, hole: letter_P(d, ox + xP, oy, k, e, hole),
        lambda d, e, hole: letter_1(d, ox + x1, oy, k, e),
    ]
    for draw_piece in pieces:
        for e, colour in ((LINE, DARK), (0, (255, 255, 255, 255))):
            mask = Image.new("L", (S, S), 0)
            hole = []
            draw_piece(ImageDraw.Draw(mask), e, hole)
            for bowl, inner in hole:
                mask = ImageChops.lighter(mask, ImageChops.subtract(bowl, inner))
            if colour == DARK and hole:
                # the bowl's counter shows the background, not the dark line colour
                pass
            fill = Image.new("RGBA", (S, S), colour)
            layer.paste(fill, (0, 0), mask)
    return layer


def width_units():
    return 258 + T - 30 + 320  # F start to P end


def logo(round_corners=True, letter_width=0.62, margin=0.0):
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    m = int(S * margin)
    mask = Image.new("L", (S, S), 0)
    if round_corners:
        ImageDraw.Draw(mask).rounded_rectangle([m, m, S - 1 - m, S - 1 - m], radius=int((S - 2 * m) * 0.22), fill=255)
    else:
        mask.paste(255, (0, 0, S, S))
    grad = Image.new("RGBA", (S, S))
    gd = ImageDraw.Draw(grad)
    top, bottom = (238, 18, 12), (190, 0, 0)
    for y in range(S):
        t = y / (S - 1)
        gd.line([(0, y), (S, y)], fill=tuple(int(top[i] + (bottom[i] - top[i]) * t) for i in range(3)) + (255,))
    img.paste(grad, (0, 0), mask)

    k = S * letter_width / width_units()
    ox = (S / k - width_units()) / 2
    oy = (S / k - H) / 2
    letters = letters_layer(k, ox, oy)
    # soft shadow under the letters for a little depth
    shadow = Image.new("RGBA", (S, S), (80, 0, 0, 0))
    shadow.putalpha(letters.getchannel("A").point(lambda a: int(a * 0.32)))
    shadow = shadow.resize((S // 8, S // 8)).filter(ImageFilter.GaussianBlur(3)).resize((S, S))
    img.alpha_composite(shadow, (int(S * 0.006), int(S * 0.012)))
    img.alpha_composite(letters)
    # keep everything inside the rounded square
    img.putalpha(ImageChops.multiply(img.getchannel("A"), mask))
    return img


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else "."
    os.makedirs(out, exist_ok=True)
    big = logo(margin=0.04)  # app icon, with a little room around it like MultiViewer's
    for px in (1024, 512, 256):
        big.resize((px, px), Image.LANCZOS).save(os.path.join(out, f"f1party-logo-{px}.png"))
    # Discord avatar: full square (Discord shows it as a circle), letters a bit smaller to fit the circle
    logo(round_corners=False, letter_width=0.58).resize((512, 512), Image.LANCZOS).save(
        os.path.join(out, "f1party-discord-avatar.png"))



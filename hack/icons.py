"""Writes the favicon set of docs/adr/0052 D8 from the mark: frontend/public/favicon.svg, a
favicon.ico with 16, 32 and 48 pixels, and the 180-pixel apple-touch-icon.png.

Usage: python3 hack/icons.py [board|twin|spark-c]   (default board, the owner's pick)

The glyphs are the ones of frontend/src/app/brand/logo.ts; change both together. Rasterising
needs rsvg-convert (librsvg: `brew install librsvg`). The ICO embeds PNGs, which every current
browser reads."""

import os
import struct
import subprocess
import sys
import tempfile

PUBLIC = os.path.join(os.path.dirname(__file__), '..', 'frontend', 'public')
GRADIENT = [('0', '#70e6ce'), ('0.22', '#6e51eb'), ('0.40', '#a63cd8'), ('0.56', '#d233b8'),
            ('0.72', '#db6a68'), ('0.86', '#e7a666'), ('1', '#f2d672')]


def sparkle(cx, cy, r):
    """The four-pointed sparkle of logo.ts, centred on (cx, cy)."""
    k = r * 0.55
    return (f'M{cx} {cy - r}C{cx} {cy - r + k} {cx + r - k} {cy} {cx + r} {cy}'
            f'C{cx + r - k} {cy} {cx} {cy + r - k} {cx} {cy + r}'
            f'C{cx} {cy + r - k} {cx - r + k} {cy} {cx - r} {cy}'
            f'C{cx - r + k} {cy} {cx} {cy - r + k} {cx} {cy - r}Z')


def inner(v):
    """A glyph coordinate of the 64-unit box, scaled into the mark's ink."""
    return round(32 + (v - 32) * 0.70, 2)


def glyph(variant):
    s = lambda cx, cy, r: f'<path fill="#fff" d="{sparkle(inner(cx), inner(cy), round(r * 0.70, 2))}"/>'
    if variant == 'spark-c':
        a, b = (inner(44.5), inner(21.5)), (inner(44.5), inner(42.5))
        return (f'<path d="M{a[0]} {a[1]} A{15 * 0.7} {15 * 0.7} 0 1 0 {b[0]} {b[1]}" fill="none" stroke="#fff" '
                f'stroke-width="{7 * 0.7}" stroke-linecap="round"/>' + s(47, 32, 9))
    if variant == 'board':
        bars = [(13, 34, 1), (27.5, 24, 0.85), (42, 13, 0.7)]
        rects = ''.join(f'<rect x="{inner(x)}" y="{inner(14)}" width="{9 * 0.7}" height="{h * 0.7}" rx="{3.5 * 0.7}" '
                        f'fill="#fff" opacity="{o}"/>' for x, h, o in bars)
        return rects + s(45, 43, 9)
    return s(37, 37, 17) + s(19, 19, 8)


def svg(variant):
    stops = ''.join(f'<stop offset="{o}" stop-color="{c}"/>' for o, c in GRADIENT)
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <defs>
    <linearGradient id="border" x1="0" y1="0" x2="1" y2="1">{stops}</linearGradient>
    <radialGradient id="ink" cx="0.5" cy="0.3" r="0.72"><stop offset="0" stop-color="#2b1673"/><stop offset="1" stop-color="#160941"/></radialGradient>
  </defs>
  <rect x="2" y="2" width="60" height="60" rx="18" fill="url(#border)"/>
  <rect x="5.5" y="5.5" width="53" height="53" rx="14.5" fill="url(#ink)"/>
  {glyph(variant)}
</svg>
'''


def main():
    variant = sys.argv[1] if len(sys.argv) > 1 else 'board'
    if variant not in ('twin', 'spark-c', 'board'):
        sys.exit(f'unknown variant {variant}: twin, spark-c or board')
    source = os.path.join(PUBLIC, 'favicon.svg')
    with open(source, 'w') as f:
        f.write(svg(variant))
    with tempfile.TemporaryDirectory() as tmp:
        png = {}
        for size in (16, 32, 48, 180):
            out = os.path.join(tmp, f'{size}.png')
            subprocess.run(['rsvg-convert', '-w', str(size), '-h', str(size), source, '-o', out], check=True)
            with open(out, 'rb') as f:
                png[size] = f.read()
    with open(os.path.join(PUBLIC, 'apple-touch-icon.png'), 'wb') as f:
        f.write(png[180])
    sizes = (16, 32, 48)
    offset, entries, data = 6 + 16 * len(sizes), b'', b''
    for size in sizes:
        entries += struct.pack('<BBBBHHII', size, size, 0, 0, 1, 32, len(png[size]), offset + len(data))
        data += png[size]
    with open(os.path.join(PUBLIC, 'favicon.ico'), 'wb') as f:
        f.write(struct.pack('<HHH', 0, 1, len(sizes)) + entries + data)
    print(f'favicon set written for the {variant} mark')


if __name__ == '__main__':
    main()

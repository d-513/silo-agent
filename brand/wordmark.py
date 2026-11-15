#!/usr/bin/env python3
"""Outlines the name in Geist 600 and writes logo/lockup.svg and logo/stacked.svg.

Only needed when the name or the mark changes (run `make brand` first, and
again after, so web/public gets the new files). Needs fonttools and brotli:
  python3 -m venv /tmp/brandenv && /tmp/brandenv/bin/pip install fonttools brotli
  /tmp/brandenv/bin/python brand/wordmark.py

The typeface is the app's own and is not signed off as the final wordmark:
the designer's mockups used Unbounded SemiBold as a placeholder.
"""
import re
from pathlib import Path

from fontTools.pens.boundsPen import BoundsPen
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

BRAND = Path(__file__).resolve().parent
FONT = BRAND.parent / "web/node_modules/@fontsource-variable/geist/files/geist-latin-wght-normal.woff2"
INK = "#1A1917"
NAME = "silo"

font = instantiateVariableFont(TTFont(FONT), {"wght": 600})
glyphs, cmap, upem = font.getGlyphSet(), font.getBestCmap(), font["head"].unitsPerEm


def outline(text, x, middle, size, tracking, anchor="start"):
    """The word as one path, its ink centred on `middle`, and its width."""
    scale = size / upem
    advances = [glyphs[cmap[ord(c)]].width * scale + tracking * size for c in text]
    width = sum(advances) - tracking * size
    bounds = BoundsPen(glyphs)
    for c in text:
        glyphs[cmap[ord(c)]].draw(bounds)
    low, high = bounds.bounds[1] * scale, bounds.bounds[3] * scale
    baseline = middle + (low + high) / 2
    if anchor == "middle":
        x -= width / 2
    d = []
    for char, advance in zip(text, advances):
        pen = SVGPathPen(glyphs, lambda v: f"{v:.2f}".rstrip("0").rstrip("."))
        glyphs[cmap[ord(char)]].draw(TransformPen(pen, (scale, 0, 0, -scale, x, baseline)))
        d.append(pen.getCommands())
        x += advance
    return " ".join(p for p in d if p), width


mark = re.search(r"<svg[^>]*>(.*)</svg>", (BRAND / "logo/mark.svg").read_text(), re.S).group(1).strip()

# The tile on the left, the name on the right: the gap a fifth of the tile,
# the type four fifths of it.
TILE = 40
word, width = outline(NAME, TILE * 1.2, TILE / 2, TILE * 0.8, -0.02)
w = round(TILE * 1.2 + width + 1)
(BRAND / "logo/lockup.svg").write_text(
    f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {TILE}" width="{w}" height="{TILE}" role="img" aria-label="Silo">\n'
    f'  <g transform="scale({TILE / 100:g})">\n    {mark}\n  </g>\n'
    f'  <path fill="{INK}" d="{word}"/>\n</svg>\n'
)

word, _ = outline(NAME, 40, 68, 22, -0.02, "middle")
(BRAND / "logo/stacked.svg").write_text(
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 80 80" width="80" height="80" role="img" aria-label="Silo">\n'
    f'  <g transform="translate(16 2) scale(.48)">\n    {mark}\n  </g>\n'
    f'  <path fill="{INK}" d="{word}"/>\n</svg>\n'
)

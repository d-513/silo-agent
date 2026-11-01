#!/usr/bin/env python3
"""Outlines the name in Geist 500 and writes logo/lockup.svg and logo/stacked.svg.

Only needed when the name or the mark changes. Needs fonttools and brotli:
  python3 -m venv /tmp/brandenv && /tmp/brandenv/bin/pip install fonttools brotli
  /tmp/brandenv/bin/python brand/wordmark.py
"""
import re
from pathlib import Path

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

BRAND = Path(__file__).resolve().parent
FONT = BRAND.parent / "web/node_modules/@fontsource-variable/geist/files/geist-latin-wght-normal.woff2"
INK = "#1A1917"

font = instantiateVariableFont(TTFont(FONT), {"wght": 500})
glyphs, cmap, upem = font.getGlyphSet(), font.getBestCmap(), font["head"].unitsPerEm


def outline(text, x, baseline, size, tracking, anchor="start"):
    scale = size / upem
    advances = [glyphs[cmap[ord(c)]].width * scale + tracking * size for c in text]
    width = sum(advances) - tracking * size
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

word, width = outline("Silo Agent", 40, 23, 19, -0.02)
w = round(40 + width + 2)
(BRAND / "logo/lockup.svg").write_text(
    f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} 32" width="{w}" height="32" role="img" aria-label="Silo Agent">\n'
    f'  <g transform="scale(.5)">\n    {mark}\n  </g>\n'
    f'  <path fill="{INK}" d="{word}"/>\n</svg>\n'
)

word, _ = outline("SILO", 40, 74, 13, 0.04, "middle")
(BRAND / "logo/stacked.svg").write_text(
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 80 80" width="80" height="80" role="img" aria-label="Silo">\n'
    f'  <g transform="translate(14 0) scale(.81)">\n    {mark}\n  </g>\n'
    f'  <path fill="{INK}" d="{word}"/>\n</svg>\n'
)

#!/usr/bin/env python3
"""Render a document and make proof images to look at before presenting it.

    python3 render.py IN [OUT.pdf] [--page N] [--dpi 110] [--slides]

IN is .html (rendered with WeasyPrint), .pdf (proofed as is) or .docx
(approximated through pandoc + the shared design.css; Word itself will differ).
Writes bot/proof/<name>-sheet.png (every page as a thumbnail, 8 to a sheet) and,
with --page, bot/proof/<name>-pN.png at a readable size. Prints warnings about
fallback fonts, near-empty last pages and WeasyPrint problems. Then: present the
sheet, fix what looks wrong, run again; present the final file only when clean.
"""
import argparse
import logging
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent.parent
PROOF = Path("/workspace/bot/proof") if Path("/workspace").is_dir() else Path("bot/proof")
FALLBACK = re.compile(r"Liberation|DejaVu|FreeSans|FreeSerif|Nimbus|Arial|Helvetica|Times|Courier", re.I)
WANTED = ("Inter", "EB Garamond", "Libertine", "Biolinum", "Noto")


class Collect(logging.Handler):
    def __init__(self):
        super().__init__(logging.WARNING)
        self.lines = []

    def emit(self, record):
        if len(self.lines) < 12:
            self.lines.append("weasyprint: " + record.getMessage())


def html_to_pdf(src: Path, out: Path):
    from weasyprint import HTML

    h = Collect()
    logging.getLogger("weasyprint").addHandler(h)
    HTML(filename=str(src), base_url=str(src.parent)).write_pdf(str(out))
    return h.lines


def docx_to_html(src: Path) -> Path:
    css = HERE / "design.css"
    tmp = Path(tempfile.mkdtemp(prefix="docx-proof-")) / (src.stem + ".html")
    subprocess.run(["pandoc", str(src), "-s", "-o", str(tmp), "--extract-media", str(tmp.parent),
                    "--metadata", "pagetitle=" + src.stem, "-c", str(css)], check=True)
    return tmp


def sheet(pages, out: Path):
    from PIL import Image, ImageDraw

    thumbs = [Image.open(p).convert("RGB") for p in pages]
    w = max(t.width for t in thumbs)
    h = max(t.height for t in thumbs)
    cols = min(4, len(thumbs))
    rows = (len(thumbs) + cols - 1) // cols
    pad = 16
    img = Image.new("RGB", (cols * (w + pad) + pad, rows * (h + pad + 14) + pad), (226, 226, 222))
    d = ImageDraw.Draw(img)
    for i, t in enumerate(thumbs):
        x = pad + (i % cols) * (w + pad)
        y = pad + (i // cols) * (h + pad + 14)
        img.paste(t, (x, y + 14))
        d.text((x, y), str(int(re.findall(r"(\d+)\.png$", str(pages[i]))[0])), fill=(60, 60, 60))
    img.save(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("src")
    ap.add_argument("out", nargs="?")
    ap.add_argument("--page", type=int, help="also render this page large")
    ap.add_argument("--dpi", type=int, default=110)
    ap.add_argument("--slides", action="store_true", help="a deck: skip the page-fill check")
    a = ap.parse_args()

    src = Path(a.src)
    if not src.exists():
        sys.exit(f"no such file: {src}")
    PROOF.mkdir(parents=True, exist_ok=True)
    warnings = []
    ext = src.suffix.lower()
    if ext == ".pdf":
        pdf = src
    else:
        if ext == ".docx":
            warnings.append("docx proof is an approximation (pandoc + design.css); check structure, not exact Word layout")
            src = docx_to_html(src)
        elif ext not in (".html", ".htm"):
            sys.exit("render.py takes .html, .pdf or .docx")
        pdf = Path(a.out) if a.out else PROOF / (Path(a.src).stem + ".pdf")
        pdf.parent.mkdir(parents=True, exist_ok=True)
        warnings += html_to_pdf(src, pdf)

    name = Path(a.src).stem
    for old in PROOF.glob(f"{name}-t-*.png"):
        old.unlink()
    subprocess.run(["pdftoppm", "-png", "-r", "40", str(pdf), str(PROOF / f"{name}-t")], check=True)
    pages = sorted(PROOF.glob(f"{name}-t-*.png"), key=lambda p: int(re.findall(r"(\d+)\.png$", p.name)[0]))
    sheets = []
    for i in range(0, len(pages), 8):
        s = PROOF / (f"{name}-sheet.png" if i == 0 else f"{name}-sheet{i // 8 + 1}.png")
        sheet(pages[i:i + 8], s)
        sheets.append(s)
    for p in pages:
        p.unlink()

    fonts = subprocess.run(["pdffonts", str(pdf)], capture_output=True, text=True).stdout.splitlines()[2:]
    names = sorted({f.split()[0].split("+")[-1] for f in fonts if f.strip()})
    bad = [n for n in names if FALLBACK.search(n)]
    if bad:
        warnings.append("fallback fonts used: " + ", ".join(bad) + " (a font-family name is wrong or not installed)")
    if names and not any(any(w in n for w in WANTED) for n in names):
        warnings.append("no design typeface in the PDF; see DESIGN.md (Inter, EB Garamond, Linux Libertine O)")

    n = len(pages)
    if n > 1 and not a.slides:
        text = subprocess.run(["pdftotext", str(pdf), "-"], capture_output=True, text=True).stdout.split("\f")
        sizes = sorted(len(t.strip()) for t in text[:n])
        last = len(text[n - 1].strip())
        if last < 0.4 * sizes[-1]:
            warnings.append(f"page {n} holds little ({last} chars vs ~{sizes[-1]} on the fullest page): "
                            f"tighten so it ends on page {n - 1}, or add content")

    big = None
    if a.page:
        big = PROOF / f"{name}-p{a.page}"
        subprocess.run(["pdftoppm", "-png", "-r", str(a.dpi), "-f", str(a.page), "-l", str(a.page),
                        "-singlefile", str(pdf), str(big)], check=True)
        big = big.with_suffix(".png")

    rel = lambda p: os.path.relpath(p, "/workspace") if str(p).startswith("/workspace/") else str(p)
    print(f"pdf: {rel(pdf)} ({n} page{'s' if n != 1 else ''})")
    print("fonts: " + (", ".join(names) or "none"))
    for s in sheets:
        print(f"proof: present {rel(s)}")
    if big:
        print(f"detail: present {rel(big)}")
    for w in dict.fromkeys(warnings):
        print("WARNING: " + w)
    if not warnings:
        print("no warnings; still look at the proof before presenting")


if __name__ == "__main__":
    main()

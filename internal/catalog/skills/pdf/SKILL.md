---
name: pdf
description: Works with PDF files in the workspace — read and extract with poppler (pdftotext, pdftoppm, pdfinfo, pdfimages) and pypdf; create designed PDFs from ready HTML templates (report, newsletter, one-pager, itinerary) rendered with weasyprint and proofed as page images; assemble or edit with pypdf, img2pdf, reportlab. Use when the user uploads or points at a .pdf, or wants a report, newsletter, flyer, itinerary, invoice, letter, or any document produced as a PDF.
metadata:
  silo_seed: pdf
---

# PDFs

Chat uploads land in `tmp/`, which is wiped on every machine start. Paths are relative to `/workspace` (`tmp/report.pdf`, not `/workspace/tmp/report.pdf`). Anything you make for the human goes under `/workspace`; scratch goes under `bot/`.

## Read: text

`pdftotext` is fastest. `-layout` keeps tables and columns readable.

```python
import subprocess
out = subprocess.run(["pdftotext", "-layout", "tmp/report.pdf", "-"], capture_output=True, text=True)
print(out.stdout[:8000])
```

Metadata and page count: `pdfinfo tmp/report.pdf`.

## Read: page by page / embedded text (pypdf)

```python
from pypdf import PdfReader
r = PdfReader("tmp/report.pdf")
print(len(r.pages))
print(r.pages[0].extract_text()[:4000])
```

## Read: see a page (you get the pixels)

The model only receives pixels for `.png`, `.jpg`/`.jpeg`, `.webp`, `.gif`. Rasterize the page, then `present` it.

```python
import subprocess
subprocess.run(["pdftoppm", "-png", "-r", "110", "-f", "1", "-l", "1", "tmp/report.pdf", "bot/page"], check=True)
# -> bot/page-1.png
```

`present bot/page-1.png` is scratch (you get pixels; the human sees a collapsed row). Render under `/workspace` when the human should see a folio.

## Read: scanned PDFs (no text layer)

If `pdftotext` returns almost nothing, the pages are images. Rasterize and OCR:

```python
subprocess.run(["pdftoppm", "-png", "-r", "200", "-f", "1", "-l", "1", "tmp/scan.pdf", "bot/scan"], check=True)
print(subprocess.run(["tesseract", "bot/scan-1.png", "-"], capture_output=True, text=True).stdout)
```

Images: `pdfimages -png tmp/report.pdf bot/img`, then `present` one. Simple tables: `pdftotext -layout`; for complex layouts, rasterize and read the pixels.

## Create: a designed PDF (default for anything a person will read)

Start from a template, never a blank page. Read `DESIGN.md` in this skill once per task (themes, type scale, what to avoid).

| Template (`/opt/silo/skills/pdf/templates/`) | Use for | Theme |
|---|---|---|
| `report.html` | reports, reviews, proposals over 2 pages: cover, key numbers, sections, tables | `ink` |
| `newsletter.html` | family/club/team newsletters: masthead, lead story with photo, sidebar, two sheets | `garden` |
| `onepager.html` | brief, proposal, product sheet, flyer: must fit one page | `ink` |
| `itinerary.html` | trips, event programmes, agendas: essentials box, one block per day | `ember` |

1. Copy the template to `bot/<name>.html`; replace the sample text, keep the structure and class names. Switch theme with the class on `<html>` (`theme-ink`, `theme-garden`, `theme-ember`). Images go next to the HTML or by absolute path.
2. Charts: `sys.path.insert(0, "/opt/silo/skills/pdf/scripts"); import theme; t = theme.mpl("ink")`, plot, `savefig("bot/chart.png")`, then `<figure><img src="chart.png"><figcaption>…</figcaption></figure>`.
3. Render and proof (step below is not optional).

```python
import subprocess
print(subprocess.run(["python3", "/opt/silo/skills/pdf/scripts/render.py", "bot/report.html", "/workspace/report.pdf"],
                     capture_output=True, text=True).stdout)
```

## Check: look at it before you present it

`render.py` writes `bot/proof/<name>-sheet.png` (every page as a thumbnail) and prints warnings (fallback fonts, a near-empty last page, CSS it ignored). Always:

1. `present bot/proof/<name>-sheet.png` (you get the pixels). For detail, rerun with `--page N` and present `bot/proof/<name>-pN.png`.
2. Fix what you see: overflowing or clipped text, a heading stranded at a page foot, an almost-empty last page, cramped or empty areas, wrong fonts, unreadable chart labels. Rerun.
3. Only when the proof is clean, `present` the final PDF. `render.py some.pdf` proofs any PDF, including reportlab output.

## Create: programmatic PDFs (reportlab)

`reportlab` suits bulk or very regular output (hundreds of invoices, labels). It cannot use the OTF design fonts and defaults to Helvetica, so prefer the HTML path for anything that should look designed; still proof it with `render.py`.

```python
from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import getSampleStyleSheet
from reportlab.lib.units import cm
from reportlab.platypus import Paragraph, SimpleDocTemplate, Spacer, Table, TableStyle

doc = SimpleDocTemplate("/workspace/report.pdf", pagesize=A4)
s = getSampleStyleSheet()
tbl = Table([["Item", "Qty"], ["Widget", "3"]])
tbl.setStyle(TableStyle([("GRID", (0, 0), (-1, -1), 0.5, colors.grey), ("FONTNAME", (0, 0), (-1, 0), "Helvetica-Bold")]))
doc.build([
    Paragraph("Weekly report", s["Title"]),
    Spacer(1, 12),
    Paragraph("Summary of the week.", s["BodyText"]),
    Spacer(1, 12),
    tbl,
])
```

`Image("bot/chart.png", width=12*cm, height=6*cm)` embeds a picture. Keep long text in `Paragraph` (it wraps); `Table` handles grids.

## Create: HTML/CSS to PDF (weasyprint)

When the layout is easier to express as HTML, write HTML and convert. This pairs with `pandoc` for markdown input.

```python
import subprocess
subprocess.run(["pandoc", "tmp/notes.md", "-o", "bot/notes.html", "--standalone"], check=True)
subprocess.run(["weasyprint", "bot/notes.html", "/workspace/notes.pdf"], check=True)
```

Add `-c /opt/silo/skills/pdf/design.css` to the pandoc call so it does not look like a bare web page. Or from Python: `from weasyprint import HTML; HTML(filename="bot/x.html").write_pdf("/workspace/out.pdf")`. WeasyPrint here is v57: no CSS grid or `gap`, and `var()` only in longhand properties.

## Create: chart PDFs (matplotlib)

```python
import sys; sys.path.insert(0, "/opt/silo/skills/pdf/scripts")
import theme; theme.mpl("ink")
import matplotlib.pyplot as plt
plt.plot([1, 2, 3], [2, 4, 9])
plt.savefig("/workspace/chart.pdf", bbox_inches="tight")
```

## Create: images to PDF (img2pdf)

Lossless and fast for scans or photos:

```python
import subprocess
subprocess.run(["img2pdf", "bot/scan-1.png", "bot/scan-2.png", "-o", "/workspace/scan.pdf"], check=True)
```

A single image can also be saved directly with Pillow:

```python
from PIL import Image
Image.open("tmp/photo.jpg").convert("RGB").save("/workspace/photo.pdf")
```

## Edit: merge, split, rotate, encrypt (pypdf)

```python
from pypdf import PdfReader, PdfWriter
w = PdfWriter()
for f in ["tmp/a.pdf", "tmp/b.pdf"]:
    for p in PdfReader(f).pages:
        w.add_page(p)
w.write("/workspace/merged.pdf")
```

Split by writing one page per `PdfWriter`; rotate with `page.rotate(90)`; encrypt with `w.encrypt("password")`.

## Output

Write user-facing PDFs to `/workspace` and `present` the relative path (a folio) only after the proof check above. Do not paste a whole PDF's text into chat; summarize and point at the file.

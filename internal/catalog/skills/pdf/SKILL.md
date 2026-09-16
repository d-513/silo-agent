---
name: pdf
description: Works with PDF files in the workspace — read and extract with poppler (pdftotext, pdftoppm, pdfinfo, pdfimages) and pypdf, and create, assemble, or edit with reportlab, img2pdf, weasyprint, and pypdf. Use when the user uploads or points at a .pdf, or wants a report, invoice, letter, chart, or document produced as a PDF.
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

## Create: text, tables, reports (reportlab)

`reportlab` builds multi-page PDFs from paragraphs, tables, and images without a browser or LaTeX. This is the default for invoices, reports, and letters.

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

Or from Python directly: `from weasyprint import HTML; HTML(string="<h1>Hi</h1>").write_pdf("/workspace/out.pdf")`. Use `@page { size: A4; margin: 2cm; }` for margins.

## Create: chart PDFs (matplotlib)

```python
import matplotlib
matplotlib.use("Agg")
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

Write user-facing PDFs to `/workspace` and `present` the relative path (a folio). Do not paste a whole PDF's text into chat; summarize and point at the file.

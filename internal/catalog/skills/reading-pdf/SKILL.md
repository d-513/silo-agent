---
name: reading-pdf
description: Reads PDF files in the workspace with poppler (pdftotext, pdftoppm, pdfinfo) and pypdf. Use when the user uploads or points at a .pdf and you need its text, metadata, page count, or a page rendered to pixels.
metadata:
  silo_seed: reading-pdf
---

# Reading PDFs

Chat uploads land in `tmp/`, which is wiped on every machine start. Paths are relative to `/workspace` (`tmp/report.pdf`, not `/workspace/tmp/report.pdf`).

## Text

`pdftotext` is fastest. `-layout` keeps tables and columns readable.

```python
import subprocess
out = subprocess.run(["pdftotext", "-layout", "tmp/report.pdf", "-"], capture_output=True, text=True)
print(out.stdout[:8000])
```

Metadata and page count: `pdfinfo tmp/report.pdf`.

## Page by page / embedded text (pypdf)

```python
from pypdf import PdfReader
r = PdfReader("tmp/report.pdf")
print(len(r.pages))
print(r.pages[0].extract_text()[:4000])
```

## See a page (you get the pixels)

The model only receives pixels for `.png`, `.jpg`/`.jpeg`, `.webp`, `.gif`. Rasterize the page, then `present` it.

```python
import subprocess
subprocess.run(["pdftoppm", "-png", "-r", "110", "-f", "1", "-l", "1", "tmp/report.pdf", "bot/page"], check=True)
# -> bot/page-1.png
```

`present bot/page-1.png` is scratch (you get pixels; the human sees a collapsed row). Render under `/workspace` when the human should see a folio.

## Scanned PDFs (no text layer)

If `pdftotext` returns almost nothing, the pages are images. Rasterize and OCR:

```python
subprocess.run(["pdftoppm", "-png", "-r", "200", "-f", "1", "-l", "1", "tmp/scan.pdf", "bot/scan"], check=True)
print(subprocess.run(["tesseract", "bot/scan-1.png", "-"], capture_output=True, text=True).stdout)
```

## Images and tables

- Images: `pdfimages -png tmp/report.pdf bot/img`, then `present` one.
- Simple tables: `pdftotext -layout` is usually enough. For complex layouts, rasterize the page and read the pixels.

## Output

Write derived, user-facing text to `/workspace` (for example `Path("/workspace/report.md").write_text(...)`) and `present` the relative path. Do not paste a whole PDF into chat.

---
name: word
description: Works with word-processor documents (.docx, .odt, .rtf, legacy .doc) in the workspace — read with pandoc, python-docx, and antiword/catdoc, and create or edit with python-docx and pandoc, styled with shared themes and proofed as page images. Use when the user uploads a Word/Writer document, or wants a letter, report, contract, or other formatted document produced.
metadata:
  silo_seed: word
---

# Word documents

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`. User-facing documents go under `/workspace`; scratch under `bot/`.

## Read: text / Markdown (docx, odt, rtf)

pandoc keeps headings, lists, and tables:

```python
import subprocess
out = subprocess.run(["pandoc", "tmp/doc.docx", "-t", "gfm"], capture_output=True, text=True)
print(out.stdout[:8000])
```

The same command handles `.odt` and `.rtf`.

## Read: structured access (python-docx)

Use this when you need styles, tables, or specifics beyond plain text:

```python
import docx
d = docx.Document("tmp/doc.docx")
for p in d.paragraphs:
    if p.text.strip():
        print(p.style.name, "|", p.text)
for t in d.tables:
    for row in t.rows:
        print(" | ".join(c.text for c in row.cells))
```

Embedded images live in `word/media/`; unzip the file (it is a zip) to reach them.

## Read: legacy `.doc` (binary)

pandoc and python-docx do not read binary `.doc`. Use `antiword`, then `catdoc`:

```python
print(subprocess.run(["antiword", "tmp/old.doc"], capture_output=True, text=True).stdout)
```

## Create: python-docx

Read `DESIGN.md` in this skill once per task. Always apply a theme first (`ink`, `garden`, `ember`): it sets margins, Georgia/Calibri (fonts every Word has), the type scale, spacing and colours, so the file does not look like Word's defaults.

```python
import sys; sys.path.insert(0, "/opt/silo/skills/word/scripts")
import docx, theme
from docx.shared import Inches

d = docx.Document()
theme.docx(d, "ink")
d.add_paragraph("Quarterly report", style="Title")
d.add_paragraph("Northwind Logistics · Q3 2026", style="Subtitle")
d.add_heading("Delivery", 1)
d.add_paragraph("Summary paragraph.")
t = d.add_table(rows=2, cols=2)
for c, v in enumerate(["Metric", "Value"]): t.cell(0, c).text = v
for c, v in enumerate(["On time", "96.2%"]): t.cell(1, c).text = v
theme.docx_table(t, "ink", numeric_cols=(1,))      # hairlines, right-aligned numbers
d.add_picture("bot/chart.png", width=Inches(6))    # chart made after theme.mpl("ink")
d.save("/workspace/report.docx")
```

Style runs with `run = p.add_run("bold"); run.bold = True; run.font.size = Pt(12)`. Use `docx.Document("tmp/template.docx")` to fill an existing document instead of starting blank (keep its styles; do not apply a theme over a house style).

## Create: Markdown to docx (pandoc)

Fastest path when the content is already markdown. Headings, lists, tables, and images carry over.

```python
import subprocess
subprocess.run(["pandoc", "tmp/notes.md", "-o", "/workspace/notes.docx"], check=True)
```

Pandoc also writes `.odt` and `.rtf` (`-o out.odt`). Match an existing house style with `--reference-doc=tmp/template.docx`. There is no writer for legacy binary `.doc` — produce `.docx` instead.

## Check: look at it before you present it

There is no Word on this machine, so the proof is an approximation (pandoc + `design.css`): it shows structure, length, headings, tables and images, not Word's exact layout.

```python
print(subprocess.run(["python3", "/opt/silo/skills/word/scripts/render.py", "/workspace/report.docx"], capture_output=True, text=True).stdout)
```

`present bot/proof/report-sheet.png`, fix what is wrong (walls of bullets, missing headings, broken tables, stretched images), rerun, then present the file. If the human wants a PDF, build it from the `pdf` skill's templates instead of converting the docx.

## Output

Write the document to `/workspace` and `present` the relative path (a folio). Do not paste the whole document into chat; summarize and point at the file. To also hand back a PDF, convert with `pandoc` + `weasyprint` (HTML path) or build it with `reportlab` — see the `pdf` skill.

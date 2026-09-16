---
name: word
description: Works with word-processor documents (.docx, .odt, .rtf, legacy .doc) in the workspace — read with pandoc, python-docx, and antiword/catdoc, and create or edit with python-docx and pandoc. Use when the user uploads a Word/Writer document, or wants a letter, report, contract, or other formatted document produced.
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

Good for generated content — headings, paragraphs, lists, tables, images, and page breaks.

```python
import docx
from docx.shared import Inches, Pt

d = docx.Document()
d.add_heading("Quarterly report", 0)
d.add_paragraph("Summary paragraph.")
d.add_paragraph("First point", style="List Bullet")
d.add_page_break()
t = d.add_table(rows=1, cols=2)
t.rows[0].cells[0].text = "Metric"
t.rows[0].cells[1].text = "Value"
d.add_picture("bot/chart.png", width=Inches(6))
d.save("/workspace/report.docx")
```

Style runs with `run = p.add_run("bold"); run.bold = True; run.font.size = Pt(12)`. Use `docx.Document("tmp/template.docx")` to fill an existing document instead of starting blank.

## Create: Markdown to docx (pandoc)

Fastest path when the content is already markdown. Headings, lists, tables, and images carry over.

```python
import subprocess
subprocess.run(["pandoc", "tmp/notes.md", "-o", "/workspace/notes.docx"], check=True)
```

Pandoc also writes `.odt` and `.rtf` (`-o out.odt`). Match an existing house style with `--reference-doc=tmp/template.docx`. There is no writer for legacy binary `.doc` — produce `.docx` instead.

## Output

Write the document to `/workspace` and `present` the relative path (a folio). Do not paste the whole document into chat; summarize and point at the file. To also hand back a PDF, convert with `pandoc` + `weasyprint` (HTML path) or build it with `reportlab` — see the `pdf` skill.

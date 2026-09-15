---
name: reading-word
description: Reads word-processor documents (.docx, .odt, .rtf, legacy .doc) in the workspace with pandoc, python-docx, and catdoc/antiword. Use when the user uploads or points at a Word/Writer document and you need its text, structure, tables, or images.
metadata:
  silo_seed: reading-word
---

# Reading Word documents

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`.

## Text / Markdown (docx, odt, rtf)

pandoc keeps headings, lists, and tables:

```python
import subprocess
out = subprocess.run(["pandoc", "tmp/doc.docx", "-t", "gfm"], capture_output=True, text=True)
print(out.stdout[:8000])
```

The same command handles `.odt` and `.rtf`.

## Structured access (python-docx)

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

## Legacy `.doc` (binary)

pandoc and python-docx do not read binary `.doc`. Use `antiword`, then `catdoc`:

```python
print(subprocess.run(["antiword", "tmp/old.doc"], capture_output=True, text=True).stdout)
```

## Convert for the human

```python
subprocess.run(["pandoc", "tmp/doc.docx", "-o", "/workspace/doc.md"], check=True)
```

Then `present doc.md` (a folio). Do not paste the whole document into chat.

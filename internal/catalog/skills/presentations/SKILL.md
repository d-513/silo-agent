---
name: presentations
description: Works with PowerPoint/OpenDocument decks (.pptx, .odp) in the workspace — read slide text, speaker notes, tables, and embedded media with python-pptx, and create or edit decks with python-pptx (text, tables, charts, images, notes). Use when the user uploads or points at a slide deck, or wants a presentation produced. This machine has no LibreOffice, so read and build the pieces rather than a rendered slide.
metadata:
  silo_seed: presentations
---

# Presentations

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`. User-facing decks go under `/workspace`; scratch under `bot/`.

`python-pptx` reads and writes `.pptx`. There is **no LibreOffice** here, so you cannot rasterize a slide directly. Extract text/notes/media instead. If a rendered slide truly matters, open the file on the desktop (Files or Chromium), or `sudo apt-get install -y libreoffice-impress` and render, then `present` the image.

## Read: slide text and speaker notes

```python
from pptx import Presentation
prs = Presentation("tmp/deck.pptx")
for i, slide in enumerate(prs.slides, 1):
    print(f"— slide {i} —")
    for shape in slide.shapes:
        if shape.has_text_frame and shape.text_frame.text.strip():
            print(shape.text_frame.text)
    if slide.has_notes_slide and slide.notes_slide.notes_text_frame.text.strip():
        print("notes:", slide.notes_slide.notes_text_frame.text)
```

## Read: tables and embedded images

Shapes expose `.has_table` / `.table.rows`. For media, unzip the deck (it is a zip):

```python
import zipfile
with zipfile.ZipFile("tmp/deck.pptx") as z:
    print([n for n in z.namelist() if n.startswith("ppt/media/")])
```

## Create: a deck from scratch

Blank template, 16:9. Layouts: `0` title slide, `1` title + content, `5` title only, `6` blank.

```python
from pptx import Presentation
from pptx.util import Inches, Pt

prs = Presentation()
prs.slide_width = Inches(13.333)
prs.slide_height = Inches(7.5)

slide = prs.slides.add_slide(prs.slide_layouts[1])
slide.shapes.title.text = "Quarterly review"
body = slide.placeholders[1].text_frame
body.text = "Revenue up 12%"
p = body.add_paragraph()
p.text = "Churn flat"
p.level = 1
slide.notes_slide.notes_text_frame.text = "Speaker notes for this slide."

prs.save("/workspace/deck.pptx")
```

Style a run with `run = p.runs[0]; run.font.bold = True; run.font.size = Pt(18)`. Add a slide per point; do not cram.

## Create: tables, charts, and images

```python
# table
shape = slide.shapes.add_table(2, 2, Inches(1), Inches(2), Inches(8), Inches(1.5))
tbl = shape.table
tbl.cell(0, 0).text = "Metric"
tbl.cell(0, 1).text = "Value"

# image (render a chart first, e.g. with matplotlib)
slide.shapes.add_picture("bot/chart.png", Inches(1), Inches(1.5), width=Inches(6))

# native chart
from pptx.chart.data import CategoryChartData
from pptx.enum.chart import XL_CHART_TYPE
data = CategoryChartData()
data.categories = ["Q1", "Q2", "Q3"]
data.add_series("Revenue", (4, 5, 6))
slide.shapes.add_chart(XL_CHART_TYPE.COLUMN_CLUSTERED, Inches(1), Inches(1.5), Inches(8), Inches(4.5), data)
```

## Edit: fill an existing template

```python
prs = Presentation("tmp/template.pptx")   # keeps theme and masters
prs.slides[0].shapes.title.text = "New title"
prs.save("/workspace/filled.pptx")
```

Match placeholder text before editing; iterate `slide.placeholders` and set `.text`.

## Output

Write the deck to `/workspace` and `present` the relative path (a folio). Summarize; do not dump every slide into chat. Do not claim you saw a rendered slide you did not render.

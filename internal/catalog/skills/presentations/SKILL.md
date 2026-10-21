---
name: presentations
description: Works with PowerPoint/OpenDocument decks (.pptx, .odp) in the workspace — read slide text, speaker notes, tables, and embedded media with python-pptx, and build themed decks with a ready slide kit (title, section, points, numbers, chart, image, table, quote) that also renders a proof of every slide to look at. Use when the user uploads or points at a slide deck, or wants a presentation produced.
metadata:
  silo_seed: presentations
---

# Presentations

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`. User-facing decks go under `/workspace`; scratch under `bot/`.

`python-pptx` reads and writes `.pptx`. There is **no LibreOffice** on this machine, so an uploaded deck cannot be rasterized — extract text/notes/media in Python instead. Decks you build with `slides.py` (below) come with a proof you can see. The web console renders `.pptx` slides for the human in `present` and `artifact` cards; it draws native pptx charts only as placeholders, so put charts in as images.

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

## Create: a themed deck (default)

Read `DESIGN.md` in this skill once per task. Build with the slide kit; it lays out every slide on one grid in a theme (`ink`, `garden`, `ember`) and warns about overlong titles, more than 5 bullets, or crowded tables.

```python
import sys; sys.path.insert(0, "/opt/silo/skills/presentations/scripts")
import theme
from slides import Deck
import matplotlib.pyplot as plt

t = theme.mpl("ink")                                     # chart in the deck's colours
fig, ax = plt.subplots(figsize=(11, 4.4)); ax.plot(["Jul", "Aug", "Sep"], [94.2, 95.3, 96.2], marker="o")
fig.savefig("bot/chart.png")

d = Deck("ink")
d.title("Q3 operations review", "Northwind Logistics · 21 October 2026")
d.section("01", "Delivery")
d.points("On-time delivery is back above target", ["New carrier moves 38% of regional parcels", "Late deliveries fell from 6.9% to 3.8%"], kicker="Delivery")
d.stats("The quarter in three numbers", [("96.2%", "On-time delivery", "+3.1 pts"), ("€1.84m", "Warehouse cost", "−6%")])
d.chart("Delivery recovered every month", "bot/chart.png", note="Share of parcels on time")
d.table("Cost by site", [["Site", "Q2", "Q3"], ["Lyon", "€812k", "€706k"]], numeric=(1, 2))
d.image("The new Lyon site", "bot/site.jpg", caption="Opened 2 September")
d.quote("The best quarter for delivery since 2019.", "Head of operations")
d.end("Questions", "ops@northwind.example")
d.save("/workspace/deck.pptx")      # prints warnings and the proof sheet path
```

Slide titles are the message ("Costs fell 6%"), not the topic ("Costs"). One idea per slide; 3–5 bullets; speaker notes via `notes="…"` on any method.

## Check: look at it before you present it

`save` writes `bot/proof/<name>-sheet.png` (every slide, drawn from the same layout with the box's fonts). `present` it, fix clipped or crowded slides, weak titles and empty-looking slides, save again, then hand over the deck.

## Create: a deck by hand (python-pptx)

For layouts the kit does not cover. Use the theme's colours and fonts (`theme.get("ink")`) rather than python-pptx defaults.

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

Style a run with `run = p.runs[0]; run.font.bold = True; run.font.size = Pt(18)`. Add a slide per point; do not cram. There is no proof for hand-built slides, so keep them simple.

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

Write the deck to `/workspace` and call `artifact` on the relative path so the human gets a versioned card they can step through slide by slide and download (or `present` to show it inline). Summarize; do not dump every slide into chat. Do not claim you saw a rendered slide you did not render.

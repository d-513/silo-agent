---
name: reading-presentations
description: Reads PowerPoint/OpenDocument decks (.pptx, .odp) in the workspace with python-pptx, extracting slide text, speaker notes, tables, and embedded images. Use when the user uploads or points at a slide deck. This machine has no LibreOffice, so read the pieces rather than a rendered slide.
metadata:
  silo_seed: reading-presentations
---

# Reading presentations

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`.

`python-pptx` reads `.pptx`. There is **no LibreOffice** here, so you cannot rasterize a slide directly. Extract text/notes/media instead. If a visual truly matters, open the file on the desktop (Files or Chromium), or `sudo apt-get install -y libreoffice-impress` and render, then `present` the image.

## Slide text and speaker notes

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

## Tables and embedded images

Shapes expose `.has_table` / `.table.rows`. For media, unzip the deck (it is a zip):

```python
import zipfile
with zipfile.ZipFile("tmp/deck.pptx") as z:
    print([n for n in z.namelist() if n.startswith("ppt/media/")])
```

## Output

Summarize the deck into `/workspace` (markdown) and `present` it. Do not claim you saw a slide you did not render.

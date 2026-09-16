---
name: images
description: Works with image files (.png, .jpg, .webp, .gif, .bmp, .tiff) in the workspace — read and inspect with Pillow, OCR with tesseract, and create or edit with Pillow, matplotlib, and ImageMagick (icons, diagrams, annotations, charts, resizes, GIFs). Use when the user uploads a photo or screenshot, or wants an image produced or edited.
metadata:
  silo_seed: images
---

# Images

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`. User-facing images go under `/workspace`; scratch under `bot/`.

You only receive pixels for `present`ed `.png`, `.jpg`/`.jpeg`, `.webp`, and `.gif`. Convert other formats (`.bmp`, `.tiff`, `.heic`) first. The present budget is bounded — downscale a very large photo before presenting.

## Read: inspect

```python
from PIL import Image, ExifTags
im = Image.open("tmp/photo.jpg")
print(im.format, im.size, im.mode)
print({ExifTags.TAGS.get(k, k): v for k, v in (im.getexif() or {}).items()})
```

## Read: OCR

```python
import subprocess
print(subprocess.run(["tesseract", "tmp/photo.jpg", "-"], capture_output=True, text=True).stdout)
```

For better OCR, upscale to roughly 300 DPI equivalent and grayscale first.

## Read: downscale / convert for `present`

```python
im = Image.open("tmp/photo.jpg").convert("RGB")
im.thumbnail((1600, 1600))
im.save("bot/photo.png")
```

Then `present bot/photo.png` (scratch) or an `out.png` under `/workspace` for the human as a folio.

## Create: draw with Pillow

Good for cards, banners, icons, labels, and quick diagrams.

```python
from PIL import Image, ImageDraw, ImageFont

im = Image.new("RGB", (1200, 630), "white")
d = ImageDraw.Draw(im)
font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 64)
d.text((60, 240), "Hello", fill="#1a1a1a", font=font)
d.rectangle([40, 40, 1160, 590], outline="#1a1a1a", width=4)
im.save("/workspace/card.png")
```

`ellipse`, `line`, `polygon`, and `rounded_rectangle` round out diagrams. Composite or watermark with `Image.alpha_composite` / `Image.blend`.

## Create: charts (matplotlib)

matplotlib and numpy are installed. Save a chart to `/workspace` and `present` the PNG. Do not describe a chart you have not rendered.

```python
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
plt.bar(["A", "B", "C"], [3, 7, 5], color="#3b6ea5")
plt.title("Counts")
plt.savefig("/workspace/chart.png", dpi=150, bbox_inches="tight")
```

## Edit and batch (ImageMagick)

The `convert`/`mogrify` CLI does resizing, cropping, rotation, annotation, and montage without code:

```python
import subprocess
subprocess.run(["convert", "tmp/in.jpg", "-resize", "800x", "-quality", "85", "/workspace/out.jpg"], check=True)
subprocess.run(["convert", "tmp/a.png", "tmp/b.png", "+append", "/workspace/stacked.png"], check=True)
subprocess.run(["montage", "tmp/frame-*.png", "-tile", "3x2", "/workspace/contact.png"], check=True)
```

## Create: animated GIF

```python
import subprocess
subprocess.run(["convert", "-delay", "20", "-loop", "0", "tmp/frame-*.png", "/workspace/anim.gif"], check=True)
```

Or with Pillow: `frames[0].save("/workspace/anim.gif", save_all=True, append_images=frames[1:], duration=100, loop=0)`.

## Output

Write user-facing images to `/workspace` and `present` the relative path (a folio). `present` a `bot/…` path when you only need the pixels yourself. Never paste base64 image bytes into chat or a file tool.

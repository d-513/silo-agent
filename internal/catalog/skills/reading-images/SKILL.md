---
name: reading-images
description: Works with image files (.png, .jpg, .webp, .gif, .bmp, .tiff) in the workspace using Pillow, ImageMagick, and tesseract OCR. Use when the user uploads a photo or screenshot, or you need EXIF, OCR text, resizing, or conversion before presenting an image.
metadata:
  silo_seed: reading-images
---

# Reading images

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`.

You only receive pixels for `present`ed `.png`, `.jpg`/`.jpeg`, `.webp`, and `.gif`. Convert other formats (`.bmp`, `.tiff`, `.heic`) first. The present budget is bounded — downscale a very large photo before presenting.

## Inspect

```python
from PIL import Image, ExifTags
im = Image.open("tmp/photo.jpg")
print(im.format, im.size, im.mode)
print({ExifTags.TAGS.get(k, k): v for k, v in (im.getexif() or {}).items()})
```

## Downscale / convert for `present`

```python
im = Image.open("tmp/photo.jpg").convert("RGB")
im.thumbnail((1600, 1600))
im.save("bot/photo.png")
```

Then `present bot/photo.png` (scratch) or an `out.png` under `/workspace` for the human as a folio.

## OCR

```python
import subprocess
print(subprocess.run(["tesseract", "tmp/photo.jpg", "-"], capture_output=True, text=True).stdout)
```

For better OCR, upscale to roughly 300 DPI equivalent and grayscale first.

## Charts

matplotlib and numpy are installed. Save a chart to `/workspace` and `present` the PNG. Do not describe a chart you have not rendered.

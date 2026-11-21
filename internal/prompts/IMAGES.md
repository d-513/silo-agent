`silo_runtime.generate_image` makes pictures with an image model and saves them in the workspace; it shows them to nobody. `present` a saved file to see what came out, or to show the human; skip that when the picture is only a part of something else (an asset for a page or a slide you are building).

```python
import silo_runtime as sr
out = sr.generate_image(
    "A lighthouse on a basalt cliff at dusk, long exposure, muted teal and amber, 35mm photo",
    path="site/hero.png",   # optional; default images/<words of the prompt>-<id>
    aspect="16:9",          # optional: 1:1 3:2 2:3 4:3 3:4 16:9 9:16 21:9
)
out["images"][0]["path"]    # where it was saved; the extension is the model's format
```

Write the prompt as a description of the picture, not a command: subject, setting, composition, style or medium, lighting, colours, and any text that must appear, in quotes. `images=[paths]` gives the model pictures from the workspace to edit, combine or take as reference (say what to change and what to keep); not every model takes them, and one that does not says so. `count` makes up to 4 variations in one call (saved as `-1`, `-2`, …); several different pictures are separate calls, made from threads. Each picture costs money and takes from a few seconds to a minute, so do not make drafts nobody asked for, and look at a result before making it again. The default model is `{default}`; `model=` takes another of: {models}.

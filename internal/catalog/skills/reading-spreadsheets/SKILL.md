---
name: reading-spreadsheets
description: Reads spreadsheets (.xlsx, .xls, .ods, .csv) in the workspace with pandas and openpyxl/xlrd/odf. Use when the user uploads or points at a workbook and you need sheets, cells, formulas, summaries, or a rendered table.
metadata:
  silo_seed: reading-spreadsheets
---

# Reading spreadsheets

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`.

pandas is installed. Engines: openpyxl (`.xlsx`), xlrd (`.xls`), odfpy (`.ods`). CSV needs no engine.

## Enumerate sheets first

```python
import pandas as pd
book = pd.read_excel("tmp/book.xlsx", sheet_name=None)  # dict of DataFrames
for name, df in book.items():
    print(name, df.shape, list(df.columns))
```

## Read one sheet

```python
df = pd.read_excel("tmp/book.xlsx", sheet_name="Sheet1")
print(df.head(20).to_markdown(index=False))
```

## Formulas and raw cells (openpyxl)

pandas gives computed values. To see formulas or formatting:

```python
import openpyxl
wb = openpyxl.load_workbook("tmp/book.xlsx", data_only=False)
ws = wb.active
for row in ws.iter_rows(min_row=1, max_row=10, values_only=True):
    print(row)
```

## Write a user-facing table

Markdown reads best in the thread:

```python
open("/workspace/book.md", "w").write(df.to_markdown(index=False))
```

Then `present book.md`. For a large or wide sheet, summarize in markdown and put the full CSV at `/workspace` instead.

Never paste thousands of rows into chat.

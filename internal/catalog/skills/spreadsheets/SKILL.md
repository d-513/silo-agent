---
name: spreadsheets
description: Works with spreadsheets (.xlsx, .xls, .ods, .csv) in the workspace — read with pandas and openpyxl/xlrd/odf, and create or edit with openpyxl and pandas. Use when the user uploads or points at a workbook, or wants a table, report, budget, formula sheet, or chart produced as a spreadsheet.
metadata:
  silo_seed: spreadsheets
---

# Spreadsheets

Chat uploads land in `tmp/`, wiped on every machine start. Paths are relative to `/workspace`. User-facing workbooks go under `/workspace`; scratch under `bot/`.

pandas is installed. Engines: openpyxl (`.xlsx`), xlrd (`.xls`), odfpy (`.ods`). CSV needs no engine.

## Read: enumerate sheets first

```python
import pandas as pd
book = pd.read_excel("tmp/book.xlsx", sheet_name=None)  # dict of DataFrames
for name, df in book.items():
    print(name, df.shape, list(df.columns))
```

## Read: one sheet

```python
df = pd.read_excel("tmp/book.xlsx", sheet_name="Sheet1")
print(df.head(20).to_markdown(index=False))
```

## Read: formulas and raw cells (openpyxl)

pandas gives computed values. To see formulas or formatting:

```python
import openpyxl
wb = openpyxl.load_workbook("tmp/book.xlsx", data_only=False)
ws = wb.active
for row in ws.iter_rows(min_row=1, max_row=10, values_only=True):
    print(row)
```

## Create: openpyxl

Use this when formatting, formulas, charts, or multiple sheets matter.

```python
import openpyxl
from openpyxl.styles import Font, PatternFill

wb = openpyxl.Workbook()
ws = wb.active
ws.title = "Report"
ws.append(["Item", "Qty", "Price", "Total"])
ws.append(["Widget", 3, 4.5, None])
ws["D2"] = "=B2*C2"                       # formula, not a computed value
ws["A1"].font = Font(bold=True)
for col in "ABCD":
    ws[f"{col}1"].fill = PatternFill("solid", fgColor="DDDDDD")
    ws.column_dimensions[col].width = 14
ws.freeze_panes = "A2"
ws["D2"].number_format = "#,##0.00"
wb.save("/workspace/report.xlsx")
```

## Create: charts (openpyxl)

```python
from openpyxl.chart import BarChart, Reference
chart = BarChart()
chart.title = "Qty by item"
data = Reference(ws, min_col=2, min_row=1, max_row=2)
cats = Reference(ws, min_col=1, min_row=2, max_row=2)
chart.add_data(data, titles_from_data=True)
chart.set_categories(cats)
ws.add_chart(chart, "F2")
```

`LineChart` and `PieChart` work the same way.

## Create: from Python data (pandas)

Quickest for a plain table or CSV:

```python
df = pd.DataFrame([{"name": "Ada", "score": 9}, {"name": "Linus", "score": 7}])
df.to_excel("/workspace/scores.xlsx", index=False)
df.to_csv("/workspace/scores.csv", index=False)
```

## Write a user-facing summary

Markdown reads best in the thread:

```python
open("/workspace/book.md", "w").write(df.to_markdown(index=False))
```

Then `present book.md` — Markdown previews inline. For a large or wide sheet, summarize in markdown and `artifact` the full workbook at `/workspace` (the thread cannot preview `.xlsx`).

Never paste thousands of rows into chat.

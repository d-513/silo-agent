"""Silo document themes for Python: the same colours and type as design.css.

    import sys; sys.path.insert(0, "/opt/silo/skills/<skill>/scripts")
    import theme
    t = theme.get("ink")          # ink | garden | ember
    theme.mpl("ink")              # matplotlib rcParams; then plot with t.series
    theme.docx(doc, "garden")     # python-docx: fonts, sizes, colours, margins
"""
from types import SimpleNamespace

THEMES = {
    # paper, ink, muted, rule, tint, accent, accent_soft, accent_2; fonts for print and for Office files
    "ink": dict(paper="#ffffff", ink="#1b1f24", muted="#5b6470", rule="#d9dde2", tint="#f3f5f8",
                accent="#1f4e8c", accent_soft="#e6edf7", accent_2="#7a8ca6",
                display="Inter Display", body="Inter", office_display="Georgia", office_body="Calibri"),
    "garden": dict(paper="#fbf8f1", ink="#23241f", muted="#66665c", rule="#ddd6c6", tint="#f2ecdf",
                   accent="#2e6b4f", accent_soft="#e2ece4", accent_2="#b5893a",
                   display="EB Garamond", body="Linux Libertine O", office_display="Georgia", office_body="Georgia"),
    "ember": dict(paper="#fffaf5", ink="#26201c", muted="#6e625a", rule="#e5d9cd", tint="#f7eee5",
                  accent="#b4532a", accent_soft="#f6e3d8", accent_2="#3d5a6c",
                  display="Linux Libertine Display O", body="Inter", office_display="Georgia", office_body="Calibri"),
}


def get(name="ink"):
    t = dict(THEMES[name])
    # Chart series: lead with the accent, then quieter companions. Never more than 4.
    t["series"] = [t["accent"], t["accent_2"], t["muted"], t["rule"]]
    t["name"] = name
    return SimpleNamespace(**t)


def mpl(name="ink"):
    """Restrained matplotlib defaults: no top/right spines, light y grid, theme fonts."""
    import matplotlib
    matplotlib.use("Agg")
    from cycler import cycler
    t = get(name)
    matplotlib.rcParams.update({
        "font.family": "sans-serif",
        "font.sans-serif": ["Inter", "Noto Sans", "DejaVu Sans"],
        "font.size": 9,
        "axes.titlesize": 11, "axes.titleweight": "semibold", "axes.titlelocation": "left",
        "axes.labelsize": 8.5, "axes.labelcolor": t.muted,
        "axes.edgecolor": t.rule, "axes.linewidth": 0.8,
        "axes.spines.top": False, "axes.spines.right": False,
        "axes.grid": True, "axes.grid.axis": "y", "grid.color": t.rule, "grid.linewidth": 0.6,
        "axes.prop_cycle": cycler(color=t.series),
        "xtick.color": t.muted, "ytick.color": t.muted, "xtick.labelsize": 8, "ytick.labelsize": 8,
        "xtick.major.size": 0, "ytick.major.size": 0,
        "legend.frameon": False, "legend.fontsize": 8,
        "figure.facecolor": t.paper, "axes.facecolor": t.paper, "savefig.facecolor": t.paper,
        "figure.dpi": 200, "savefig.dpi": 200, "savefig.bbox": "tight",
        "lines.linewidth": 2, "lines.solid_capstyle": "round",
    })
    return t


def _untheme(style):
    # Built-in styles name a theme font (asciiTheme), which beats font.name; drop it.
    rpr = style.element.get_or_add_rPr()
    fonts = rpr.find("{http://schemas.openxmlformats.org/wordprocessingml/2006/main}rFonts")
    if fonts is not None:
        for k in list(fonts.attrib):
            if k.endswith("Theme"):
                del fonts.attrib[k]


def docx(doc, name="ink"):
    """Style a python-docx Document. Office fonts (Georgia/Calibri) travel to the reader's Word."""
    from docx.enum.text import WD_LINE_SPACING
    from docx.shared import Mm, Pt, RGBColor
    t = get(name)
    rgb = lambda h: RGBColor.from_string(h.lstrip("#").upper())
    for s in doc.sections:
        s.top_margin, s.bottom_margin = Mm(22), Mm(24)
        s.left_margin = s.right_margin = Mm(22)
    st = doc.styles
    n = st["Normal"]
    n.font.name, n.font.size, n.font.color.rgb = t.office_body, Pt(10.5), rgb(t.ink)
    _untheme(n)
    n.paragraph_format.space_after = Pt(6)
    n.paragraph_format.line_spacing_rule = WD_LINE_SPACING.MULTIPLE
    n.paragraph_format.line_spacing = 1.2
    sizes = {"Title": 30, "Heading 1": 18, "Heading 2": 13.5, "Heading 3": 11}
    for style, size in sizes.items():
        h = st[style]
        h.font.name, h.font.size, h.font.bold = t.office_display, Pt(size), style != "Title"
        h.font.color.rgb = rgb(t.ink)
        h.font.italic = False
        h.paragraph_format.space_before = Pt(0 if style == "Title" else size * 1.1)
        h.paragraph_format.space_after = Pt(size * 0.4)
        h.paragraph_format.keep_with_next = True
        _untheme(h)
    st["Heading 3"].font.color.rgb = rgb(t.accent)
    if "Subtitle" in [s.name for s in st]:
        sub = st["Subtitle"]
        sub.font.name, sub.font.size, sub.font.italic = t.office_body, Pt(13), False
        sub.font.color.rgb = rgb(t.muted)
        _untheme(sub)
    return t


def docx_table(table, name="ink", numeric_cols=()):
    """Horizontal rules only, bold small-caps header, right-aligned numeric columns."""
    from docx.enum.text import WD_ALIGN_PARAGRAPH
    from docx.oxml import OxmlElement
    from docx.oxml.ns import qn
    from docx.shared import Pt, RGBColor
    t = get(name)

    def border(cell, edge, size, color):
        tcpr = cell._tc.get_or_add_tcPr()
        b = tcpr.find(qn("w:tcBorders"))
        if b is None:
            b = OxmlElement("w:tcBorders")
            tcpr.append(b)
        e = OxmlElement(f"w:{edge}")
        e.set(qn("w:val"), "single")
        e.set(qn("w:sz"), str(size))
        e.set(qn("w:color"), color.lstrip("#"))
        b.append(e)

    tbl = table._tbl.tblPr
    borders = OxmlElement("w:tblBorders")
    for edge in ("top", "left", "bottom", "right", "insideH", "insideV"):
        e = OxmlElement(f"w:{edge}")
        e.set(qn("w:val"), "nil")
        borders.append(e)
    tbl.append(borders)
    for r, row in enumerate(table.rows):
        for c, cell in enumerate(row.cells):
            border(cell, "bottom", 8 if r == 0 else 4, t.ink if r == 0 else t.rule)
            for p in cell.paragraphs:
                p.paragraph_format.space_after = Pt(2)
                if c in numeric_cols:
                    p.alignment = WD_ALIGN_PARAGRAPH.RIGHT
                for run in p.runs:
                    run.font.size = Pt(9)
                    if r == 0:
                        run.font.bold = True
                        run.font.color.rgb = RGBColor.from_string(t.muted.lstrip("#").upper())
    return table

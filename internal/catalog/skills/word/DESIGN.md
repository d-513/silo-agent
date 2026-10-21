# Design reference (shared by pdf, word, presentations)

The goal is a document a person would choose to keep: calm, clear hierarchy, one accent colour, real typefaces. `design.css` (HTML/PDF) and `scripts/theme.py` (matplotlib, python-docx, python-pptx) encode everything below; use them instead of inventing values.

## Themes

Pick one per document; never mix. Class on `<html>` or `theme.get("…")`.

| Theme | Feel | Accent | Paper | Display / body (print) | Good for |
|---|---|---|---|---|---|
| `ink` | sober, modern | navy `#1f4e8c` | white | Inter Display / Inter | reports, proposals, one-pagers, decks |
| `garden` | warm, personal | green `#2e6b4f` + ochre `#b5893a` | cream `#fbf8f1` | EB Garamond / Linux Libertine O | newsletters, family and club news, invitations |
| `ember` | travel, events | terracotta `#b4532a` + slate `#3d5a6c` | `#fffaf5` | Linux Libertine Display O / Inter | itineraries, programmes, menus |

Text is `ink` (near-black), secondary text `muted` (grey), hairlines `rule`, panels `tint`, highlights `accent-soft`. The accent marks at most a few things per page: kickers, a rule, the one number that matters, the lead chart series.

## Type

- Typefaces installed on the box: **Inter** / **Inter Display** (sans, 9 weights), **EB Garamond** (display serif; regular, italic, bold), **Linux Libertine O** (book serif with bold, semibold, italic) and **Linux Biolinum O**. Noto Sans/Serif is the fallback for other scripts.
- Office files (`.docx`, `.pptx`) travel to other machines: use **Georgia** (headings) and **Calibri** (body), which every Word/PowerPoint has; `theme.py` does this.
- Scale (pt), print: caption 7.5 · small 9 · body 10–10.5 · lede 12.5 · h3 12.5 · h2 16 · h1 22 · cover 34–48. Slides: caption 11 · body 20–22 · title 34 · cover 46 · big number 60.
- Body line height 1.45–1.5; headings 1.05–1.2. Measure 55–80 characters: on A4 use two columns or a sidebar rather than a 170 mm line of 10 pt text.
- Hierarchy comes from size, weight and space, in that order; one weight step (400 → 600) is enough. Uppercase only for short kickers/labels, letter-spaced (`.kicker`, `h4`).
- Body text is left-aligned (ragged right). Centre only a masthead, a cover title or a sign-off.
- Numbers: tabular figures (on in `design.css`), right-aligned in tables, units and thousands separators consistent.

## Space

- A4 margins 20–22 mm (newsletter 14–16 mm), bottom slightly larger than top. Slides: 0.8 in sides.
- Spacing steps in mm: 1.5 · 3 · 6 · 9 · 12. Space above a heading is about twice the space below it.
- Leave room. Empty space is what makes a page look designed; filling every corner looks generated.

## Elements

- **Cover / title**: one strong block (accent band or full-bleed colour), the title large, a one-sentence lede, and a meta line (for whom, period, date). No clip-art, no logos you were not given.
- **Key numbers**: 2–4 `.stat` blocks (big value, small uppercase label, optional delta in the accent) before the prose they summarise.
- **Tables**: horizontal hairlines only, a darker rule under the header, small uppercase header labels, no zebra stripes, no vertical lines, totals row in semibold with a rule above. 7 or fewer columns.
- **Charts**: `theme.mpl(name)` then plot. One message per chart, said in the title ("Delivery recovered every month"), not "Chart 1". Lead series in the accent, others muted; direct labels over legends; no 3D, no pie with more than 4 slices, no gridlines on x. Save PNG at 200 dpi, width matching the column.
- **Callout**: `.callout` for the one decision or warning; `.pullquote` for one quote per page at most.
- **Photos**: real ones the human gave you (or ones you made); full column width, a caption under each. Never stretch: keep the aspect ratio, crop with Pillow if needed.
- **Lists**: 3–5 items. More than that is a table or prose. Never a page that is only bullets.

## Avoid

- Default output: Liberation/Arial/DejaVu, reportlab's sample stylesheet, pandoc's bare HTML, Word's blue "Title" style untouched.
- Rainbow or saturated multi-colour palettes, gradients, drop shadows, rounded "card" boxes everywhere, emoji as icons.
- Walls of bullets, everything bold, more than three type sizes on one page, centred body text, justified text in narrow columns.
- A last page holding two lines, a heading alone at the bottom of a page, a table split after its header row.
- Placeholder text, lorem ipsum, "Insert image here", invented data presented as real.

## Check before you hand it over

Render, look, fix, repeat: `python3 /opt/silo/skills/<skill>/scripts/render.py <file>` writes a contact sheet to `bot/proof/` and prints warnings (fallback fonts, near-empty last page). `present` the sheet; you see every page. Look for: overflow or clipped text, orphaned headings, cramped or empty areas, wrong fonts, unreadable chart labels, anything misaligned. Use `--page N` for a close look at one page. Only `present` the final file once the proof is clean.

WeasyPrint here is version 57: `var()` works only in longhand properties (`border-top-color: var(--accent)`, not `border-top: 1pt solid var(--accent)`), and there is no CSS grid and no `gap`; use floats, tables, `columns` or flex with margins.

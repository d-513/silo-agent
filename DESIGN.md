---
name: Silo
description: Daylight archive for isolated Bots. Plaster, iron type, bindery blue. Each Bot is a folio with a picked crest; its desktop is a dark hatch set into the page.
colors:
  surface: '#F3F0E8'
  surface-dim: '#E4E0D6'
  surface-bright: '#F8F6F0'
  surface-container-lowest: '#FFFcf7'
  surface-container-low: '#F8F6F0'
  surface-container: '#EDE9DF'
  surface-container-high: '#E4E0D6'
  surface-container-highest: '#D8D3C8'
  on-surface: '#1E2126'
  on-surface-variant: '#5F5E58'
  inverse-surface: '#1E2126'
  inverse-on-surface: '#F3F0E8'
  outline: '#C9C3B6'
  outline-variant: '#DDD8CE'
  surface-tint: '#2A3F5F'
  primary: '#2A3F5F'
  on-primary: '#F3F0E8'
  primary-container: '#D7DEE8'
  on-primary-container: '#1A2A42'
  inverse-primary: '#9AADC8'
  secondary: '#3D6F6A'
  on-secondary: '#F3F0E8'
  secondary-container: '#D5E4E1'
  on-secondary-container: '#1E3A37'
  tertiary: '#5C6B7A'
  on-tertiary: '#F3F0E8'
  tertiary-container: '#D8DEE4'
  on-tertiary-container: '#2A333C'
  error: '#A33B4A'
  on-error: '#F3F0E8'
  error-container: '#F0D4D7'
  on-error-container: '#6E2230'
  primary-fixed: '#D7DEE8'
  primary-fixed-dim: '#9AADC8'
  on-primary-fixed: '#1A2A42'
  on-primary-fixed-variant: '#2A3F5F'
  secondary-fixed: '#D5E4E1'
  secondary-fixed-dim: '#8FB0AB'
  on-secondary-fixed: '#1E3A37'
  on-secondary-fixed-variant: '#3D6F6A'
  tertiary-fixed: '#D8DEE4'
  tertiary-fixed-dim: '#9AABBA'
  on-tertiary-fixed: '#2A333C'
  on-tertiary-fixed-variant: '#5C6B7A'
  background: '#F3F0E8'
  on-background: '#1E2126'
  surface-variant: '#EDE9DF'
typography:
  display-lg:
    fontFamily: IBM Plex Sans
    fontSize: 40px
    fontWeight: '500'
    lineHeight: 48px
    letterSpacing: -0.02em
  headline-md:
    fontFamily: IBM Plex Sans
    fontSize: 22px
    fontWeight: '500'
    lineHeight: 28px
    letterSpacing: -0.015em
  body-base:
    fontFamily: IBM Plex Sans
    fontSize: 14px
    fontWeight: '400'
    lineHeight: 22px
    letterSpacing: '0'
  body-bold:
    fontFamily: IBM Plex Sans
    fontSize: 14px
    fontWeight: '550'
    lineHeight: 22px
    letterSpacing: '0'
  label-caps:
    fontFamily: IBM Plex Sans
    fontSize: 11px
    fontWeight: '500'
    lineHeight: 16px
    letterSpacing: 0.08em
  stat-lg:
    fontFamily: IBM Plex Mono
    fontSize: 20px
    fontWeight: '500'
    lineHeight: 28px
    letterSpacing: -0.02em
  mono:
    fontFamily: IBM Plex Mono
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 20px
    letterSpacing: '0'
rounded:
  sm: 0.25rem
  DEFAULT: 0.375rem
  md: 0.625rem
  lg: 0.875rem
  xl: 1rem
  full: 9999px
spacing:
  unit: 4px
  xs: 4px
  sm: 8px
  md: 16px
  lg: 24px
  xl: 32px
  gutter: 20px
  margin-mobile: 16px
  margin-desktop: 28px
---

# Design System: Silo

Desktop web app. Light by default. A clerk’s desk looking into sealed machines — not a dark ops room, not a chat product.

The previous pass was a warm kiln (lamp black + ochre). That is retired. No orange, amber, gold, rust, or “safety yellow” anywhere.

Bot crests are flat colored shapes with two small dots, picked on New Bot.

## 1. Visual Theme & Atmosphere

An archive in daylight. The page is plaster — a cool paper, slightly toothy, never white-hot. Type is iron. The one structural color is **bindery blue**, the ink a bookbinder uses on cloth: deep, dry, a little grey. It is for primary actions, the active rail, and focus. It is not a sky and not a neon link.

Each Bot is a **folio**: a crest (a simple colored shape with two dots), a name, a short last line, a 7px lamp. Open a folio and the live desktop appears as a **hatch** — a dark rectangle set into the plaster, like a night window in a light room. That contrast is the product. The Bot’s machine is the only darkness on the page.

When a Bot needs a human, a **carmine ribbon** marks the folio (2px left edge + lamp). Carmine is a wax-seal red, used nowhere else except Deny. Working is pine, a quiet green, lamp only.

No glass, no glow, no gradient, no purple, no orange. No 3D robots. Personality is the crest and the name.

## 2. Color Palette & Roles

### Primary Foundation

- **Plaster** (`#F3F0E8`) — app background. The room.
- **Folio** (`#FFFcf7`) — cards, drawers, the approval slip.
- **Cloth** (`#EDE9DF`) — rail, inset wells, table header.
- **Linen** (`#E4E0D6`) — pressed / selected wells.
- **Thread** (`#C9C3B6` / `#DDD8CE`) — hairline borders. Structural only.

### Accent & Interactive

- **Bindery Blue** (`#2A3F5F`) — primary buttons, active nav, focus ring. The ink.
- **Bindery Pale** (`#D7DEE8`) — selected row, quiet well.
- Do not introduce a second blue. Hover darkens bindery slightly (`#1A2A42`), it does not lighten into sky.

### Typography & Text Hierarchy

- **Iron** (`#1E2126`) — titles, Bot names, body.
- **Stone** (`#5F5E58`) — timestamps, hints, inactive rail, masked secrets as `••••`.

### Functional States

- **Online** — **Pine** (`#3D6F6A`) lamp, no pulse. Worker is up. Never label this Idle.
- **Working** — Pine lamp, small pulse (1.5s, opacity 1 → 0.45).
- **Needs you** — **Carmine** (`#A33B4A`) lamp + 2px carmine ribbon on the folio’s left edge.
- **Stopped** — Thread lamp, Stone name.
- The rail lamp is a pip on the crest corner (cloth ring), not on the account initial.
- **Deny / error** — Carmine fill, plaster label. Same red as the ribbon, never orange.
- **MCP badge** — Slate (`#5C6B7A`) cloth chip.
- **Python badge** — Pine cloth chip.

Crests are a filled shape plus two small dots — a mark, not a face. Eight shapes. Fills are a rainbow: plaster, brown, carmine, orange, yellow, green, pine, bindery, purple, pink, stone, iron. Palette colors where they already sit on the wheel; plain hues for the rest. Light fills get a thread stroke.

## 3. Typography Rules

### Hierarchy & Weights

**IBM Plex Sans** for UI. **IBM Plex Mono** for run IDs, paths, `gmail.send`, secret names, terminal, approval args. Mono at the same size as nearby UI.

Weights 400–550. No black, no ultra.

- Display 40/500 — empty archive, sign-in only.
- Page title 22/500 — `Bots`, the Bot name in the hatch header.
- Body 14/400, 22px line.
- Section labels 11/500, +0.08em, Stone. Title case.
- Bot name on a folio: 16/500 Iron.
- Status word: 12/500, color of the lamp.

### Spacing Principles

4px baseline. Folios pad 16–20. Rail is 64px and stays 64. Run view gutters 12px so the hatch gets the pixels. Left-aligned. No centered product screens except sign-in.

## 4. Component Stylings

### Buttons

Height 36px. Radius 6px. Primary: bindery fill, plaster label. Secondary: Folio fill, Thread border, iron label. Ghost: Stone label, iron on hover. Deny: carmine fill, plaster label — approval slip only.

Start Bot / Stop Bot and Send nest a 20px square glyph on the right (power for the machine, arrow for Send). Never a media stop-square on the header — that reads as abort-the-reply. Header Stop Bot is ghost. Press `scale(0.98)`. Focus is 1px bindery, no glow.

No pills. Rail icons are 40px hits with tooltips.

### Folio cards (Bots)

Not dashboard tiles.

- Folio fill, 10px radius, 1px Thread.
- Left: 56px crest (the shape, no plate).
- Name, optional one-line description in Stone, last task in Stone, 7px lamp.
- Hover: border to `#B9B3A6`. No shadow, no lift.
- Needs you: 2px carmine ribbon on the left edge only.

Empty archive: outline of a crest, display line “No Bots yet”, one bindery button “New Bot”. No rockets.

### Navigation

64px rail, Cloth, Thread on the right. Top: Silo mark — one vertical rounded-rect + `Silo` 13/500, stacked. Then **Bots**, a scrollable stack of 28px crests (one per Bot), **+** for New Bot, **Admin**. Bottom: user initial in a 28px iron-on-linen circle. No names in the rail; `title` tooltips only.

Active home/Admin: Bindery Pale well, 2px bindery bar on the left of the icon. Active crest: same well + bar. Needs you: 2px carmine ribbon instead of the bindery bar, plus the lamp. Icons 20px, 1.75 stroke, iron. Crest lamp is 7px on the shape.

### Inputs

Folio or Cloth fill, 1px Thread, 6px radius. Label above, 12/500 Stone, always visible. Focus: 1px bindery, no glow. Error: carmine border + 12px sentence.

Secret values never render. `••••••••` in mono. No eye. No copy. Rotate / delete only.

### Bot crests

Picked on New Bot: one of eight silhouettes (circle, blob, squircle, pill, triangle, hex, cloud, drop) and a rainbow of fills. Same two small dots on every shape. Packed as `color * 8 + shape` in the `crest` int.

Sizes: 20 / 28 / 56 / 88. The lamp is never a substitute for the crest. No generate / upload.

### The hatch (desktop)

The VNC surface is a dark window in a light room.

- Outer: iron (`#1E2126`), 10px radius.
- 8px inner matte `#12141A`.
- Top strip 32px, iron: crest 20px, name, `Desktop`, 6px lamp, spacer, status in mono (plaster at 80%).
- Framebuffer fills the rest.
- Worker down: Cloth well on the plaster (not a fake hatch), Stone sentence “Desktop not connected”, secondary “Start Bot”.

### Approval slip

A 400px Folio panel from the right, Thread on the left. Paper, not a modal dim-to-black.

- Crest, Bot name, `Needs you` in carmine
- Title from the security catalog (`Read a secret`), not `secrets.get`
- One Stone sentence, then labeled fields on Cloth (Secret → `TEST`). No raw JSON.
- **Allow once** (bindery), **Always allow this action** (secondary), **Deny** (carmine)
- Stone: “This run is paused until you choose.”

No `confirm()`. No toast.

### Thread (chat)

The Chat tab. Not a marketing chat.

- User: folio well, 4px bindery bar on the left. The product’s “bubble” — left spine, not iMessage.
- Assistant: iron, no well. Markdown (headings, lists, tables, fenced code).
- Thinking: spinner + “Thinking” while streaming; collapsed “Thought” when done.
- Tool lines: left-aligned cloth row **below** the text (not a centered divider). Icon + `Using Python` / `Used Python`. Collapsed by default; click to expand. Pretty body: Python shows the code as it streams, patch shows a +/− diff, terminal shows the command. No raw JSON as the primary view.
- Composer: Cloth well, placeholder “Ask this Bot…”, bindery send.

### Status lamp

7px circle on the crest corner (cloth ring), never on the account initial. Color = state. Online is pine, solid. Working is the only pulse.

## 5. Layout Principles

### Grid & Structure

Desktop-first, 1280 and 1440. Settings pages max 960. The Bot run view is the remaining viewport after the 64px rail — full width. Cockpit, not article.

**Shell:** rail 64 | main.

**Bots:** padding 28. Header `Bots` + `New Bot`. Grid: 3 columns at 1440, 2 at 1100, 1 below.

**Bot chat:** header 56px (crest, name, lamp, tabs, Start Bot / Stop Bot). Body: chats list 240 | thread `1fr`. Tabs: `Chat`, `Desktop`, `Files`, `Secrets`, `Rules`, `Container`, `Settings`. The hatch lives only on `Desktop`, full main column. Files is a workspace browser: breadcrumbs, type icons, preview (images, PDF, media, markdown, code, docx), upload / new / download. Delete is a second click, not `confirm()`. Settings is a 760px column (name + description now; more later).

**Settings / admin:** header + one 720–800px column. Lists. Bot Settings: name, description, SOUL, MEMORY (mono wells).

### Whitespace Strategy

Home can breathe. Run view is 12px tight. No page footer. No KPI row above the folios.

### Alignment

Left spine. Crests left of names. Header actions right. One ribbon in the grid at a time is enough.

### Responsive

v1 is a desktop operator UI. Below 960: rail becomes a 48px top bar; run stacks thread then hatch; approval becomes a bottom sheet. 40px hits. No separate marketing site.

## 6. Design System Notes for Stitch Generation

### Language to Use

Say: plaster, iron, folio, crest, hatch, ribbon, slip, bindery, pine, cloth, stone.

Do not say: dashboard, kiln, ochre, lamp black, bay, AI copilot, glassmorphism, neon, vibrant, sleek, gradient hero.

Do not use orange, amber, gold, rust, or warm brown — even as a hover.

One screen per prompt. Apply this design system at the project level; do not restate hex in generation prompts.

### Color References

Plaster page. Folio cards. Iron type. Stone secondary. Bindery blue for primary and focus. Pine for working. Carmine only for “Needs you” and Deny.

### Screen map

| # | Screen | Purpose |
|---|---|---|
| 1 | Sign in | Session. Email + password. |
| 2 | Bots | Home. Every folio the user can open. |
| 3 | New Bot | Name, description, crest preview, create. |
| 13 | Bot · Settings | Name and description. More knobs later. |
| 4 | Bot · Run | Thread + dark hatch. Default. |
| 5 | Bot · Run · Needs you | Same, approval slip open. |
| 6 | Bot · Desktop | Hatch full-bleed. |
| 7 | Bot · Connectors | MCP and Python on this Bot. |
| 8 | New connector | Type, name, MCP or git SHA. |
| 9 | Bot · Secrets | Named secrets. Values never shown. |
| 10 | Bot · Rules | Allow / ask / deny per action. |
| 11 | Admin | Product settings. Not operator YAML. |
| 12 | Audit | Who allowed what. |

### Screen prompts

Paste these one at a time. Attach the crest sheet on screens 2–6.

**1 · Sign in**

```
Desktop web, sign-in for Silo.
Full plaster canvas. No rail.
A 400px folio card, centered but slightly above true center, 1px thread border.
Silo mark: one vertical rounded rectangle and the word Silo, iron.
Title: Sign in. Two fields, labels above: Email, Password.
Primary button bindery blue: Sign in.
No hero, no marketing line, no gradient, no orange.
```

**2 · Bots**

```
Desktop web, 1440 wide. 64px left rail on cloth (Silo mark, Bots active, crest stack, +, Admin, user initial at bottom).
Main plaster: header “Bots” left, primary “New Bot” right.
Stone subtitle: “Machines you can open.”
A 3-column grid of folio cards. Six Bots. Use the attached crests. Each has a name, one-line last task, 7px lamp.

Populate:
- Owl (Mail) — Idle — “Sorted the inbox down to 12”
- Fox (Scout) — Working — “Reading the pricing page”
- Scarab (Crawler) — Needs you — “Captcha on the vendor login” — carmine left ribbon
- Kettle (House) — Idle — “No runs this week”
- Fish (Ledger) — Working — “exec_python · reconcile.py”
- Key (Vault) — Stopped — “Stopped by you”

No KPI row. No search. No orange. The hatch is not on this screen.
```

**3 · New Bot**

```
Same shell. Main column 560px on plaster.
Title: New Bot.
An 88px crest preview, then a folio picker: 4×2 shapes, a row of color dots. Bindery ring on the active shape and color.
Field: Name (placeholder “Scout”). Description (placeholder “What this machine is for”).
Stone hint: “A Bot is its own machine. It does not share files with the others.”
Primary: Create Bot. Ghost: Cancel.
No model picker, no tags, no orange.
```

**4 · Bot · Chat**

```
Same shell. Header 56px: fox crest, “Scout”, pine lamp, “Working”, tabs (Chat active, Desktop, Secrets, Rules), Stop.
Body split: left 240px chats list on cloth; thread on plaster. No hatch on this tab.

Thread:
- User: “Log into the vendor site and download last month’s invoice.”
- Tool row: browser_snapshot · running
- Assistant: “The login page is up. There’s a captcha. I need you on the desktop.”
- Composer: “Ask this Bot…”
```

**5 · Bot · Chat · Needs you**

```
Same Scout chat screen.
A 400px folio slip from the right. Plaster/folio, not a black overlay.
Header: crest, Scout, “Needs you” in carmine.
Title: Read a secret
Stone: This Bot wants the stored secret “vendor_password”. The value is not shown here.
Cloth fields: Secret → vendor_password
Stone: This run is paused until you choose.
Buttons: Allow once (bindery), Always allow this action (secondary), Deny (carmine).
Thread stays visible, slightly dimmed. No orange.
```

**6 · Bot · Desktop**

```
Same shell. Tab Desktop active.
Thread gone. The iron hatch fills the main column, 8px matte.
Top strip: crest, Scout, Desktop, pine lamp.
Chrome mid-task inside. Stone caption under the hatch, left: “Same browser the Bot uses. You can type and click.”
```

**7 · Bot · Connectors**

```
Same shell, Scout, tab Connectors.
Column 760px. “Connectors” and primary “Add connector”.
Two list rows on folio:
1) gmail — slate chip “MCP” — “3 auto · 1 ask” — last call 2h ago
2) imap_home — pine chip “Python” — “Ask every time” — commit 9f2a1c0 in mono
Hairline hover only. No card carnival.
```

**8 · New connector**

```
Same shell. Title: Add connector. Column 560px.
Segmented: MCP | Python. MCP selected.
Fields: Name, Server command (mono), key/value env rows with “Uses secret…” (values hidden).
Primary: Add connector. Ghost: Cancel.
```

**9 · Bot · Secrets**

```
Same shell, tab Secrets. Column 760px.
Title Secrets. Stone: “Handed to the Bot only after you allow it. Masked before the model sees output.”
Primary: Add secret.
Rows: vendor_password · added Apr 2 · used 12m ago; imap_password · added Jan 11 · never used.
Values are mono bullets. Rotate, Delete. No eye, no copy.
Add well: Name, Value (password), Add.
```

**10 · Bot · Rules**

```
Same shell, tab Rules. Column 760px.
Table: Connector, Action (mono), Decision, Then.
gmail / send / Ask; gmail / list / Allow; secrets / get / Ask; imap_home / * / Deny.
Decision is a select: Allow / Ask / Deny.
Stone: “Ask pauses the run and opens the slip.”
```

**11 · Admin**

```
Same shell, Admin active.
Title: Admin. Column 640px. Product settings, not a server file.
Groups: Model (select Claude Sonnet, API key as bullets); Web (Firecrawl key as bullets); Session (Idle hours before stop = 12).
No YAML, no docker host, no listen address.
Primary: Save.
```

**12 · Audit**

```
Title: Audit. Column 880px.
Table: When, Bot (crest+name), Actor, Action, Decision.
Five rows, Allow once / Always allow / Deny, Scout and Crawler.
No graphs. Action in mono.
```

### Incremental Iteration

Generate **Bots** first. If anything comes back orange, amber, or dark-warm, stop and restated: plaster page, bindery primary, carmine ribbon only. Then **Run** and **Needs you** — the hatch in the plaster wall is the test. Then 6–12.

If Stitch adds KPI cards, a warm accent, or a sky-blue hover, remove them.

### Do not

- Orange, ochre, amber, gold, rust, terracotta, “warm highlight”.
- Purple, cyan, electric green.
- Inter, Geist, Space Grotesk. Plex only.
- Glass, blur, drop shadows, gradient text.
- 3D robots, illustrated mascots. Two-dot eyes on a flat shape are the crest, not a face library.
- Showing a secret.
- A marketing landing page.

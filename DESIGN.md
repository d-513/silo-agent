---
name: Silo
description: Precision Instrument for isolated Bots. Optic mist canvas, obsidian type, signal cobalt. Each Bot is a dedicated hardware apparatus with a picked crest; its desktop is a dark optical hatch set into the console.
colors:
  surface: '#F8FAFC'
  surface-dim: '#E2E8F0'
  surface-bright: '#FFFFFF'
  surface-container-lowest: '#FFFFFF'
  surface-container-low: '#F8FAFC'
  surface-container: '#F1F5F9'
  surface-container-high: '#E2E8F0'
  surface-container-highest: '#CBD5E1'
  on-surface: '#0F172A'
  on-surface-variant: '#64748B'
  inverse-surface: '#0F172A'
  inverse-on-surface: '#F8FAFC'
  outline: '#E2E8F0'
  outline-variant: '#CBD5E1'
  surface-tint: '#1D4ED8'
  primary: '#1D4ED8'
  on-primary: '#FFFFFF'
  primary-container: '#DBEAFE'
  on-primary-container: '#1E40AF'
  inverse-primary: '#93C5FD'
  secondary: '#059669'
  on-secondary: '#FFFFFF'
  secondary-container: '#D1FAE5'
  on-secondary-container: '#065F46'
  tertiary: '#475569'
  on-tertiary: '#FFFFFF'
  tertiary-container: '#E2E8F0'
  on-tertiary-container: '#1E293B'
  error: '#DC2626'
  on-error: '#FFFFFF'
  error-container: '#FEE2E2'
  on-error-container: '#991B1B'
  primary-fixed: '#DBEAFE'
  primary-fixed-dim: '#93C5FD'
  on-primary-fixed: '#1E40AF'
  on-primary-fixed-variant: '#1D4ED8'
  secondary-fixed: '#D1FAE5'
  secondary-fixed-dim: '#6EE7B7'
  on-secondary-fixed: '#065F46'
  on-secondary-fixed-variant: '#059669'
  tertiary-fixed: '#E2E8F0'
  tertiary-fixed-dim: '#94A3B8'
  on-tertiary-fixed: '#1E293B'
  on-tertiary-fixed-variant: '#475569'
  background: '#F8FAFC'
  on-background: '#0F172A'
  surface-variant: '#F1F5F9'
typography:
  display-lg:
    fontFamily: Geist, sans-serif
    fontSize: 40px
    fontWeight: '500'
    lineHeight: 48px
    letterSpacing: -0.02em
  headline-md:
    fontFamily: Geist, sans-serif
    fontSize: 22px
    fontWeight: '500'
    lineHeight: 28px
    letterSpacing: -0.015em
  body-base:
    fontFamily: Geist, sans-serif
    fontSize: 14px
    fontWeight: '400'
    lineHeight: 22px
    letterSpacing: '0'
  body-bold:
    fontFamily: Geist, sans-serif
    fontSize: 14px
    fontWeight: '550'
    lineHeight: 22px
    letterSpacing: '0'
  label-caps:
    fontFamily: Geist, sans-serif
    fontSize: 11px
    fontWeight: '500'
    lineHeight: 16px
    letterSpacing: 0.08em
  stat-lg:
    fontFamily: Geist Mono, monospace
    fontSize: 20px
    fontWeight: '500'
    lineHeight: 28px
    letterSpacing: -0.02em
  mono:
    fontFamily: Geist Mono, monospace
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 20px
    letterSpacing: '0'
rounded:
  sm: 0.25rem
  DEFAULT: 0.375rem
  md: 0.5rem
  lg: 0.75rem
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

Desktop web app. Light mode first. A precision instrument console looking into sealed machines — not a dark ops room, not an antique paper archive.

The previous pass was a warm kiln (lamp black + ochre). That is retired. No orange, amber, gold, rust, or “safety yellow” anywhere.

Bot crests are flat colored shapes with two small dots, picked on New Bot.

## 1. Visual Theme & Atmosphere

Precision Instrument in daylight. The canvas is Optic Mist (`#F8FAFC`) — a clean, cool technical surface with zero yellowing. Typography is Obsidian (`#0F172A`) — crisp, surgical, and high-contrast. The primary structural color is **Signal Cobalt** (`#1D4ED8`), calibrated for high confidence and immediate clarity. It is for primary actions, active tabs, rail indicators, and focus.

Each Bot is a **folio apparatus**: a crest (a clean colored mark with two dots), a name, a concise status line, and a 7px status lamp. Open a bot and the live desktop appears as a **hatch** — an obsidian technical viewport (`#0B0F19`) set into the console, creating stark contrast against the clean surrounding shell.

When a Bot needs a human, a **Signal Vermilion ribbon** (`#DC2626`) marks the folio (2px left edge + lamp). Working is Precision Emerald (`#059669`), a calibrated hardware LED with a subtle pulse.

No muddy gradients, no yellow parchment, no AI-purple glows. Personality is the crest, the crisp typography, and surgical tool finishes.

## 2. Color Palette & Roles

### Primary Foundation

- **Optic Mist** (`#F8FAFC`) — app canvas and background.
- **Optic White** (`#FFFFFF`) — cards, drawers, dialogs, the approval slip.
- **Technical Slate** (`#F1F5F9`) — rail, inset wells, table headers.
- **Linen Slate** (`#E2E8F0`) — pressed / selected wells.
- **Hairline Thread** (`#E2E8F0` / `#CBD5E1`) — crisp 1px structural borders.

### Accent & Interactive

- **Signal Cobalt** (`#1D4ED8`) — primary buttons, active nav, focus rings. The signal ink.
- **Cobalt Pale** (`#DBEAFE`) — active row highlight, selection well.
- Hover deepens to `#1E40AF`.

### Typography & Text Hierarchy

- **Obsidian** (`#0F172A`) — titles, Bot names, body.
- **Cool Slate** (`#64748B`) — timestamps, hints, inactive nav, masked secrets.

### Functional States

- **Online** — **Precision Emerald** (`#059669`) lamp, solid. Worker is up.
- **Working** — Precision Emerald lamp, small pulse (1.5s, opacity 1 → 0.45).
- **Needs you** — **Signal Vermilion** (`#DC2626`) lamp + 2px vermilion indicator ribbon.
- **Stopped** — Hairline Thread lamp, Slate name.
- **Deny / error** — Signal Vermilion fill, white label.
- **MCP badge** — Slate (`#475569`) chip.
- **Python badge** — Emerald (`#059669`) chip.

Crests are a filled shape plus two small dots. Fills: mist, brown, vermilion, orange, yellow, green, emerald, cobalt, purple, pink, slate, obsidian.

## 3. Typography Rules

### Hierarchy & Weights

**Geist** for UI. **Geist Mono** for run IDs, paths, `gmail.send`, secret names, terminal, approval args, and code. Tabular numbers enabled for metrics and statistics.

Weights 400–550.

- Display 40/500, tracking -0.02em — empty state, sign-in.
- Page title 22/500, tracking -0.015em — `Bots`, Bot name in header.
- Body 14/400, 22px line height.
- Section labels 11/500, +0.08em tracking, Slate.
- Bot name on card: 16/500 Obsidian.
- Status word: 12/500, color of the lamp.

### Spacing Principles

4px baseline. Folios pad 16–20. Rail is 64px and stays 64. Run view gutters 12px so the hatch gets the pixels. Left-aligned. No centered product screens except sign-in.

## 4. Component Stylings

### Buttons

Height 36px. Radius 6px. Primary: Signal Cobalt fill, white label (`text-white`). Secondary: Optic White fill, Hairline Thread border, obsidian label. Ghost: Slate label, obsidian on hover. Deny: Signal Vermilion fill, white label — approval slip only.

Start Bot / Stop Bot and Send nest a 20px square glyph on the right (power for the machine, arrow for Send). Never a media stop-square on the header — that reads as abort-the-reply. Header Stop Bot is ghost. Press `scale(0.98)`. Focus is 2px Signal Cobalt, no glow.

No pills. Rail icons are 40px hits with tooltips.

### Folio cards (Bots)

Not dashboard tiles.

- Optic White fill, 10px radius, 1px Hairline Thread, subtle elevation (`shadow-xs`).
- Left: 56px crest (the shape, no plate).
- Name, optional one-line description in Slate, 7px lamp.
- Hover: border to `#CBD5E1`, `shadow-sm`.
- Needs you: 2px Signal Vermilion ribbon on the left edge only.

Empty archive: outline of a crest, display line “No Bots yet”, one Signal Cobalt button “New Bot”. No rockets.

### Navigation

64px rail, Technical Slate, Hairline Thread on the right. Top: Silo mark — one vertical rounded-rect + `Silo` 13/500, stacked. Then **Bots**, a scrollable stack of 28px crests (one per Bot), **+** for New Bot. Bottom: **wrench** Admin (admins only), **user** Account, **sign-out**. No names in the rail; `title` tooltips only.

Active home/Admin: Cobalt Pale well, 2px Signal Cobalt bar on the left of the icon. Active crest: same well + bar. Needs you: 2px Signal Vermilion ribbon instead of the cobalt bar, plus the lamp. Icons 20px, 1.75 stroke, obsidian. Crest lamp is 7px on the shape.

### Inputs

Optic White or Technical Slate fill, 1px Hairline Thread, 6px radius. Label above, 12/500 Slate, always visible. Focus: 1px Signal Cobalt, no glow. Error: Signal Vermilion border + 12px sentence.

Secret values never render. `••••••••` in mono. No eye. No copy. Rotate / delete only.

### Bot crests

Picked on New Bot: one of eight silhouettes (circle, blob, squircle, pill, triangle, hex, cloud, drop) and a calibrated rainbow of fills. Same two small dots on every shape. Packed as `color * 8 + shape` in the `crest` int.

Sizes: 20 / 28 / 56 / 88. The lamp is never a substitute for the crest. No generate / upload.

### The hatch (desktop)

The VNC surface is a dark window in a light room.

- Outer: Obsidian (`#0F172A`), 10px radius.
- 8px inner matte `#0B0F19`.
- Top strip 32px, obsidian: crest 20px, name, `Desktop` or `Console`, 6px lamp, spacer, status in Geist Mono (white at 80%).
- Framebuffer or the PTY fills the rest. Console uses Geist Mono on the matte.
- Worker down: one `NeedMachine` well (Files, Desktop, Console). Technical Slate, centered Slate sentence, larger Signal Cobalt Start Bot. While starting: dual cobalt/emerald ring + elapsed seconds — not a disabled “Starting…”.

### Approval slip

A 400px Optic White panel from the right, Hairline Thread on the left with elevation shadow. Clean paper, not a modal dim-to-black.

- Crest, Bot name, `Needs you` in Signal Vermilion
- Title from the security catalog (`Read a secret`), not a raw key. Each secret is its own action.
- One Slate sentence, then labeled fields on Technical Slate (Secret → `TEST`). No raw JSON.
- **Allow once** (Signal Cobalt), **Always allow this action** (secondary), **Deny** (Signal Vermilion)
- Slate: “This run is paused until you choose.”

No `confirm()`. No toast.

### Thread (chat)

The Chat tab. Not a marketing chat.

- User: Optic White well, 4px Signal Cobalt bar on the left. The product’s “bubble” — left spine, not iMessage.
- Assistant: Obsidian, no well. Markdown (headings, lists, tables, fenced code).
- Thinking: spinner + “Thinking” while streaming; collapsed “Thought” when done.
- Tool lines: left-aligned technical row **below** the text (not a centered divider). Icon + `Using Python` / `Used Python`. Collapsed by default; click to expand. Pretty body: Python shows the code as it streams, patch shows a +/− diff, terminal shows the command. No raw JSON as the primary view. Connector calls made from Python (`import tools`) sit **above** that Python row (`Used Twilio Docs · retrieve`).
- `present`: a user-facing path is the file itself in a white card well (same preview as Files). Not collapsed. A `bot/…` path is scratch — a collapsed “Looked at …” row the human can open. The model still gets image pixels. The model does not retype it.
- `look`: collapsed “Looked at screen” (same scratch fold as `present bot/…`). Clicks are 1600×900 screenshot pixels.
- Artifact: hatch card in the thread (type icon, name, muted type label, download). A skill is a scroll mark + “Skill” and **Save skill** while pending; a file is its kind icon + label (PDF, Image, Document, Spreadsheet, …) and is download-only. `artifact` is Allow — show the card, do not install. Clicking opens the hatch overlay: the skill browser for a skill, a full-fidelity file preview for a file. Skill download is a `.zip` of the whole skill directory; file download streams the bytes. Save on the card copies it to personal; after save, checkmark, no Save. No Needs-you slip for this.
- Composer: Optic White well with subtle shadow, placeholder “Ask this Bot…”, Signal Cobalt send. While a run is live, Send becomes Stop (filled square in Signal Vermilion) — `StopRun` cancels that chat’s run, not the Bot.

### Status lamp

7px circle on the crest corner (slate ring), never on the account initial. Color = state. Online is Precision Emerald, solid. Working is the only pulse.

## 5. Layout Principles

### Grid & Structure

Desktop-first, 1280 and 1440. Settings pages max 960. The Bot run view is the remaining viewport after the 64px rail — full width. Cockpit, not article.

**Shell:** rail 64 | main.

**Bots:** padding 28. Header `Bots` + `New Bot`. Grid: 3 columns at 1440, 2 at 1100, 1 below.

**Bot chat:** header 56px (crest, name, lamp, tabs, Start Bot / Stop Bot). Body: chats list 240 on Technical Slate | thread `1fr` on Optic Mist. Chat titles: generated from the first prompt; pencil or double-click to rename. The composer has a paperclip (disabled until the worker is online) that uploads to `/workspace/tmp`; the message renders attachment chips. Tabs: `Chat`, `Desktop` (caret → folio menu with only the other view; the tab itself becomes Console while that view is open), `Files`, `Connectors`, `Channels`, `Skills`, `Secrets`, `Rules`, `Container`, `Settings`. Channels lists adapter instances (logo, status lamp, adapter chip, send-only badge), Add opens an adapter picker (logo, name, description), and the form is generated from the adapter's declared fields plus Name / Enabled / “Deliver messages to the Bot” / Prompt, with the adapter's markdown **Setup instructions** in a collapsible well (open by default). Add/edit/Setup are their own routes (`/channels/new`, `/channels/new/:adapter`, `/channels/:id`, `/channels/:id/setup`) so the browser back button works. Adapters declare interactive **Setup** actions (`pick` / `run` / `qr`); a channel that still needs its target shows a primary **Set up** button with a blinking gear, otherwise a plain gear, opening the Setup page that runs those actions generically — the chat picker lives there, not in the edit form. On/off rows use the shared `Switch`. Each row has a **View log** that opens that channel's conversations in a read-only hatch overlay. Dynamic adapter state (QR, chat picker) renders in the same form. The hatch lives on `Desktop` / `Console`, full main column. Console is the same obsidian hatch with a PTY instead of VNC. Files is a workspace `FileBrowser`: tree sidebar + preview (images, PDF, media, markdown, code, docx), breadcrumbs in the header, upload / new / download. Delete is a second click, not `confirm()`. Skill inspect (Hub, Admin library, Bot Skills, Artifact) is the same tree+preview in a **hatch overlay** — obsidian chrome, white preview pane, open `SKILL.md` first, readonly. Connectors attach a copy from the Admin library or add a custom MCP (Library | Custom switch; same form as Admin). Skills: toggle switch (no On/Off labels); click the row to inspect. `catalog` chip only on embed library names. Settings is a scrolling 760px column: name, description, SOUL | MEMORY two-up with char counts, Save, then an elevated **Dangerous** well (Reset / Delete, second click).

**Settings / admin:** header + one 720–800px column. Admin sub-nav: Settings | Connectors Library | Skills Library | Search & Extract. Lists. Bot Settings: name, description, SOUL | MEMORY two-up, then Dangerous well. Folio tabs (Settings, Secrets, Rules, Container, Connectors, Channels, Skills) scroll inside the hatch; do not clip.

### Whitespace Strategy

Home can breathe. Run view is 12px tight. No page footer. No KPI row above the folios.

### Alignment

Left spine. Crests left of names. Header actions right. One ribbon in the grid at a time is enough.

### Responsive

Desktop-first operator UI. Break at **960** (`wide:` / `max-wide:`).

- **≥960:** 64px left rail. Bot header is one row: crest + name + lamp, then a horizontally scrollable tab strip (no native scrollbar; fade + caret when that edge still has tabs), then Start Bot / Stop Bot. Desktop’s caret opens one Console tab, same size and style as Desktop, flush under it (portaled so overflow does not clip). When the header is under 1280px, Start / Stop is the power glyph only. Chats stay a 240px sidebar. Approval is the 400px right slip.
- **<960:** rail becomes a 48px top bar (safe-area padded); crests scroll sideways. Bot header is tabs + Start/Stop only — no repeated name or crest (the rail mark is enough). Tabs are icons with the label only on the active one; Start / Stop is the power glyph. Desktop and Console are separate tabs, no caret. Chats become a horizontal chip strip. Files shows tree or preview, not both (crumbs go back). Approval is a bottom sheet. Settings SOUL / MEMORY stack. 40px hits.
- **Bots grid:** 1 column, 2 at 1100, 3 at 1440. Settings columns use `.silo-page` (max 760, 16px gutters on small, 28px on wide).

No separate marketing site.

## 6. Design System Notes for Stitch Generation

### Language to Use

Say: optic mist, optic white, obsidian, folio, crest, hatch, vermilion, slip, cobalt, emerald, technical slate, cool slate.

Do not say: dashboard, plaster, bindery, cloth, antique paper, kiln, ochre, warm brown, neon, vibrant, sleek, gradient hero.

Do not use orange, amber, gold, rust, or warm brown — even as a hover.

One screen per prompt. Apply this design system at the project level; do not restate hex in generation prompts.

### Color References

Optic Mist canvas. Optic White cards. Obsidian type. Cool Slate secondary. Signal Cobalt for primary and focus. Precision Emerald for working. Signal Vermilion only for “Needs you” and Deny.

### Screen map

| # | Screen | Purpose |
|---|---|---|
| 1 | Sign in | Session. Email + password. |
| 2 | Bots | Home. Every folio the user can open. |
| 3 | New Bot | Name, description, crest preview, create. |
| 13 | Bot · Settings | Name and description. More knobs later. |
| 4 | Bot · Run | Thread + dark hatch. Default. |
| 5 | Bot · Run · Needs you | Same, approval slip open. |
| 6 | Bot · Desktop | Hatch full-bleed. Caret on the tab opens Console. |
| 7 | Bot · Connectors | Attach a library preset (more than once is another account) or add a custom MCP. |
| 14 | Skills | Hub: Personal / Library. Install URL or zip on Personal. |
| 15 | Bot · Skills | Toggle library and personal skills. |
| 8 | Admin · Connectors Library | Site presets. HTTP MCP, OAuth or none. Re-add defaults. |
| 16 | Admin · Skills Library | Site skills. Install URL or zip. Re-add defaults. |
| 9 | Bot · Secrets | Named secrets. Values never shown. |
| 10 | Bot · Rules | Allow / ask / deny per action. |
| 11 | Admin · Settings | Operator YAML: nested form + editor. Env warning. |
| 12 | Audit | Who allowed what. |

### Screen prompts

Paste these one at a time. Attach the crest sheet on screens 2–6.

**1 · Sign in**

```
Desktop web, sign-in for Silo.
Full optic mist canvas (#F8FAFC). No rail.
A 400px optic white card, centered but slightly above true center, 1px hairline thread border (#E2E8F0), subtle shadow.
Silo mark: one vertical rounded rectangle and the word Silo, obsidian (#0F172A).
Title: Sign in. Two fields, labels above: Email, Password.
Primary button Signal Cobalt (#1D4ED8): Sign in.
No hero, no marketing line, no gradient, no orange.
```

**2 · Bots**

```
Desktop web, 1440 wide. 64px left rail on Technical Slate (#F1F5F9) (Silo mark, Bots active, crest stack, +, Admin, user initial at bottom).
Main Optic Mist: header “Bots” left, primary “New Bot” right.
Slate subtitle: “Machines you can open.”
A 3-column grid of white cards. Six Bots. Use the attached crests. Each has a name, one-line description, 7px lamp.

Populate:
- Owl (Mail) — Idle — “Reads the house inbox”
- Fox (Scout) — Working — “Vendor pricing and captchas” — emerald pulsing lamp
- Scarab (Crawler) — Needs you — “Walks the supplier catalog” — vermilion left ribbon
- Kettle (House) — Idle — “Kitchen orders and deliveries”
- Fish (Ledger) — Working — “Monthly reconcile” — emerald pulsing lamp
- Key (Vault) — Stopped — “Holds the spare keys”

No KPI row. No search. No orange. The hatch is not on this screen.
```

**3 · New Bot**

```
Same shell. Main column 560px on Optic Mist.
Title: New Bot.
An 88px crest preview, then a white card picker: 4×2 shapes, a row of color dots. Cobalt ring on the active shape and color.
Field: Name (placeholder “Scout”). Description (placeholder “What this machine is for”).
Slate hint: “A Bot is its own machine. It does not share files with the others.”
Primary: Create Bot (Signal Cobalt). Ghost: Cancel.
No model picker, no tags, no orange.
```

**4 · Bot · Chat**

```
Same shell. Header 56px: fox crest, “Scout”, emerald lamp, “Working”, tabs (Chat active, Desktop, Secrets, Rules), Stop Bot.
Body split: left 240px chats list on Technical Slate (titles generated from the first prompt; double-click or pencil to rename); thread on Optic Mist. No hatch on this tab.

Thread:
- User: “Log into the vendor site and download last month’s invoice.”
- Tool row: browser_snapshot · running
- Assistant: “The login page is up. There’s a captcha. I need you on the desktop.”
- Composer: “Ask this Bot…”. Left cluster: Attach, a model picker (allowed models; sets the chat override), and a cache chip after usage events.
```

**5 · Bot · Chat · Needs you**

```
Same Scout chat screen.
A 400px white slip from the right with elevation shadow. Clean paper, not a black overlay.
Header: crest, Scout, “Needs you” in Signal Vermilion.
Title: Read a secret
Slate: This Bot wants the stored secret “vendor_password”. The value is not shown here.
Technical Slate fields: Secret → vendor_password
Slate: This run is paused until you choose.
Buttons: Allow once (Signal Cobalt), Always allow this action (secondary), Deny (Signal Vermilion).
Thread stays visible, slightly dimmed. No orange.
```

**6 · Bot · Desktop**

```
Same shell. Tab Desktop active.
Thread gone. The obsidian hatch fills the main column, 8px matte (#0B0F19).
Top strip: crest, Scout, Desktop, emerald lamp.
Chrome mid-task inside. Slate caption under the hatch, left: “Same browser the Bot uses. You can type and click.”
```

**7 · Bot · Connectors**

```
Same shell, Scout, tab Connectors.
Column 760px. “Connectors” and primary “Add connector”. Add opens Library | Custom. Each row has ghost Edit, Refresh (re-list tools) and Remove.
Two list rows on white cards:
1) gmail — slate chip “MCP” — “3 auto · 1 ask” — last call 2h ago
2) imap_home — emerald chip “Python” — “Ask every time” — commit 9f2a1c0 in mono
Hairline hover only. No card carnival.
```

**8 · New connector**

```
Same shell. Title: Add preset (Admin library) or Add connector (Bot, Custom). Column 560px.
Shared form. Fields: Name, URL (mono), Auth None|OAuth, optional OAuth Client ID/Secret (servers without DCR), Default Allow|Ask|Deny, extra headers (values hidden).
Bot add also has segmented Library | Custom. Library is a picker; clicking a preset shows mark + large name, then a cobalt-edged technical well with the catalog `guide` (obsidian, not a field). Settings sit in collapsed Advanced settings.
Primary: Add to library / Add connector. Ghost: Cancel. Back returns to the picker.
```

**9 · Bot · Secrets**

```
Same shell, tab Secrets. Column 760px.
Title Secrets. Slate: “Handed to the Bot only after you allow it. Masked before the model sees output.”
Primary: Add secret.
Rows: vendor_password · added Apr 2 · used 12m ago; imap_password · added Jan 11 · never used.
Values are mono bullets. Rotate, Delete. No eye, no copy.
Add well: Name, Value (password), Add.
```

**10 · Bot · Rules**

```
Same shell, tab Rules. Column 760px.
Intro: Allow runs without asking. Ask pauses and opens the slip (Allow once / Always / Deny). Deny refuses. Always keeps only that action or secret.
White card sections, not one table. Each section is a collapsible card (caret); This Bot starts open. This Bot: Python, Terminal, Files, Desktop, Soul, Memory (segmented Allow / Ask / Deny). Secrets: one row per stored secret (empty: add them on Secrets). Then one section per attached connector; actions from the tool list; unset uses the connector default. Authorize-needed connectors say so instead of inventing rows.
No ghost rows after detach or deleting a secret.
```

**11 · Admin**

```
Same shell, Admin active.
Title: Admin. Column 640px. Operator config: silo.yaml.
Groups match YAML: Models, Providers, Search, Server, Bootstrap. Models holds the allowlist plus Default and Title pickers; Providers holds one block per provider (key, base URL, cache toggle). Each field shows source (default / yaml / env). Env-set fields are disabled with a vermilion warning.
YAML editor below (syntax highlighted). Save writes silo.yaml only.
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

Generate **Bots** first. If anything comes back orange, amber, or dark-warm, stop and restate: optic mist page, cobalt primary, vermilion ribbon only. Then **Run** and **Needs you** — the optical hatch in the mist console is the test. Then 6–12.

If Stitch adds KPI cards, a warm accent, or a sky-blue hover, remove them.

### Do not

- Orange, ochre, amber, gold, rust, terracotta, “warm highlight”.
- Purple, cyan, electric green.
- IBM Plex, Comic Sans, decorative serifs. Geist and Geist Mono only.
- Glass, blur, drop shadows, gradient text.
- 3D robots, illustrated mascots. Two-dot eyes on a flat shape are the crest, not a face library.
- Showing a secret.
- A marketing landing page.

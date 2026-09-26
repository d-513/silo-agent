---
name: Silo
description: A calm console for isolated Bots. Paper canvas, ink type, one quiet cobalt for "you are here". Each Bot is its own machine with a picked crest; its desktop is a dark hatch set into a light room. Motion only ever answers something the operator did.
colors:
  canvas: "#FAF9F7"
  surface: "#FFFFFF"
  well: "#F3F2EF"
  pressed: "#EAE8E4"
  line: "rgba(26, 25, 23, 0.09)"
  line-strong: "rgba(26, 25, 23, 0.16)"
  ink: "#1A1917"
  ink-2: "#55534E"
  ink-3: "#6B6862"
  cobalt: "#2B4FC7"
  cobalt-deep: "#2340A8"
  cobalt-pale: "#E8EDFB"
  emerald: "#0F7A55"
  lamp: "#16A06F"
  vermilion: "#C4372B"
  vermilion-deep: "#A82E24"
  vermilion-pale: "#FBEAE8"
  hatch: "#141413"
  matte: "#0E0E0D"
typography:
  display:
    fontFamily: Geist, sans-serif
    fontSize: 40px
    fontWeight: "500"
    lineHeight: 48px
    letterSpacing: -0.02em
  title:
    fontFamily: Geist, sans-serif
    fontSize: 22px
    fontWeight: "500"
    lineHeight: 28px
    letterSpacing: -0.015em
  card-title:
    fontFamily: Geist, sans-serif
    fontSize: 15px
    fontWeight: "600"
    lineHeight: 20px
    letterSpacing: -0.01em
  body:
    fontFamily: Geist, sans-serif
    fontSize: 14px
    fontWeight: "400"
    lineHeight: 22px
  body-lg:
    fontFamily: Geist, sans-serif
    fontSize: 15px
    fontWeight: "400"
    lineHeight: 24px
  reply:
    fontFamily: Source Serif 4, Georgia, serif
    fontSize: 16.5px
    fontWeight: "400"
    lineHeight: 28px
    letterSpacing: -0.003em
  meta:
    fontFamily: Geist, sans-serif
    fontSize: 12.5px
    fontWeight: "400"
    lineHeight: 18px
  label:
    fontFamily: Geist, sans-serif
    fontSize: 12px
    fontWeight: "500"
    lineHeight: 16px
  label-caps:
    fontFamily: Geist, sans-serif
    fontSize: 11px
    fontWeight: "500"
    lineHeight: 16px
    letterSpacing: 0.08em
  mono:
    fontFamily: Geist Mono, monospace
    fontSize: 12.5px
    fontWeight: "400"
    lineHeight: 20px
rounded:
  xs: 6px
  sm: 8px
  control: 10px
  card: 14px
  panel: 16px
  bubble: 18px
  full: 9999px
spacing:
  unit: 4px
  xs: 4px
  sm: 8px
  run: 12px
  md: 16px
  gutter: 20px
  lg: 24px
  page: 28px
  xl: 32px
motion:
  ease-quiet: cubic-bezier(0.32, 0.72, 0, 1)
  ease-settle: cubic-bezier(0.22, 1, 0.36, 1)
  press: 70ms
  hover: 160ms
  state: 200ms-280ms
  fold: 320ms
  arrival: 420ms
  breathe: 2.4s
---

# Design System: Silo

Desktop-first web app. Light only. A calm, precise console looking into sealed machines. It should feel like a well-made instrument: steady, quiet, and exact. It shouldn't look like an ops dashboard or a marketing site.

**What makes it feel stable:** most of the screen doesn't move and doesn't call for attention. There's one ink color, a paper ground, and accents that show up only when they mean something. **What makes it feel good to use:** every action gets a small, immediate, physical answer, and every decision leaves a trace.

## 1. Principles

1. **One ink.** Text, primary buttons and icons share `ink`. The UI is mostly black and white on paper.
2. **Color is a signal, not a decoration.** `cobalt` means "you are here / you can type here". `emerald` means the machine is alive. `vermilion` means it needs you. If none of those apply, don't use color.
3. **Separate by tone before lines.** Areas sit on `canvas`, `well` or `surface`. Hairlines are translucent ink and used sparingly.
4. **Motion answers an action.** Nothing moves by itself except a Working lamp, and that lamp breathes rather than blinks.
5. **Morph, don't swap.** Send becomes Stop in the same place, and copy becomes a check. Nothing jumps or reflows.
6. **Every decision leaves a receipt** in the thread. No toasts, no `confirm()`.
7. **The Bot's words look different from the interface.** Replies are set in a serif, and everything else is Geist.

## 2. Color

### Neutrals

| Token         | Hex                  | Role                                                                                            |
| ------------- | -------------------- | ----------------------------------------------------------------------------------------------- |
| `canvas`      | `#FAF9F7`            | App background, thread background, header.                                                      |
| `surface`     | `#FFFFFF`            | Cards, the approval slip, the composer, menus, inputs, selected chat row.                       |
| `well`        | `#F3F2EF`            | Rail, chats list, user speech bubble, inset field groups, code blocks, hover on quiet controls. |
| `pressed`     | `#EAE8E4`            | Hover on rail items and chat rows, disabled Send, spinner track.                                |
| `line`        | `rgba(26,25,23,.09)` | Default hairline: card rings, header bottom edge, row dividers.                                 |
| `line-strong` | `rgba(26,25,23,.16)` | Secondary button ring, input ring on focus-within, dividers inside wells.                       |

Borders are drawn as `box-shadow: 0 0 0 1px var(--line)` (or `inset`) instead of `border`, so they never change layout.

### Text

| Token   | Hex       | On canvas             | Use                                                              |
| ------- | --------- | --------------------- | ---------------------------------------------------------------- |
| `ink`   | `#1A1917` | 16.7:1                | Titles, body, Bot names, primary button fill, icons when active. |
| `ink-2` | `#55534E` | 7.3:1                 | Secondary sentences, descriptions, inactive icons, tool labels.  |
| `ink-3` | `#6B6862` | 5.3:1 (5.0 on `well`) | Timestamps, hints, placeholders, field labels, section labels.   |

Never use opacity to make grey text. Pick a level.

### Accents

| Token            | Hex       | Use                                                                                                                                            |
| ---------------- | --------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `cobalt`         | `#2B4FC7` | Focus ring (2px, 2px offset), links, text caret, active-tab underline, active rail bar, selected crest in the picker. **Never a button fill.** |
| `cobalt-deep`    | `#2340A8` | Link hover.                                                                                                                                    |
| `cobalt-pale`    | `#E8EDFB` | Text selection, selected picker cell.                                                                                                          |
| `emerald`        | `#0F7A55` | Online / Working status word, done check on tool rows, allow receipts. Passes as small text (5.1:1).                                           |
| `lamp`           | `#16A06F` | Fill of the Online and Working lamp dots. Fill only, never text.                                                                               |
| `vermilion`      | `#C4372B` | Needs you (lamp, status word, 2px folio ribbon), Deny text, Stop-reply button fill, destructive armed state, errors. White on it is 5.4:1.     |
| `vermilion-deep` | `#A82E24` | Hover on vermilion fills.                                                                                                                      |
| `vermilion-pale` | `#FBEAE8` | Hover behind Deny, PDF file badge, error field background.                                                                                     |

The accents never appear as gradients, glows or large fields. Orange, amber, gold, purple and cyan never appear in the UI. The crest fills below are the only exception.

### The hatch

The live desktop and console are a dark window in a light room: `hatch` (`#141413`) frame, `matte` (`#0E0E0D`) inset 8px, status text white at 80% in Geist Mono.

### Crest fills

The order is fixed because it's packed into the `crest` int as `color * 8 + shape`. Never reorder these, only restyle them.

| #   | Name     | Fill                                    | Eyes     |
| --- | -------- | --------------------------------------- | -------- |
| 0   | mist     | `#F3F2EF` + 1.4px `line-strong` outline | `ink`    |
| 1   | brown    | `#8B5A3C`                               | `canvas` |
| 2   | wine     | `#9F1239`                               | `canvas` |
| 3   | orange   | `#EA580C`                               | `canvas` |
| 4   | yellow   | `#E0AE1C`                               | `ink`    |
| 5   | green    | `#16A34A`                               | `canvas` |
| 6   | emerald  | `#0F7A55`                               | `canvas` |
| 7   | cobalt   | `#2B4FC7`                               | `canvas` |
| 8   | purple   | `#6D28D9`                               | `canvas` |
| 9   | pink     | `#DB2777`                               | `canvas` |
| 10  | graphite | `#55534E`                               | `canvas` |
| 11  | ink      | `#1A1917`                               | `canvas` |

Wine is deliberately darker than `vermilion`, so a red Bot never reads as Needs you.

## 3. Typography

- **Geist** is used for all interface text. Weights are 400, 500 and 600, never lighter.
- **Source Serif 4** is used only for assistant replies in the thread (`reply`, 16.5/28, optical sizing on). It separates the machine's voice from the chrome and makes long answers calmer to read. Markdown headings inside a reply stay in the serif at 500.
- **Geist Mono** is for machine facts: run IDs, paths, tool actions (`secrets.read`), secret names, approval args, terminal and code. It's never used for prose.
- Tabular numbers for metrics and timestamps in tables.
- Scale:
  - `display`: empty states and sign-in only.
  - `title`: page titles, the slip title.
  - `card-title`: the Bot name in the header and on folios.
  - `body-lg`: user bubbles and the composer.
  - `body`: everything else.
  - `meta`: descriptions and hints.
  - `label`: field labels and status words.
  - `label-caps`: section labels in `ink-3`.
- Sentence case everywhere. **Bot** is always capitalized.

## 4. Shape, space, elevation

- 4px baseline. Folios and panels pad `md`/`gutter`, pages pad `page` on wide screens and `md` on narrow ones, and the run view uses `run` so the hatch gets the pixels.
- Radii:
  - `xs` (6px): glyph wells inside buttons, kbd hints.
  - `sm` (8px): tabs, mini icon buttons, chips, code blocks.
  - `control` (10px): buttons, inputs, rail items, chat rows.
  - `card` (14px): folios, file cards, tool rows when open.
  - `panel` (16px): the approval slip, large cards.
  - The composer is 18px.
  - User bubble: `bubble` with a 6px bottom-right corner.
  - `full`: switches, lamps, avatars.
  - No pill buttons.
- Elevation:
  - `shadow-card`: `0 0 0 1px var(--line)` only. Resting cards are flat.
  - `shadow-float`: `0 0 0 1px var(--line), 0 1px 2px rgba(20,18,14,.04), 0 6px 20px -8px rgba(20,18,14,.08)`. Used for the composer, and for folio hover.
  - `shadow-float-focus`: `0 0 0 1px var(--line-strong), 0 1px 2px rgba(20,18,14,.04), 0 10px 30px -10px rgba(20,18,14,.16)`. Used for the composer with focus inside.
  - `shadow-slip`: `0 0 0 1px var(--line), 0 24px 60px -20px rgba(20,18,14,.22), 0 2px 8px rgba(20,18,14,.05)`. Used for the approval slip and menus.
  - No blur, no glass, no colored shadows.

## 5. Motion

Only two curves:

- `ease-quiet` `cubic-bezier(.32,.72,0,1)` for state changes.
- `ease-settle` `cubic-bezier(.22,1,.36,1)` for things arriving.

No bounce, and no overshoot beyond the settle curve.

| Kind         | Duration  | Examples                                                                                                    |
| ------------ | --------- | ----------------------------------------------------------------------------------------------------------- |
| Press down   | 70ms      | `:active { transform: scale(.97) }` on every button; Send uses `.94`.                                       |
| Hover        | 160ms     | Background, color and ring changes.                                                                         |
| State change | 200–280ms | Icon morphs, status word crossfade, tab underline slide, switch knob.                                       |
| Fold         | 320ms     | Tool rows and "Thought" rows opening (`grid-template-rows: 0fr → 1fr`, contents fade in with a 60ms delay). |
| Arrival      | 420ms     | New thread items rise 8px and fade in.                                                                      |
| Glide        | 520ms     | A just-sent user message rises about 56px from the composer while scaling from .96 to 1.                    |
| Breathe      | 2.4s loop | Working lamp opacity 1 → .4 → 1. This is the only ambient loop.                                             |

`prefers-reduced-motion: reduce`: remove every transform and loop. Keep crossfades at 120ms or less, and make the lamp static.

## 6. Components

### Buttons

- **Primary**: `ink` fill, white label, 36px tall (40px in the slip), `control` radius. Hover darkens to `#000`.
- **Secondary**: `surface` with an inset `line-strong` ring and `ink` label. Hover fills `well`.
- **Ghost**: `ink-2` label, no ring. Hover fills `well` and turns the label `ink`.
- **Deny**: `vermilion` label, no fill. Hover fills `vermilion-pale`. It's only used on the approval slip. A destructive confirm uses the armed pattern (below).
- **Glyph**: Start Bot / Stop Bot put a 22px glyph well (`xs` radius, `well` fill, or white at 20% on ink) on the right of the label, with a power glyph for the machine. Send is its own component.
- Focus is always a 2px `cobalt` outline at a 2px offset, never a glow.

### Send / Stop (composer)

- It's a 34px square with 11px radius.
  - Empty draft: `pressed` fill, `ink-3` arrow.
  - Draft present: `ink` fill, white arrow.
  - Run in progress: `vermilion` fill, white 10px rounded square.
- The change is a **morph**: the arrow lifts out (translateY −10px, scale .6, fading) while the square rotates in from −90°. It takes 320ms `ease-settle`, and the fill crossfades over 240ms.
- Enter sends, Shift+Enter adds a new line, and Escape stops the reply while it's running. The hint text beside the button switches between "Enter to send" and "Esc stops the reply".
- Stop cancels **this chat's run**, not the Bot, and leaves a "Stopped · this run" receipt.

### Composer

- A `surface` card with 18px radius and `shadow-float`, switching to `shadow-float-focus` when focus is inside.
- The textarea auto-grows (`field-sizing: content`, 26–168px) in `body-lg`. The placeholder is "Ask {Bot name}…".
- The bottom row holds the paperclip (disabled until the worker is online, uploads to `/workspace/tmp`), the model chip, a spacer, the hint and Send.

### Thread

- The column is 720px max, centered on `canvas`, with 16px between items; consecutive thinking / tool / receipt rows stack 2px apart. A centered `meta` date divider ("Today · 09:12") sits at the top.
- **User**: right-aligned `well` bubble in `body-lg`. There's no border.
- **Assistant**: no container, `reply` serif. While streaming, text arrives in chunks of a few words, each fading from opacity 0 with 3px blur to clear over 360ms, and a small breathing `ink` dot sits at the end. When it's done, hovering the turn reveals the action row (copy, retry), which fades up 2px over 180ms.
- **Thinking**: a lightbulb, then the word "Thinking" with a moving highlight shimmer (text gradient `ink-3 → ink → ink-3`, 1.6s linear). The reasoning streams open beneath it; when it's done it folds shut into "Thought for Ns" with a chevron.
- **Tool row**: a 32px row with a 16px state slot, a 16px `ink-2` tool icon (the Python mark, a terminal, a file glyph, a globe for web search; a connector call shows that connector's own image, or a plug when it has none), then the verb and app (`ink`, 500: "Using Browser" / "Used Browser"), then the action in `mono` `ink-3`, then a chevron.
  - The state slot crossfades between spinner (running), `emerald` check (done), breathing `vermilion` dot plus "Waiting for you" (paused for approval) and an `ink-3` ✕ (skipped or stopped). Each icon scales in from .6 over 300ms `ease-settle`.
  - While it runs it is open and streams live: the body is capped at 220px, pinned to its newest lines, with the top fading out. When it finishes it folds shut (unless the reader toggled it). Replayed history starts collapsed. When open, the row becomes a `surface` card and shows the pretty body in a `well` mono block: Python shows code, patch shows a diff, terminal shows the command. The primary view is never raw JSON.
  - Connector calls made from Python (`import tools`) sit above that Python row.
- **Receipt**: a one-line record of a decision. It has a shield icon (`emerald` check for allowed, `ink-3` ✕ for denied or stopped), the decision in `ink` 500, then "· action · target" in `ink-3`, a hairline filling the rest of the line, and "by you". Examples: "**Allowed once** · Read a secret · vendor_password". It rises in like any other item.
- **File / artifact card**: a 380px `surface` card with a `line` ring. It has a type badge (PDF: `vermilion-pale` with a `vermilion` label), the name in 500, "PDF · 184 KB · saved to Files" in `meta`, and a download icon button. On hover it lifts 1px and gains `shadow-float`. Skills show a scroll mark, "Skill" and **Save skill** while pending, and a check after saving.
- `present` of a user-facing path shows the file itself in a card. A `bot/…` path and `look` screenshots are collapsed "Looked at …" rows.
- New items auto-scroll the thread smoothly to the bottom.

### Approval slip

- A 400px `surface` panel floating 12px inside the right edge, with `panel` radius and `shadow-slip`. It slides in from 28px right over 420ms `ease-settle`, and leaves by sliding 20px right while fading over 260ms `ease-quiet`. The area behind it gets a `canvas` wash at 55% opacity (never black). Below 960px it becomes a bottom sheet.
- Contents from top to bottom:
  - Crest, Bot name, and "Needs you" with a `vermilion` lamp.
  - A `title` taken from the security catalog ("Read a secret"), never a raw key.
  - One `ink-2` sentence.
  - A `well` group of labelled rows (Secret → `vendor_password` in mono, Used for → host, Last used).
  - Pinned to the bottom: **Allow once** (primary ink), **Always allow this action** (secondary), **Deny** (vermilion text).
  - The footer line: breathing `vermilion` dot + "This run is paused until you choose."
- Secret values never render.
- Choosing: the slip leaves, a receipt lands in the thread, the tool row's state changes, and the lamp crossfades back to Working.

### Status lamp

- A 7px dot, or 9px when it sits on a crest corner with a 2px ring in the ground color. States:
  - Online: `lamp`, solid.
  - Working: `lamp`, breathing.
  - Needs you: `vermilion`.
  - Starting: a hollow 1.5px `cobalt` ring.
  - Stopped: a hollow 1.5px `ink-3` ring.
- Color changes crossfade over 320ms. When the state becomes **Needs you**, the lamp fires **one** ping: a `vermilion` circle scaling ×4.2 while fading from 55% to 0 over 900ms. It never loops.
- The status word next to the lamp crossfades too: old word out, new word up 4px, 240ms. Use `emerald` for Online and Working, `vermilion` for Needs you, `ink-3` for Stopped.
- A lamp is never the only signal. It always sits next to a word or the ribbon.

### Crests

- Eight silhouettes (circle, blob, squircle, pill, triangle, hex, cloud, drop) filled with one of the twelve fills, plus two dots. Sizes are 20 / 28 / 56 / 88.
- **Blink:** on hover, the eyes squash to `scaleY(.12)` and back over 220ms (`transform-box: fill-box; transform-origin: center`). Working Bots in the rail blink by themselves about every 6s. That's the only personality animation in the product.
- Picker: selected cell `cobalt-pale` with an inset 1px `cobalt` ring; selected color dot `0 0 0 2px surface, 0 0 0 4px cobalt`.

### Tabs (Bot header)

- Tabs are 32px tall, 13px/500 in `ink-3`, with a 15px icon. Hovering fills `well` and turns the text `ink`. The active tab is `ink`.
- A single 2px `cobalt` underline **slides** to the active tab (transform + width over 280ms `ease-quiet`). It doesn't re-render per tab.

### Rail

- 64px wide on `well`, with no right border. From the top:
  - The Silo mark (`ink`, 26px) over "Silo" at 11/600.
  - Bots.
  - A 24px hairline.
  - Crests, 28px each in 40px hit areas with `control` radius.
  - `+` for New Bot.
  - Spacer.
  - Admin (wrench, admins only), then the account avatar (an `ink` circle with a white initial).
- The active item is a `surface` well with a `line` ring and a 3px `cobalt` bar on the left edge. Hover fills `pressed`. There are no labels, only `title` tooltips.

### Chats list

- 248px wide on `well`, headed by a `label-caps` "Chats" and a new-chat icon button.
- Rows are 2 lines: title in 13.5/500 `ink`, meta in `ink-3` ("Just now", "Working…", "Waiting for you", "Yesterday"). A live run shows its lamp after the title.
- The active row is lifted: `surface` fill with a `line` ring. Hover fills `pressed`.
- Titles are generated from the first prompt. Rename with the pencil or a double-click.

### Inputs

- 36px, `surface`, with an inset 1px `line-strong` ring and `control` radius. Label above in `label` `ink-3`, always visible.
- Focus: a 1px `cobalt` ring plus a 2px `cobalt` outline at a 2px offset. Error: a `vermilion` ring plus a 12px `vermilion` sentence below.
- Placeholders use `ink-3` at full strength.
- Secrets render as `••••••••` in mono, with no eye and no copy button, only Rotate or Delete.

### Switch

- The track is 44×26: `pressed` with an inset `line` ring when off, `ink` when on. The knob is 20px white with a hairline ring and a small shadow.
- **While pressed, the knob stretches to 26px** toward where it's going. It slides over 260ms `ease-quiet`.
- There are no On/Off labels. Clicking the switch doesn't trigger the row's own click.

### Feedback patterns

- **Copy:** the icon morphs into an `emerald` check (rotate ±30°, scale .5 → 1) for 1.5s, then morphs back.
- **Save:** the button reads Saving… with an inline spinner, then Saved with a check that pops in (scale .6 → 1 with `ease-settle`), then after about 1.7s quietly reads Save again. The button itself is the feedback.
- **Destructive (Delete Bot, Reset, remove connector):**
  - The first click **arms** the button: it gets a `vermilion` fill and the label "Click again to delete", with a 3px white bar along the bottom that drains over 3s. When the bar runs out, the button disarms.
  - The second click performs the action and briefly shows "Deleted · Undo" where undo is possible.
- **Loading lists:** skeleton rows in `well` with a slow shimmer, not spinners. Reserve space so nothing shifts when content arrives.

### Folio cards (Bots home)

- `surface` with a `line` ring and `card` radius, padded 16–20. A 56px crest on the left, then the name (`card-title`), a one-line description (`meta`, `ink-2`) and the lamp with its status word.
- Hover: `shadow-float`, a 1px lift and a crest blink. Press: scale .995.
- Needs you: a 2px `vermilion` ribbon down the left edge only.
- Empty state: "No Bots yet" in `display`, with a single primary **New Bot**.

## 7. Layout

- **Shell:** the rail (64px) plus main. Break at **960** (`wide:` / `max-wide:`). Below 960, the rail becomes a 48px top bar (safe-area padded) and crests scroll sideways.
- **Bot view:**
  - A 56px header on `canvas` with a `line` bottom edge: crest 28 + name + lamp/status, the tab strip, a spacer, then Stop Bot / Start Bot (ghost).
  - The body is chats (248, `well`) next to the thread on `canvas`.
  - Desktop and Console give the whole main column to the hatch. Desktop's caret opens Console.
  - Under 1280px the header's Start/Stop shows only the power glyph.
- **Tabs:** `Chat`, `Desktop`, `Files`, `Connectors`, `Channels`, `Skills`, `Secrets`, `Rules`, `Container`, `Settings`. The strip scrolls horizontally with edge fades. Add, edit and setup flows are their own routes, so the browser Back button works.
- **Settings / admin:** one column, 760px max (forms 560px), in `card`-radius panels. The Dangerous panel comes last, with armed destructive buttons.
- **Bots home:** padded 28. One column below 1100px, 2 up to 1440px, 3 above that. No KPI row, no footer, no centered screens except sign-in.
- **Below 960:**
  - Tabs become icons, with a label only on the active tab.
  - Chats become a horizontal chip strip.
  - Files shows either the tree or the preview, not both.
  - The approval slip becomes a bottom sheet.
  - Hit areas are 40px.

## 8. Voice

- Address the operator as _you_ and the machine as _the Bot_ or its name. Keep sentences short and declarative: "This run is paused until you choose." "Handed to the Bot only after you allow it."
- States are plain words: **Needs you**, **Working**, **Online**, **Stopped**.
- Titles come from the security catalog ("Read a secret").
- No emoji, exclamation marks or marketing lines.
- Vocabulary: _folio_, _crest_, _hatch_, _slip_, _rail_, _receipt_.

## 9. Do not

- Use blue button fills, gradients, glows, glass or blur.
- Use orange, amber, purple or cyan anywhere except crest fills.
- Put a border on every box. Use tone first.
- Use spinners where a skeleton or an in-place morph works.
- Add toasts, `confirm()` or modal dim-to-black.
- Loop any animation other than the Working lamp's breathing.
- Use IBM Plex, Inter or decorative display faces. Geist, Geist Mono and Source Serif 4 (replies only) are the only fonts.
- Show a secret value.
- Add illustrations, mascots or 3D robots. The crest is the character.

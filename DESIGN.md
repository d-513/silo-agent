---
name: Silo
description: A calm console for isolated Bots. Paper canvas, ink type, one quiet cobalt for "you are here". Each Bot is its own machine with a picked crest; its desktop is a dark hatch set into the room. Light and dark share one set of names. Motion only ever answers something the operator did.
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

Desktop-first web app, light and dark (§2, Dark). A calm, precise console looking into sealed machines. It should feel like a well-made instrument: steady, quiet, and exact. It shouldn't look like an ops dashboard or a marketing site.

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
| `emerald`        | `#0F7A55` | Online / Working status word, allow receipts. Passes as small text (5.1:1).                                           |
| `lamp`           | `#16A06F` | Fill of the Online and Working lamp dots. Fill only, never text.                                                                               |
| `vermilion`      | `#C4372B` | Needs you (lamp, status word, 2px folio ribbon), Deny text, Stop-reply button fill, destructive armed state, errors. White on it is 5.4:1.     |
| `vermilion-deep` | `#A82E24` | Hover on vermilion fills.                                                                                                                      |
| `vermilion-pale` | `#FBEAE8` | Hover behind Deny, PDF file badge, error field background.                                                                                     |

The accents never appear as gradients, glows or large fields. Orange, amber, gold, purple and cyan never appear in the UI. The crest fills below are the only exception.

### The hatch

The live desktop and console are a dark window in a light room: `hatch` (`#141413`) frame, `matte` (`#0E0E0D`) inset 8px, status text white at 80% in Geist Mono. Text set on it is `on-hatch` (`#FAF9F7`), never `ink` or `canvas`, because the hatch is dark in both themes. The Desktop hatch's bar carries one more control before the pane's own: **Pause Bot**, a mono pill outlined in `on-hatch` at 18% with a hand glyph. On, it fills `vermilion` ("You're in control · Resume", a play glyph) and the matte takes a 2px `vermilion` edge, so a glance says the screen is the owner's; a Tip says what the Bot can and cannot do meanwhile.

### Dark

Dark is the same system re-inked, not a second design: every component names the same tokens, and `[data-theme="dark"]` in `index.css` gives them other values. Nothing in a component may say `white`, `black` or a hex; a color that only works on paper is a bug in the dark.

| Token                             | Light                 | Dark                    | Note                                                              |
| --------------------------------- | --------------------- | ----------------------- | ----------------------------------------------------------------- |
| `canvas`                          | `#FAF9F7`             | `#171615`               | A warm near-black, not blue-black.                                |
| `well`                            | `#F3F2EF`             | `#1E1D1B`               |                                                                   |
| `surface`                         | `#FFFFFF`             | `#252422`               |                                                                   |
| `pressed`                         | `#EAE8E4`             | `#31302D`               |                                                                   |
| `line` / `line-strong`            | ink at 9% / 16%       | paper at 10% / 18%      |                                                                   |
| `ink` / `ink-2` / `ink-3`         | `#1A1917` … `#6B6862` | `#EDEBE6` `#B5B2AB` `#9B9891` | 15:1, 8.5:1 and 6:1 on the dark canvas.                     |
| `ink-deep` / `on-ink`             | `#000` / `#FFF`       | `#FFF` / `#171615`      | Hover of an ink fill, and the label on it.                        |
| `cobalt` / `-deep` / `-pale`      | `#2B4FC7` …           | `#8AA4FF` `#ADC0FF` `#283258` | Lifted until it reads as a word; hover goes lighter.        |
| `emerald` / `lamp`                | `#0F7A55` / `#16A06F` | `#4FC38D` / `#2FB880`   |                                                                   |
| `vermilion` / `-deep` / `-pale`   | `#C4372B` …           | `#F27565` `#F79284` `#3D2320` |                                                             |
| `on-accent`                       | `#FFF`                | `#171615`               | The label on a cobalt, emerald or vermilion fill.                 |
| `hatch` / `matte`                 | `#141413` / `#0E0E0D` | `#0E0E0D` / `#080808`   | Still the darkest thing in the room.                              |

- **Raised is lighter.** On paper the order is `surface` over `canvas` over `well` over `pressed`. In the dark it is `canvas`, `well`, `surface`, `pressed` from deepest to lightest, so a card still lifts, a hover still answers, and a code block inside a card still sits in.
- **Fills flip their label.** The lifted accents are too light for white text, so a primary button is a paper fill with a dark label, and Stop, an armed delete and the Feed count carry `on-accent`.
- **Shade is black and heavier** (`--shade-*`), but the ring does the work: a dark card is seated by its `line`, not its shadow.
- **Crests do not change.** A crest is the Bot's face: fills and eyes are the same in both themes. The ink crest takes a faint edge in the dark, as mist takes one on paper.
- **Brand logos keep a paper tile** (`mark`, with a 2px halo in the dark): they are drawn for a light page, and a black mark must not sink into the ground.
- A QR code stays black on white.
- **Choosing:** the rail's sun/moon item, just above the account avatar. Until the reader picks, the app follows the system setting and keeps following it; the pick is remembered in the browser. The glyph morphs (the moon turns out as the sun turns in, 320ms `ease-settle`) and shows where a press leads; the palette itself changes in one frame, with no fade.
- `theme_test.ts` holds both palettes to their contrast floors (4.5:1 for every word color on `canvas`, `well` and `surface`; the same for a label on its fill).

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

- **Primary**: `ink` fill, `on-ink` label, 36px tall (40px in the slip), `control` radius. Hover deepens to `ink-deep`.
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
- Enter sends, Shift+Enter adds a new line, and Escape stops the reply while it's running. The button's own title says which ("Enter to send" / "Esc stops the reply"); no hint text sits beside it.
- Stop cancels **this chat's run**, not the Bot, and leaves a "Stopped · this run" receipt.

### Composer

- A `surface` card with 18px radius and `shadow-float`, switching to `shadow-float-focus` when focus is inside.
- The textarea auto-grows (`field-sizing: content`, 26–168px) in `body-lg`. The placeholder is "Ask {Bot name}…".
- The bottom row is three groups, never one strip of icons:
  - **What goes in:** the paperclip (disabled until the worker is online, uploads to `/workspace/tmp`) and the dictation mic.
  - **Who answers:** the model chip (the model's own name only, `claude-haiku-4.5`; the menu keeps the provider) and the thinking bulb.
  - On the right, **the conversation:** the context ring (mono % beside it from 400px of composer), Talk, and Send. The ring opens a `shadow-slip` panel anchored to its right edge: the context window (bar, used, limit, cache, the auto-summary sentence), then rows for **Compact now**, **Save memories** (its result on the right, the saved facts under it; while it reads, a breathing `cobalt` dot sits on the ring, and a `+N` badge pops there when it lands) and **Follow live steps** (a small ink switch). Anything the conversation needs that is not about this message goes into that panel, not onto the row.

### Thread

- The column is 720px max, centered on `canvas`, with 16px between items; consecutive thinking / tool / receipt rows stack 2px apart. A centered `meta` date divider ("Today · 09:12") sits at the top.
- **User**: right-aligned `well` bubble in `body-lg`. There's no border. In an automation log the user item is instead the hairline **RunMark** divider from the Automations section.
- **Assistant**: no container, `reply` serif. While streaming, text arrives in chunks of a few words, each fading from opacity 0 with 3px blur to clear over 360ms, and a small breathing `ink` dot sits at the end. When it's done, hovering the turn reveals the action row (copy, retry), which fades up 2px over 180ms.
- **Thinking**: a lightbulb, then the word "Thinking" with a moving highlight shimmer (text gradient `ink-3 → ink → ink-3`, 1.6s linear). The reasoning streams open beneath it; when it's done it folds shut into "Thought for Ns" in `ink-3`.
- **Tool row**: a 32px line flush with the reply text above and below it — the icon sits on the column edge and the hover tone bleeds 8px past it, so nothing is inset. Left to right: a 16px `ink-2` tool icon (the Python mark, a terminal, a file glyph, a globe for web search; a connector call shows that connector's own image, or a plug when it has none), the verb in `ink-3` and the app in `ink-2` (both 500: "Used" "Browser"; the app turns `ink` on hover), then the action in `mono` `ink-3`, then a chevron. There is **no state column**: the verb tense says it is done, so a finished row carries no mark at all. Thinking, Compaction and subagent reports use the same line with their own icon and no blank slot.
  - State is carried by the words: running shimmers the whole title like "Thinking" (no spinner); paused for approval shows a breathing `vermilion` dot and "Waiting for you" after the title; skipped or stopped changes the verb ("Skipped", "Stopped") and drops the app to `ink-3`.
  - The chevron is hidden at rest on a pointer device and fades in on hover or keyboard focus; on touch it is always shown. It turns 90° over 260ms when the row opens.
  - Open, the row is not a card: its body hangs under the icon on a 1px `line` rule, indented to the title, and shows the pretty body in a `well` mono block: Python shows code, patch shows a diff, terminal shows the command. The primary view is never raw JSON.
  - While it runs it is open if the fold toggle is on, and streams live: the body is capped at 220px, pinned to its newest lines, with the top fading out. When it finishes it folds shut (unless the reader toggled it). Replayed history starts collapsed.
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
  - When the action's rule is Auto and its model would not decide, the slip settles that one call and nothing else: **Allow** (primary ink) and **Deny**, then one centred `ink-3` line, "Auto-approval was not sure about this one." with a `cobalt` **Rules** link to the Rules page. No button here changes the rule.
  - The footer line: breathing `vermilion` dot + "This run is paused until you choose."
- Secret values never render.
- Choosing: the slip leaves, a receipt lands in the thread, the tool row's state changes, and the lamp crossfades back to Working.

### Ask card

The Bot's question (`ask_owner`), the same card in a thread and on the Dashboard. A question is not an alarm: it never wears vermilion, which stays for a paused run (an approval) and for faults. It is the owner's turn to answer, so it takes the "you can type here" colour.

- A flat `surface` folio, `card` radius, `shadow-card`, in two halves. The upper half is the Bot speaking: a meta line (a `cobalt` question glyph and **Question for you** in 12/500 `cobalt`, then the source and "Asked while you were away" in `ink-3`, the time in mono on the right), the title in `card-title` only when it is not just the question's first line, the question in the reply serif at 16/26, then any context blocks (markdown, kv, metrics, diff).
- The lower half is the owner's **reply tray**: `well`, a 1px `line` top edge, holding the controls from the first one down and the actions — primary submit, a quiet **Not now** (`pressed` on hover), and on the board "Don't suggest this again" at the end. ⌘/Ctrl+Enter submits from anywhere in the tray.
- Choices (one or many) are chips that wrap when every label is short and plain, else full-width rows with room for a hint. Resting: `surface` with a `line-strong` ring, `ink-2` label. Picked: `cobalt-pale` with a `cobalt` ring and `ink` label; the 14px box (many) or dot (one) fills `cobalt` and its tick settles in over 200ms. Switches sit on `surface` rows in the tray.
- Answered or put away, the tray goes, the question turns `ink-2`, and the meta line reads **Answered** (an `emerald` check) or **Put away** in `ink-3`. The answer itself is the owner's next turn in the thread.

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

### Rules

- Every action has two rules, and the page shows one side at a time behind a **Calls | Results** pill switch under the title: Calls is what the Bot may do (Allow / Auto / Ask / Deny), Results is what happens to what comes back before the Bot reads it (Pass / Check / Flag / Withhold). The same rows, the same four-way segment, the same order of meaning: through, a model decides, a middle way, never.
- The legend and the policy panel follow the side: the auto-approve policy for Calls, the output policy for Results (empty means the built-in one; **Start from the built-in policy** loads it to edit).
- A row with no result to filter (a secret, a channel send) is not on the Results side.
- In the thread a filtered result adds one quiet word to its tool row's tail: **Flagged**, **Marked untrusted**, **Unchecked** in `ink-3`, **Withheld** in `vermilion` (it is the one the owner should look at). A withheld row's body opens with "Withheld from the Bot": what the Bot was not given. No badge, no card of its own.

### Section tabs (Settings, the Workspace page)

- A Bot has no top bar. Its Settings, and the Workspace page, open with a 56px head on `canvas` with a `line` bottom edge: the section name in `card-title`, then its tabs.
- Tabs are 32px tall, 13px/500 in `ink-3`, with a 15px icon. Hovering fills `well` and turns the text `ink`. The active tab is `ink`.
- A single 2px `cobalt` underline **slides** to the active tab (transform + width over 280ms `ease-quiet`). It doesn't re-render per tab.

### Machine rail and pane

- **Rail:** 48px wide on `well` at the right edge of every Bot page. From the top: **Files**, **Desktop**, **Console**, **Changes**, then **Mail** while the Bot has an address (18px icons in 36px hit areas, `control` radius, `title` tooltips only), a spacer, then the power glyph for Start Bot / Stop Bot. The open one is a `surface` well with a `line` ring and a 3px `cobalt` bar on its left edge, the side its pane opens toward.
- **Docked pane:** pressing a rail icon beside the chat opens that pane between the page and the rail; pressing it again puts it away. The pane and the page split the row (half each by default, each kept between a quarter and three quarters, never under 320px). The edge between them is a `line` hairline that thickens to 3px `line-strong` on hover and takes the drag; a double-click evens the split. What is docked and how wide is remembered in the browser.
- The pane adds no header of its own. Its two controls sit at the end of the header the pane already has (the hatch's bar in white at 70%, the Files crumb bar in `ink-2`): **Open as a page** (maximize) and **Close**. On a stopped Bot they float in the corner of the Start well.
- **As a page** (`/bots/:id/files|desktop|console`): the pane takes the whole row; the sidebar steps aside, the rail stays. Its one control is **Dock beside the chat**, which returns to the chat it was opened from with the pane docked. Docking and undocking never reconnect the desktop or lose the place in Files.
- Only the chat side takes a docked pane. Channels and Settings keep their full width; a rail icon pressed there goes back to the chat with the pane docked.

### Rail

- 64px wide on `well`, with no right border. From the top:
  - The **Workspace tile** over "Silo" at 11/600: a 32px `well` square with a `line` ring and the Workspace's one or two letters in `ink` 13/600. Pressing it opens a `shadow-slip` menu beside the rail (under the top bar when narrow): a `label-caps` "Workspaces" head, one 40px row per Workspace (a 24px tile, the name in 13.5/500, a `well` role chip unless it is the user's own, the current one with the 3px `cobalt` bar), a hairline, and **New workspace**. Picking a Workspace opens its first Bot.
  - **Workspace** (boxes): the Workspace page at `/w/$spaceId`, the section-tab frame (56px head, tabs, sliding underline). The head opens with the Workspace's 24px tile and name as one link, lit with a `well` fill, to the **Overview**:
    - A 52px tile, the name in 28/600, an `ink-3` line "4 Bots · 3 people · Admin", and on the right **Invite people** (secondary, admins) and **New Bot** (primary, builders).
    - A `well` band with a `line` ring, the Workspace's pulse: a lamp and count per state that has any ("2 need you" in `vermilion`, "1 at work", "3 resting", "1 switched off").
    - **Now** (needs you, then at work) and **Resting** in `label-caps`, each a grid of Bot cards (1 column, 2 from 760px, 3 from 1400px): `surface`, `card` radius, `shadow-card`, lifting to `shadow-float`, the crest blinking on hover, the vermilion 2px ribbon when it needs you. A 44px crest (dimmed when switched off), the name in `card-title`, the lamp and status word, the people's 22px initials when it is shared, two lines of description; at the foot what it is up to — each live conversation as a 32px row (lamp, where, the tool as a mono `well` chip) — or, resting, "Last active 3 h ago" and the next automation with a timer glyph. Under a hairline, what waits: the Dashboard's questions and the Feed's new posts, both in `cobalt` (a question glyph, an inbox glyph). A Bot with only questions waiting sorts after the ones that need you and wears no ribbon; the pulse line counts its questions in `cobalt`. The whole card opens the Bot; its rows open where they point.
    - Each tab below is shown only to the roles it is for:
    - **Connectors**, **Skills**, **Drives** (builders): everything its Bots share, kept once. Each card says which Bots use it and carries **Choose Bots** (a `shadow-slip` list with a switch per Bot you manage); adding asks which Bots in its last step. A resource someone signed in to shows "Signed in by Alice" and a `well` chip **Personal** or **Shared**; its lender alone gets **Share with workspace** (armed: "Click again: Bots act as you") or **Make personal**. Sign-in on a Workspace with other people asks **Only my Bots** | **Anyone in Acme** (armed).
    - **People** (everyone): a `surface` list — a 28px `well` initials circle, the email, "On 3 Bots", the role as a small ChipRadio **Admin | Member | Guest** or an Owner chip, and Remove/Leave; open invite links under it. Invite and Remove are pages of their own: Remove lists each Bot only they manage with a ChipRadio **Make someone manager** | **Take it over** | **Delete it**, then what is deleted with them and what is signed out, and one armed button.
    - **Rules** (admins): the Bot Rules page's own sections, Calls | Results switch and segmented controls, read as "the least strict any Bot here may be" — Allow (and Pass) is no limit, so a fresh Workspace reads all Allow. The policy panel above them is the text every Bot's gate reads before its own.
    - **Activity** (admins): one line per event, the actor and when in `ink-3`.
    - **Settings** (everyone): name and letters, Membership with Leave…, Ownership with Transfer, for admins **Other Bots** (dimmed `surface/70` cards of the Bots they are not on: a 40px crest at 60% with the Tip "Only people on Scout can open it.", the people's 20px initials circles, a grant count, an armed Delete; never a link), then Dangerous.
  - A 24px hairline.
  - The Workspace's crests, 28px each in 40px hit areas with `control` radius.
  - `+` for New Bot in this Workspace (not for a guest).
  - Spacer.
  - Admin (wrench, admins only), the theme toggle (moon in the light, sun in the dark), then the account avatar (an `ink` circle with an `on-ink` initial).
- The active item is a `surface` well with a `line` ring and a 3px `cobalt` bar on the left edge. Hover fills `pressed`. There are no labels, only `title` tooltips.

### Chats list

- 248px wide on `well`, on every page of a Bot. It opens with a 56px head: the Bot's name in `card-title`, and the lamp with its status word on the right.
- **New chat** is the sidebar's one lifted control: a full-width 40px `surface` button with a `line` ring, a 24px `ink` glyph well holding a white plus, then the label in 13.5/500. Hover lifts it to `shadow-float` and turns the plus a quarter (280ms `ease-quiet`); it presses like every button.
- Under it, heading the rows, **Dashboard** (layout-grid): the Bot's home, so it is the same 36px row set in `ink` where the others rest in `ink-2`, with 6px of air below it; open, it is `surface` lifted by `shadow-card` with the `cobalt` bar and a cobalt glyph. While an approval waits it ends in a `vermilion` dot and "N need you"; with only the Bot's questions waiting, a `cobalt` question glyph and "N questions"; the narrow chip carries the count as a dot.
- Then the `control`-radius rows, **Automations** (timer), **Memories** (brain), **Knowledge** (library), **Feed** (inbox), **Mail** (envelope, only while the operator has mail set up) and **Channels** (radio), 15px icon and 13.5/500; the open one is a flat `surface` row with the `cobalt` bar. They are peer pages to Chat, not chats. Feed carries a cobalt count pill (white mono 11px) while it has unread posts; the narrow chip shows an 8px cobalt dot instead. Channels is the Bot's own; what it shares with the Workspace is on the Workspace page.
- The chats follow after a 16px gap, with no "Chats" label of their own: the date groups head them. **Settings** (gear) is one more row pinned to the foot above a `line` hairline; it opens General, People, Rules, Secrets, Tunnels, Containers and Audit as tabs.
- Chats are grouped by when they last moved: **Today**, **Yesterday**, **Previous 7 days**, **Previous 30 days**, then one group per month (the year is named only when it is not this one). No row carries a day name or date; the group is the date, and the full time is the row's tooltip. A group head is a section label, not a row: `label-caps` (11/500, uppercase, tracked) in `ink-3` with the count and a chevron on the right, and 12px of air above every group but the first, so it cannot be read as one more chat. It sticks to the top while its rows scroll under it. Clicking it folds the group (`grid-template-rows` fold, 320ms; the chevron turns −90° over 200ms). The recent groups start open and the month groups start folded; what the reader folds is remembered in the browser. Landing on a chat opens the group it sits in.
- Rows are one line, 32px, flat: the title in 13.5/400 `ink-2` (`ink` on hover and when open), no second line. Hover fills `pressed`; the open row is `surface` with no ring or shadow, marked by a 2px `cobalt` bar on its left edge that grows in as the previous one shrinks out. A live run shows the lamp and its word (**Working** / **Needs you**) at the end of the title. Rename and delete fade in over the end of the title on a gradient of the row's own tone (160ms), and never take space when the row is at rest. The rows above (Automations, Memories, Knowledge, Feed) use the same open treatment.
- Titles are generated from the first prompt. Rename with the pencil or a double-click.

### Automations

- The list (`/bots/:id/automations`) is a `Panel` for the lone Heartbeat, then a **Scheduled** `Panel`. The Heartbeat row sits on a `well` band (the only one) with its 8px mark lifted to `surface`, and a quiet caption beneath — "Every Bot has one, and it cannot be deleted."; tone, not a pin or the word, sets it apart. A row is an 8px mark well, the name in 13.5/500, a line of `meta` (`describe(schedule) · Next …`, or "Paused" / "Running now" / "No schedule — never runs on its own"), and a switch on the right. A live run shows a Working lamp; a failed last run a `vermilion` "Last run failed". The switch pauses without clearing the schedule.
- Marks (`AutomationMark`, lucide's 24px grid and 2px stroke): a scheduled automation is the timer. The **Heartbeat** is a pulse trace, a flat line while it never runs on its own (no schedule, or paused), so the row says whether it beats before the caption does. The **Dashboard** curator is the board itself, a wide widget over two small ones. Both answer a hover on their row, once: the trace draws itself again (640ms `ease-settle`, dash offset), the board's last tile fills (200ms). Nothing loops; reduced motion keeps only the fill. The log page puts the same mark on a `surface` well beside the name.
- New automation (`/new`) and the inline Edit drawer share one form: Name, a Schedule picker (every N minutes / N hours, daily, chosen weekdays, monthly, or raw cron) with a plain-English line as you edit, the Prompt in the same `PromptWell` as SOUL, and an Active switch. Save is primary; Delete is an armed ghost (hidden on the Heartbeat).
- A run's log (`/bots/:id/automations/:id`) reuses the chat `Thread`. The header carries the name, description, Edit, and **Run now** / **Stop**; the tail is the shared thread. Each run opens with a **RunMark** divider — a hairline, a timer glyph, "Ran Today 09:12" in `ink-3` — because the message is the automation's own prompt, not something a human typed (the prompt is its tooltip). No user bubble, no composer.

### Subagents and the Taskboard

- **Taskboard**: a strip at the top of a chat's thread, only when the board has tasks. Folded, it is a 36px `well` bar: chevron, checklist glyph, **Tasks**, `done/total` in mono `ink-3`, one chip per assignee (a name in mono on `pressed`), and a 2px `emerald` progress hairline along the bottom. Open, it lifts to `surface` + `shadow-card` and lists every task: a square check (`emerald` fill when done), `#n` in mono, the assignee chip, the text (struck through in `ink-2` when done), and "done by … · note" in `ink-3`. An armed **Clear board** sits at the end. On a subagent's page the tasks that are not its own are dimmed.
- **Tray**: between the thread and the composer while a chat has subagents. One flat 232px folio per running subagent: `surface` + `shadow-card`, `card` radius, no rule or accent. The first line is the name (`ink` 500) with the elapsed time in mono `ink-3` on the right. The second line is the lamp and status word (**Working** in `emerald`, **Needs you** in `vermilion` while it waits on a slip), then its latest step in mono `ink-3`, like a tool row's action. On hover it lifts 1px to `shadow-float`, and a mini × (vermilion on hover) fades in where the elapsed time was. **Stop all** appears with two or more. A trailing "N finished" control opens a `shadow-slip` menu of the finished ones. The cards scroll sideways; they never wrap.
- Subagents get no colors of their own: color stays a signal (lamp, status word), never an identity. A name is always shown as text; on the Taskboard it is a mono chip on `pressed`.
- **Subagent page** (`/bots/:id/run/:chatId/agent/:agentId`): "Back to the lead", the name, lamp + status, model in mono, a **Brief** fold (Goal, Context), and **Stop** while it works. Then its Taskboard and the shared `Thread`, with no composer — only the lead talks to it. Messages from the lead render as a `well` figure with an `ink-3` rule captioned "From the lead", not as a user bubble.
- **Wake report**: a lead's run that was woken by finished subagents opens with a fold row "Subagents finished · scout ✓ · writer error", each name linking to its page; the report the lead was handed is folded inside.

### Drives

- **List** (`/bots/:id/drives`): one `surface` folio per drive — a 40px provider mark on `well` — the provider's real logo, like a connector's own image (the one place brand colors appear outside crests); protocols without a brand (WebDAV, SFTP, SMB, S3-compatible) get an `ink-2` line glyph, the name in 15/600 with the provider and Read-only as `well` chips, the path in mono with a copy button, then lamp + word: **Mounted** (`lamp`), **Connecting…** (hollow cobalt ring), **Reconnect needed** / **Waiting for an admin** / **Needs details** / **Error** (`vermilion`, plus the 2px vermilion ribbon on the folio). The provider's own error words sit after the word, truncated, full text in a Tip. Actions: **Reconnect** (primary, only when the sign-in expired), **Open in Files** (lands the Files tree in that drive), Edit, armed Remove.
- **Empty state**: a card with ten popular providers as quick tiles (mark over name, five across) and a cobalt “All 15 providers” link.
- **Gallery** (`/new`): autofocused search, then providers grouped by category under `label-caps` headings, two up. A provider an admin has not set up stays visible but quiet (`ink-3`, “Needs admin setup”) with a Tip that says who has to act; for admins, clicking it opens Admin → Drives.
- **Add / edit** (`/new/:template`, `/:driveId`): one page of numbered step cards, not a modal wizard — **Connect** (the provider's questions, then **Sign in with Google** in a popup, turning into “Connected as …” with **Change account**; or **Test connection**, which answers in place with an emerald “Connected. Found N folders.” or the provider's words in a vermilion well), **Choose what to mount** (pickers filled from the provider, and a folder picker that walks one level at a time with a crumb trail and **Mount this folder**), **Name and access** (the name as mono with a live `/workspace/drives/<name>` preview, a Read-only switch, collapsed Advanced settings). Later steps stay visible at reduced opacity until the step before is done; a done step's number becomes a check.
- **Admin → Drives**: one collapsed folio per provider that needs an OAuth client, grouped by category, with **Ready** (emerald word) or a quiet “Needs setup” chip. Open: the setup guide, the exact redirect URI with a copy button, and the fields. Secrets are write-only (`••••••••` with Replace and an armed Clear); a value set in the environment is locked and names its variable.

### Skills and Channels

- Both list pages wear the Connectors frame: a `widePage`, a title with a count chip and one calm sentence, then the toolbar — `TabPills` (a `well` tray, the open tab lifted, counts in `cobalt-pale`) and the `ToolbarSearch` field with its ⌘K hint — over a two-up grid of `surface` cards (`shadow-card`, `shadow-float` on hover). Empty and no-match states are dashed or `surface` wells with one next step, never a bare sentence.
- **Skill Hub** (`/skills`): **Personal | Library**. A skill card is a 48px scroll mark on `well`, the name in 15/600 with a `Catalog` chip for the embedded ones, two lines of description, and a hairline footer with the source in mono (a GitHub URL shrinks to `owner/repo`), **Inspect**, and an armed **Remove** where it applies. The whole card opens the hatch. Personal opens with the install band: a `well` panel (not a card) holding the URL field, **Install** and **Upload zip**; a zip can be dropped on it, and the band takes a 1.5px `cobalt` ring while one is over it. Library groups into **Catalog** and **Added by admins**. The Bot's Skills tab is the same card with a switch top-right and a footer word, **In the prompt** (`emerald`) or **Off**.
- **Add a channel** (`/bots/:id/channels/new`): one card per adapter in the Connectors "featured" style — the real logo in a lifted tile on a faint `cobalt-pale` corner, the name in 19/600, `well` chips for how it signs in (**Bot token** / **QR login**, **One chat**), the description, three emerald-check points, and a full-width **Add Telegram** (**Add another** with an emerald "In use" when the Bot already has one). A footer callout points at Connectors for email and calendar. The list's empty state offers the adapters as quick tiles.
- **Add / edit a channel** and **Setup** are two columns, not a narrow strip on the left: the form in numbered `Step` cards (**Connect**: name and credentials, short fields side by side and tokens full width; **Behavior**: reply mode, the Enabled and Deliver toggles as `well` rows, the prompt) beside the adapter's guide in a sticky `well` panel that scrolls on its own. Below 1024 the guide is a shut fold above the form. Setup stacks a status strip, the QR (**Link your device**), the **Target chat** panel (bound state, the adapter's actions, the picker, set-directly) and, on adapters with no target, an **Actions** panel. A channel that needs you wears the 2px `vermilion` ribbon.

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

## 7. Layout

- **Shell:** the rail (64px) plus main. Break at **960** (`wide:` / `max-wide:`). Below 960, the rail becomes a 48px top bar (safe-area padded): crests scroll sideways and the foot (Admin, theme, Account, Sign out) is one avatar menu. A Bot page hides it: its own top bar opens a drawer that carries it.
- **Bot view:** no top bar. Three columns: the sidebar (248, `well`), the page on `canvas`, and the machine rail (48, `well`). Chat is the page a Bot opens on.
  - The sidebar heads with the Bot's **home**: one block, the name in `card-title` with its lamp and status word, and under it a `LayoutGrid` + **Dashboard** line whose end says what waits on the board (vermilion dot + "2 need you", else cobalt dot + "3 new", else a working lamp + "Curating", else nothing; a chevron slides in on hover). The whole block opens the Dashboard; open, it is `surface` + `shadow-card` with the cobalt bar. It is not a second lifted button. Then New chat, the pages beside the chat, Channels, the chats, and Settings at the foot.
  - A right-rail pane (Files, Desktop, Console, Changes, Mail) docks between the page and the rail, or takes the row as a page of its own with the sidebar out of the way. Mail needs no machine and its rail icon shows only while the Bot has an address.
- **Sections:** **Settings** is `General`, `People`, `Rules`, `Secrets`, `Tunnels`, `Containers`, `Audit`; the Workspace page is `Connectors`, `Skills`, `Drives`, `People`, `Rules`, `Activity`, `Settings`. Each is a head with tabs over the open page. Add, edit and setup flows are their own routes, so the browser Back button works.
- **Chat sidebar:** **Automations**, **Memories**, **Knowledge** and **Feed** are conversation-side pages, so they sit as rows above the chats list. Below 960 they are rows in the Bot's drawer. **Mail** is a right-rail pane instead.
- **Dashboard:** the Bot's board, no title and no explaining sentence. One `meta` line in `ink-3`: "Curated Today 09:12", a dotted-underlined "next Tue 10:00" (or "no schedule") that opens the Dashboard automation, a quiet Look again glyph; while the Bot works on the board the line is the breathing lamp and a shimmering "TestBot1 is working on the board" with **Watch**. **Arrange** (ghost, right) turns the corners into a width chip (¼ ½ 1/1, a press moves to the next), a height chip (×1 ×2) and an armed remove; widgets drag to reorder; **Done** saves, and the owner's place then wins over the Bot's. Then what waits on the owner, two up: approvals as slips on the 2px `vermilion` ribbon, then the Bot's asks (see **Ask card**). Then **Since you last looked**, the Feed's unread posts for this visit (cobalt dots; a row opens in place to the serif body with **Quote in a new chat**; **All in Feed** in cobalt). Then the widgets on a 4-column grid (2 below 760px of page, 1 below 480), packed dense. A widget is a flat `surface` card: title in 13/500, the source and data age in mono `ink-3` at the end; Refresh and **Ask about this** fade in over that corner on hover. A changed widget has the 6px cobalt dot for the visit. Kinds: **note** is the Bot's own words in the reply serif; **stat** a 34px Geist number with a mono unit, a caption and an ink hairline sparkline with its last point marked; **chart** the same hairline or bars (`ink-3` with the last in `ink`); **list** and **table** rows with hairlines, numbers right-aligned in mono, row actions fading in like a chat row's rename; **timeline** times in mono on a rail with the `cobalt` now-rule (the board's one "you are here"); **status** lamp + mono name + word (`emerald` up, `vermilion` down, `ink-2` degraded, hollow off); **checklist** the Taskboard's square checks; **progress** a number and a 3px ink bar; **app** a live artifact app in the hatch. No colour on deltas. Under a widget: its buttons (first run action secondary, hand actions ghost with a speech glyph, `confirm` ones armed), "Couldn't refresh" in `ink-3` with the error in a Tip, and the receipt line (emerald check, the result in `ink` 500, a hairline, "by you · 09:31"). The ask bar is pinned to the foot over a canvas fade: the composer's 18px card and Send, a picked widget as a `cobalt-pale` chip. Empty, the board is the `display` line "Nothing on the board yet", one sentence, the ask bar and four suggestion chips.
- **Feed:** a `silo-page` of `surface` cards, newest first: optional title (15/600), a mono time + source line (a new post has a 6px cobalt dot for the visit), then the body in the thread's `Md`. Ghost icon actions top-right: Quote (message-square-quote) opens a new chat, Delete is armed. A chat that opens from something the Bot made starts with one `well` card on a 3px cobalt rule, captioned in 12px `ink-3` with what it is (Inbox "From the Feed", grid "Widget on the Dashboard" or "Asked on the Dashboard"), the source and the time: a post or an ask in the reply's `Md` (an ask's title in 15/500 above), a widget drawn again as the board draws it, read-only, on its own `surface` card. Read-only — no composer. A quoted post opens its chat as a `well` card with a 3px cobalt leading rule and a “Quoted from the Feed · source · time” caption.
- **Changes:** a right-rail pane with the same 40px bar as Mail — the compare glyph, "Changes", `/workspace` in mono 12/`ink-3`, then the pane actions. The body is one `surface` list with hairlines, newest first, one row per stretch between two snapshots: who was at work in 13.5/500 ("Chat “Tidy up”", or "Outside a run"), mono time on the right ("Now" for what is not snapshotted yet), then the file count and git's `+12 −4` in mono `ink-3` — no colour on the counts. A row opens in place into its files (32px rows: the path in mono, cut at its start so the file name stays, a status word only when it is not a plain edit, counts or sizes on the right), and a file opens into its diff: a `well` mono block that scrolls on its own, line numbers in `ink-3`, additions `emerald` and deletions `vermilion` as text only (the thread's diff colours; no tinted line backgrounds). A file with no diff to show says why in one `well` sentence (too large, not text, a repository of its own). **Putting things back:** an open change ends with a secondary **Undo this change** (the undo glyph) and an open file with a ghost button named for what it will do — **Restore this file**, **Remove this file** for one the change added, **Bring this file back** for one it deleted, **Move this file back** for a rename — beside the `ink-3` words "As it was before this change." Both are armed ("Click again to put 4 files back"). A file with no content in the history (too large, a repository) has no button. What a restore did is one `well` sentence above the list, including what it left alone and why, until the reader opens another row; the restore then heads the list as **Restored by you**, and undoing that row is how a restore is taken back. The footnote says what is left out. No badge on the rail icon. Once the Bot has a drive, a `well` pill switch (**Workspace** | **Drives**, no counts) sits above the list. **Drives** is the journal: the same list, a line per file written, deleted or renamed — the path in mono as the Bot sees it under `drives/`, mono time, then one `ink-3` sentence ("Deleted · Chat “Tidy up”"; a delete is `ink-2`). Its rows do not open: no copy of a drive is kept, so there is no diff, and the footnote says where to get a file back. Stopped, Workspace is the shared Start well under the pane's own header; Drives reads either way.
- **Mail:** a right-rail pane headed by a 40px `line-strong` bar — the Mail glyph, "Mail", the address in mono 12/`ink-3` with a Copy beside it, then the pane actions (dock/open as a page, close). The body scrolls the messages: one `surface` list with hairlines, newest first: sender in 13.5/500, subject, an `ink-3` preview, mono time on the right; a 6px cobalt dot is "the Bot has not read this yet", a small `ink-3` shield is an unverified sender, a paperclip is attachments. A row opens in place (chevron turns 90°): From/To, the sender check as one line (`emerald` shield, or the whole line in `vermilion` when unverified), the body as plain text in a `well` (never rendered as markdown: a stranger wrote it), attachment chips, then ghost actions (Open the chat it started, Download original, armed Delete). The per-Bot Settings page carries the rest in one panel: the address (mono 13.5/500, Copy, armed ghost **New address**) and the wake switch — its Senders field (mono) and the Save/Discard pair show only when there is something to edit. No badge on the rail icon: the Bot reads the mail, not the owner.
- **Settings:** one column, 760px max (forms 560px), in `card`-radius panels. The Dangerous panel comes last, with armed destructive buttons.
- **Admin:** a 1000px frame: the title, then one underlined tab per page (Settings and the libraries). **Settings** splits in two: a 196px list of categories that stays in view while the page scrolls, and the open category's panels beside it. A category row is the sidebar's row (36px, 15px icon, 13.5/500 `ink-2`); the open one is a `well` fill with the `cobalt` bar, and a hairline sets silo.yaml and Audit apart from the form. Each category is its own address. **Models** has its pages hanging under it on a `line-strong` hairline: 32px rows, a 14px icon in `ink-3`, 13px `ink-2`; the open page is a `well` fill and puts the 2px `cobalt` bar on the hairline itself, while Models stays `ink` without a fill. One Save bar floats at the foot of every form category and counts unsaved changes across all of them. Below 960 the list is one sideways-scrolling row of the same rows, and Models' pages are a second row of pill chips over the page (the open one a `surface` pill with `shadow-card`).
- **Models:** every page under Models opens with the same head: the kind's glyph on a 40px `surface` tile with `shadow-card`, the title in `title`, one sentence in 13.5 `ink-2`, and at the right a `label-caps` figure (how many models are allowed, in 20px mono, with the small monograms of their providers; the map counts providers, those that need a key in `vermilion`, and jobs nobody does). The Models page itself is only the **model map**: the jobs down the left (label 13/500, the model in mono `ink-3`, a 6px `ink-3` dot where its line leaves, a hollow one when nobody does it), the providers in use down the right as flat `surface` cards (a monogram on a `well` tile, the name, how many jobs, the lamp and Ready / Not set up), and between them hairline curves in `line-strong` (dashed for a job that follows another setting); a hairline key sits under it. Hovering a job or a provider draws its lines in `ink` and fades the rest to `line`; a job whose provider has no key says "needs a key" in `vermilion`. A job opens its page, a provider opens on Providers. A kind's settings are rows in one panel: the job (13.5/500) and its sentence on the left, the picker on the right (300px); under an empty picker, a `CornerDownRight` line in 12 `ink-3` names the model it falls back to in mono and where from. Its allowlist rows carry the provider's small monogram and, under the id, the jobs that use it in `ink-3`.
- **Providers:** one panel lists every provider as a row (chevron, monogram, name, the description in `ink-3`), then five columns of icons (text, decision, speech-to-text, speech, embeddings) whose cells are a hairline (not offered), an open 7px square (available), a filled `ink` square with the allowed count in mono, or the waveform glyph (live), and the status at the end. A `well` band under the rows is the key. A row opens in place (the fold) into its fields and **List models**; the open row carries the 2px `cobalt` bar. No colour in the grid but the lamp.
- **Provider models:** an open provider carries **List models** in its head. The list opens inside the panel on a `well` band under the fields: a filter field, then a `surface` list (nine rows tall, scrolling inside) of the model id in mono, the provider's name for it in `ink-3`, the context window in mono, and **Add**. An allowed model reads **Allowed** in `emerald` with a quiet × in the same slot, so the row never shifts.
- There is no Bots home: `/` opens the Workspace's first Bot, and the rail's crests are the list. No KPI rows, no footers, no centered screens except sign-in.
- **Below 960:**
    - A Bot page has one 48px top bar on `well`: one press target with the menu glyph (a dot when something waits: `vermilion` for an approval, else `cobalt`), the crest with its lamp, the Bot's name over the page you are on in `ink-3`, then New chat as a lifted `surface` square. The press slides in the drawer from the left (`min(22rem, 88vw)`, `well`, `shadow-slip`, over a dimmed page): the rail on top, then the sidebar in its order with rows 40px tall, **Machine** (Files, Desktop, Console, Changes, Mail and Start/Stop as labelled tiles, three to a row) under the pages, the chats, and Settings at the foot, all in one scroll. A press outside, Escape, a swipe left or any page change puts it away. The open chat's rename and delete stay visible: there is no hover.
    - On a touch screen, controls that fade in on hover (message actions, row actions) are always shown, and fields are 16px so iOS does not zoom into them.
    - Nothing docks: Files, Desktop, Console, Changes and Mail open as their own pages.
  - Files shows either the tree or the preview, not both.
  - The approval slip becomes a bottom sheet.
  - Hit areas are 40px.
- **iOS:** the same tokens and faces (Geist, Geist Mono, Source Serif 4 ship in the app), no glass on content and no blue fills. The app opens on a chat (the last one with the last Bot, else a new one with no row behind it). The nav bar is the crest (opens the sidebar), the chat's title (its menu renames, saves memories, compacts, deletes), New chat, and the status lamp (opens the machine). The sidebar is the web's, in the same order, sliding in from the left edge on `well` (a column on iPad): its crest strip opens with the Workspace tile (a menu of the Workspaces with their role, then New workspace) and holds only that Workspace's Bots, and Channels, the Workspace, Settings and the account sit at its foot; there is no tab bar. The Workspace page is pushed like any page: the tile, name and counts, New Bot and Invite people, the tabs as a row of chips (each a page), then the overview's pulse band and the Now and Resting cards. With no Bot of the user's in the Workspace, that page is the root. The machine is a sheet: the status with Start (ink) or Stop (quiet), then Files, Changes, Mail and Containers. A run paused on an approval puts the slip in place of the composer in its chat (Allow once in ink, Deny quiet in `vermilion`, the rule one tap further); every approval also waits on the Dashboard. Nothing pops over a page by itself.

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

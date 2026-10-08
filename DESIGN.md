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
| `emerald`        | `#0F7A55` | Online / Working status word, allow receipts. Passes as small text (5.1:1).                                           |
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
- Above the head sit three `control`-radius rows, **Automations** (timer), **Memories** (brain), and **Feed** (inbox), 15px icon and 13.5/500; the open one is a flat `surface` row with the `cobalt` bar. They are peer pages to Chat, not chats. Feed carries a cobalt count pill (white mono 11px) while it has unread posts; the narrow chip shows an 8px cobalt dot instead.
- Chats are grouped by when they last moved: **Today**, **Yesterday**, **Previous 7 days**, **Previous 30 days**, then one group per month (the year is named only when it is not this one). No row carries a day name or date; the group is the date, and the full time is the row's tooltip. A group head is a `label` (12/500, sentence case) in `ink-3` with the count and a chevron on the right; it sticks to the top while its rows scroll under it. Clicking it folds the group (`grid-template-rows` fold, 320ms; the chevron turns −90° over 200ms). The recent groups start open and the month groups start folded; what the reader folds is remembered in the browser. Landing on a chat opens the group it sits in.
- Rows are one line, 32px, flat: the title in 13.5/400 `ink-2` (`ink` on hover and when open), no second line. Hover fills `pressed`; the open row is `surface` with no ring or shadow, marked by a 2px `cobalt` bar on its left edge that grows in as the previous one shrinks out. A live run shows the lamp and its word (**Working** / **Needs you**) at the end of the title. Rename and delete fade in over the end of the title on a gradient of the row's own tone (160ms), and never take space when the row is at rest. The rows above (Automations, Memories, Knowledge, Feed) use the same open treatment.
- Titles are generated from the first prompt. Rename with the pencil or a double-click.

### Automations

- The list (`/bots/:id/automations`) is a `Panel` for the lone Heartbeat, then a **Scheduled** `Panel`. The Heartbeat row sits on a `well` band (the only one) with its 8px mark lifted to `surface`, and a quiet caption beneath — "Every Bot has one, and it cannot be deleted."; tone, not a pin or the word, sets it apart. A row is an 8px mark well (timer mark), the name in 13.5/500, a line of `meta` (`describe(schedule) · Next …`, or "Paused" / "Running now" / "No schedule — never runs on its own"), and a switch on the right. A live run shows a Working lamp; a failed last run a `vermilion` "Last run failed". The switch pauses without clearing the schedule.
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

### Folio cards (Bots home)

- `surface` with a `line` ring and `card` radius, padded 16–20. A 56px crest on the left, then the name (`card-title`), a one-line description (`meta`, `ink-2`) and the lamp with its status word.
- Hover: `shadow-float`, a 1px lift and a crest blink. Press: scale .995.
- Needs you: a 2px `vermilion` ribbon down the left edge only.
- Empty state: "No Bots yet" in `display`, with a single primary **New Bot**.

## 7. Layout

- **Shell:** the rail (64px) plus main. Break at **960** (`wide:` / `max-wide:`). Below 960, the rail becomes a 48px top bar (safe-area padded) and crests scroll sideways.
- **Bot view:**
  - A 56px header on `canvas` with a `line` bottom edge: crest 28 + name + lamp/status, the tab strip, a spacer, then Stop Bot / Start Bot (ghost).
  - The body is chats (248, `well`) next to the thread on `canvas`. The sidebar opens with the **Automations**, **Memories**, and **Feed** rows above the chats head; the page body is the log or list when one is open.
  - Desktop and Console give the whole main column to the hatch. Desktop's caret opens Console.
  - Under 1280px the header's Start/Stop shows only the power glyph.
- **Tabs:** `Chat`, `Desktop`, `Files`, `Drives`, `Connectors`, `Channels`, `Skills`, `Secrets`, `Rules`, `Containers`, `Settings`. The strip scrolls horizontally with edge fades. Add, edit and setup flows are their own routes, so the browser Back button works.
- **Chat sidebar:** **Automations**, **Memories**, and **Feed** are conversation-side pages, so they sit as rows above the chats list and keep the Chat tab lit. Below 960 they are icon chips (label only when active) at the start of the chats strip, before a hairline and the chat chips.
- **Feed:** a `silo-page` of `surface` cards, newest first: optional title (15/600), a mono time + source line (a new post has a 6px cobalt dot for the visit), then the body in the thread's `Md`. Ghost icon actions top-right: Quote (message-square-quote) opens a new chat, Delete is armed. Read-only — no composer. A quoted post opens its chat as a `well` card with a 3px cobalt leading rule and a “Quoted from the Feed · source · time” caption.
- **Settings / admin:** one column, 760px max (forms 560px), in `card`-radius panels. The Dangerous panel comes last, with armed destructive buttons.
- **Bots home:** padded 28. One column below 1100px, 2 up to 1440px, 3 above that. No KPI row, no footer, no centered screens except sign-in.
- **Below 960:**
  - Tabs become icons, with a label only on the active tab.
  - Chats become a horizontal chip strip, opening with the **Automations**, **Memories**, and **Feed** icon chips.
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

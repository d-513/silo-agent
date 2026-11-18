# iOS: intentionally skipped (web-only for now)

The iOS client aims for parity with the web client. These pieces are deliberately left on the web; the iOS pages link out ("Use the web client") instead.

| Area | Why it stays web-only |
| --- | --- |
| **Desktop** (`/bots/:id/desktop`) | VNC websocket hatch; needs a VNC client. Low value on a phone. |
| **Console** (`/bots/:id/console`) | PTY over websocket; needs a terminal emulator view. |
| **Channels setup wizard** | Adapter `Setup` actions (`pick` / `run` / `qr`) and the target picker. iOS lists channels and toggles/edits basic fields; a channel that still needs its target shows "Set up on web". |
| **Connector OAuth, editing + custom MCP form** | The OAuth callback finishes only in the browser of the user who started it (`{public_url}/oauth/callback`), and the connector form is large. On the Workspace page iOS lists the Workspace's connections, adds library presets (name + which Bots), gives and takes them per Bot (Choose Bots), shares or un-shares a lent one, and deletes; a connection that needs sign-in says "Sign in on the web client", and editing one (or a built-in's settings) is on the web. |
| **Workspace join links** (`/join/$token`) | Opening an invite link (and making an account through one) is a web page. iOS makes links (share sheet) and lists and deletes open ones, but does not handle `/join/` URLs. |
| **Skill and drive extras** | Installing a skill into the admin Library and the drive template gallery's admin hints stay on the web; iOS installs Workspace skills by URL or zip and adds, edits, signs in and reconnects drives. |
| **Setup offers for channels and drives** | A Bot's offer to add a channel or a drive needs that form (sign-in, QR, target picker), so iOS opens the chat on the web client and has "I set it up". A library connector offer is added on iOS with its default settings (the Bot's prefilled name and rule are not applied). |
| **Desktop pause** (`SetDesktopPaused`) | The pause switch sits in the Desktop pane, which is web-only. |
| **Ctrl/⌘-click answers** | On the web a modified click answers a Dashboard question in a chat of its own; iOS answers in the main chat (the Feed has "Reply in a new chat"). |
| **Admin** (`/admin/*`) | Operator settings, Connectors/Skills libraries, Drives system vars, search, audit and LLM logs, operator rules. Out of scope for now. |
| **Single sign-on, invites, display language** | OIDC sign-in is a browser round trip to `{public_url}/auth/oidc/callback`; invite links are web pages; iOS is not translated, so it has no language setting. iOS signs in with a password (and the two-factor code). Email, password, two-factor and sessions are in Account. |
| **Dashboard arranging** | Dragging widgets and setting their size (`ArrangeDashboard`). iOS shows the board in one or two columns in the Bot's order; the owner can check items, run actions, refresh, remove a widget and ask the Bot for changes. |
| **Network lists and traffic** (Rules → Network, Audit → Traffic) | Block/trust host patterns and the per-host traffic table. iOS shows the audit timeline only. |
| **Runs** (`/bots/:id/runs`) | The managers' review of everything a Bot did (`ListRuns`, `GetRunCalls`, `ListBackground`) and its looks in `statusLook.ts` (`ranLook`, `stillRunning`). A dense page for a desk; not ported. |
| **Artifact sharing** | Creating a public share link from a card (`ShareArtifact`). iOS opens, versions and deletes artifacts, and lists/removes existing shared links on the Tunnels page. |

The layout is the web's below 960: the Bot's top bar (the menu, the crest and name over what it is doing, Call, the rail), the chat tabs as a strip under it (Dashboard, the main chat, side chats in the order they were made, + and the closed-chats arrow; × closes the open side tab, a long press renames, closes or deletes one), the sidebar as a sheet from the left (a column on an iPad) and the rail as a sheet from the right (the pages, the machine's tiles with Start/Stop, Settings).

Everything else on a Bot is in scope and on iOS: the main chat, side chats with Closed chats, questions and setup offers in the thread, slash commands (the `/` menu, `/clear`, `/wipe` with its confirm), older pages of a long thread, shared Bots (the Team group, read-only chats, authors, once-only approvals), Proactivity, Dashboard (with ideas), Automations, Artifacts, Memories, Knowledge, Feed, Files, Changes (workspace diffs with restore, and the drive journal), Mail (inbox and the Settings panel), Channels, Settings (General with the time zone, People, Rules for Calls and Results with the Workspace's floors, Secrets, Tunnels, Containers, Audit, and Workspace for moving the Bot).

Workspaces are on iOS too: the switcher on the sidebar's Workspace row (and New workspace), the current Workspace's Bots as the sidebar's conversations (newest message first: what the Bot is doing or the last message, unread, Call; the open one lifted with its other work under it), New Bot in it (interview, what it can use, who can use it), and the Workspace page behind the sidebar's gear: Connectors, Skills, Drives, People (roles, invite links, remove and leave with the hand-off of Bots only they manage), Rules, Activity and Settings (name and letters, leave, transfer, Other Bots, delete).

## Behind the web

Not decisions, just not done yet:

- Secrets: surrogate mode, hosts, rotate.
- Rules: the Network card. Audit: the Traffic tab.
- Channels: Setup and Log.
- Settings: Language and the SOUL counter. Account: Language and Set password. Single sign-on.
- Changes: chat links, and Undo on Now.
- Dashboard: Look again, Arrange, the inline ask form.
- Automations: Stop, Edit and live state in the log.
- Subagents: the Taskboard page.
- Messages between Bots: the Settings switch, the From other Bots group, the read-only notice in such a conversation, and a Bot's message drawn as a note (`Chat.from_bot`, a `user` event with tool `bot`, a report label with `@` names).
- Artifacts: Share and Save skill.
- Memories: the filters (Preferences, Lessons, Facts, Noticed, Retired), Keep on a noticed one, and the retired ones with Put back. A row names its kind and says when it is only noticed.
- Offer cards: the provider's mark.
- Crests: the loops (the blink at rest, the dashed layer while working, the bob, the drifting z's) and listening while you type. A crest slides between states and shows needs you, working, done and asleep; a widget's shows only needs you and working.

## Not yet run on a phone

Everything here compiles and was checked as far as a simulator goes; none of it has met real hardware.

- A call's microphone: voice processing, Silero on a real voice, barge-in over the speaker. Barge-in may land about 100 ms later than on the web: a tap delivers audio in bursts of that size. An `AVAudioSinkNode` would fix it, but it is realtime code that wants a phone to test on.
- A real interruption. Recovery was tested on a simulator by stopping the engine and posting the notifications, not with a phone call or Siri.
- CallKit, the earpiece at your ear, the route picker with AirPods and in a car.
- The Dynamic Island's and Lock Screen's look (the views are in the debug gallery; the system accepts and renders the activity).
- Widgets on the Home Screen, controls, the Action Button, quick actions, the share sheet, notifications with Allow and Deny, background refresh.
- The crest drawn in code (`CrestView`, `Logic/Stack.swift`): it builds, `StackTests` pass, and it draws in the app on a simulator; the lock screen's circular widget (`cutout`) has not been looked at.
- Siri and Spotlight on the new entities and snippet cards (Ask, Check on, Review approvals with its buttons), and the new Shortcuts actions; the metadata registers (8 phrases, 7 entities) and the app launches with them.
- The document camera (the simulator has none), drag and drop onto the composer, the iPad menu bar and ⌘ shortcuts.
- The small Live Activity on an Apple Watch and in CarPlay, and the Smart Stack picking up the Needs you widget.
- The watch app and its complications have only been built, never run: this Mac has no watchOS simulator runtime (`xcodebuild -downloadPlatform watchOS`). Untried: the sign-in handoff, the watch reaching the server on its own (Wi-Fi, LTE, through the phone, over WireGuard), dictation, and each problem message on a real failure.
- Signing: set a team, then check the App Group it derives.

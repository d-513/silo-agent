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
| **Workspace overview links** | On the web each line of a Bot card opens where it points (a chat, the Dashboard's questions, the Feed). On iOS the card opens the Bot and a live conversation opens that chat; the questions and Feed counts are words only. |
| **Admin** (`/admin/*`) | Operator settings, Connectors/Skills libraries, Drives system vars, search, audit and LLM logs, operator rules. Out of scope for now. |
| **Single sign-on, invites, display language** | OIDC sign-in is a browser round trip to `{public_url}/auth/oidc/callback`; invite links are web pages; iOS is not translated, so it has no language setting. iOS signs in with a password (and the two-factor code). Email, password, two-factor and sessions are in Account. |
| **Dashboard arranging** | Dragging widgets and setting their size (`ArrangeDashboard`). iOS shows the board in one or two columns in the Bot's order; the owner can check items, run actions, refresh, remove a widget and ask the Bot for changes. |
| **Network lists and traffic** (Rules → Network, Audit → Traffic) | Block/trust host patterns and the per-host traffic table. iOS shows the audit timeline only. |
| **Artifact sharing** | Creating a public share link from a card (`ShareArtifact`). iOS opens, versions and deletes artifacts, and lists/removes existing shared links on the Tunnels page. |

Everything else on a Bot is in scope and on iOS: Dashboard, Automations, Artifacts, Memories, Knowledge, Feed, Files, Changes (workspace diffs with restore, and the drive journal), Mail (inbox and the Settings panel), Channels, Settings (General with the time zone, People, Rules for Calls and Results with the Workspace's floors, Secrets, Tunnels, Containers, Audit, and Workspace for moving the Bot).

Workspaces are on iOS too: the switcher on the sidebar's tile (and New workspace), the crests of the current Workspace only, New Bot in it (interview, what it can use, who can use it), and the Workspace page: the overview, Connectors, Skills, Drives, People (roles, invite links, remove and leave with the hand-off of Bots only they manage), Rules, Activity and Settings (name and letters, leave, transfer, Other Bots, delete).

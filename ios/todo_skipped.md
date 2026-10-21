# iOS: intentionally skipped (web-only for now)

The iOS client aims for parity with the web client. These pieces are deliberately left on the web; the iOS pages link out ("Use the web client") instead.

| Area | Why it stays web-only |
| --- | --- |
| **Desktop** (`/bots/:id/desktop`) | VNC websocket hatch; needs a VNC client. Low value on a phone. |
| **Console** (`/bots/:id/console`) | PTY over websocket; needs a terminal emulator view. |
| **Channels setup wizard** | Adapter `Setup` actions (`pick` / `run` / `qr`) and the target picker. iOS lists channels and toggles/edits basic fields; a channel that still needs its target shows "Set up on web". |
| **Connector OAuth + custom MCP form** | OAuth callback is `{public_url}/oauth/callback` and the custom form is large. iOS lists, refreshes, attaches library presets and detaches; a connector that needs auth shows "Authorize on web". |
| **Admin** (`/admin/*`) | Operator settings, Connectors/Skills libraries, Drives system vars, search, audit and LLM logs, operator rules. Out of scope for now. |
| **Single sign-on, invites, display language** | OIDC sign-in is a browser round trip to `{public_url}/auth/oidc/callback`; invite links are web pages; iOS is not translated, so it has no language setting. iOS signs in with a password (and the two-factor code). Email, password, two-factor and sessions are in Account. |
| **Dashboard arranging** | Dragging widgets and setting their size (`ArrangeDashboard`). iOS shows the board in one or two columns in the Bot's order; the owner can check items, run actions, refresh, remove a widget and ask the Bot for changes. |
| **Network lists and traffic** (Rules → Network, Audit → Traffic) | Block/trust host patterns and the per-host traffic table. iOS shows the audit timeline only. |
| **Artifact sharing** | Creating a public share link from a card (`ShareArtifact`). iOS opens, versions and deletes artifacts, and lists/removes existing shared links on the Tunnels page. |

Everything else on a Bot is in scope and on iOS: Dashboard, Automations, Artifacts, Memories, Knowledge, Feed, Files, Changes (workspace diffs with restore, and the drive journal), Drives, Mail (inbox and the Settings panel), Connectors, Skills, Channels, Settings (including the time zone), Rules (Calls and Results), Secrets, Tunnels, Containers and Audit.

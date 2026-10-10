# iOS: intentionally skipped (web-only for now)

The iOS client aims for parity with the web client (see the plan in the repo history). These pieces are deliberately left on the web; the iOS pages link out ("Use the web client") instead.

| Area | Why it stays web-only |
| --- | --- |
| **Desktop** (`/bots/:id/desktop`) | VNC websocket hatch; needs a VNC client. Low value on a phone. |
| **Console** (`/bots/:id/console`) | PTY over websocket; needs a terminal emulator view. |
| **Channels setup wizard** | Adapter `Setup` actions (`pick` / `run` / `qr`) and the target picker. iOS lists channels and toggles/edits basic fields; a channel that still needs its target shows "Set up on web". |
| **Connector OAuth + custom MCP form** | OAuth callback is `{public_url}/oauth/callback` and the custom form is large. iOS lists, refreshes, attaches library presets and detaches; a connector that needs auth shows "Authorize on web". |
| **Admin** (`/admin/*`) | Operator settings, Connectors/Skills libraries, Drives system vars, search/extract, audit and LLM logs. Out of scope for now. |

| **Single sign-on, invites, account settings** | OIDC sign-in is a browser round trip to `{public_url}/auth/oidc/callback`; invite links, password changes, two-factor setup and the sessions list are web pages. iOS signs in with a password and asks for the two-factor code when the account has one. A session started on iOS shows in the web Account page and can be signed out there. |
| **Knowledge** (`/bots/:id/knowledge`) | The folder picker, per-folder sync state and the try-a-search panel. Web only for now; the Swift client already has the RPCs. |
| **Changes** (`/bots/:id/changes`) | The diff pane for what changed in the workspace. Web only for now; the Swift client already has the RPCs. |
| **Tunnels** (`/bots/:id/tunnels`) | Add/flip/delete a tunnel and open its address. Web only for now; the Swift client already has the RPCs. |

Drives, Files, Secrets, Rules, Skills, Automations, Memories, Feed, Containers and Settings are in scope.

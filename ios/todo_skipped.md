# iOS: intentionally skipped (web-only for now)

The iOS client aims for parity with the web client (see the plan in the repo history). These pieces are deliberately left on the web; the iOS pages link out ("Use the web client") instead.

| Area | Why it stays web-only |
| --- | --- |
| **Desktop** (`/bots/:id/desktop`) | VNC websocket hatch; needs a VNC client. Low value on a phone. |
| **Console** (`/bots/:id/console`) | PTY over websocket; needs a terminal emulator view. |
| **Channels setup wizard** | Adapter `Setup` actions (`pick` / `run` / `qr`) and the target picker. iOS lists channels and toggles/edits basic fields; a channel that still needs its target shows "Set up on web". |
| **Connector OAuth + custom MCP form** | OAuth callback is `{public_url}/oauth/callback` and the custom form is large. iOS lists, refreshes, attaches library presets and detaches; a connector that needs auth shows "Authorize on web". |
| **Admin** (`/admin/*`) | Operator settings, Connectors/Skills libraries, Drives system vars, search/extract, audit and LLM logs. Out of scope for now. |

| **Knowledge** (`/bots/:id/knowledge`) | The folder picker, per-folder sync state and the try-a-search panel. Web only for now; the Swift client already has the RPCs. |
| **Tunnels** (`/bots/:id/tunnels`) | Add/flip/delete a tunnel and open its address. Web only for now; the Swift client already has the RPCs. |

Drives, Files, Secrets, Rules, Skills, Automations, Memories, Feed, Containers and Settings are in scope.

import type { LampStatus } from "./Lamp";

// How each kind of thing shows its state: the shared lamp, the word beside it
// (a lamp is never alone), and that word's color. Each state vocabulary is the
// server's; the lamp vocabulary is the design system's.
export type StatusLook = { lamp: LampStatus; word: string; tone: string };

// A channel adapter's connection.
export function channelLook(status: string): StatusLook {
  switch (status) {
    case "connected":
      return { lamp: "online", word: "Connected", tone: "text-ink-3" };
    case "starting":
      return { lamp: "starting", word: "Starting…", tone: "text-ink-3" };
    case "pending_auth":
      // A QR login waiting to be scanned (WhatsApp).
      return { lamp: "needs_you", word: "Needs linking", tone: "text-vermilion" };
    case "error":
      return { lamp: "needs_you", word: "Error", tone: "text-vermilion" };
    case "stopped":
      return { lamp: "stopped", word: "Stopped", tone: "text-ink-3" };
    default:
      return { lamp: "stopped", word: status.replace(/_/g, " "), tone: "text-ink-3" };
  }
}

// A connector attached to a Bot; a built-in one signs in with its own settings,
// not an authorization.
export function connectorLook(status: string, builtin?: boolean): StatusLook {
  switch (status) {
    case "authorized":
      return { lamp: "online", word: builtin ? "Signed in" : "Authorized", tone: "text-ink-3" };
    case "needs_auth":
      return { lamp: "needs_you", word: builtin ? "Needs setup" : "Needs authorization", tone: "text-vermilion" };
    case "initializing":
      return { lamp: "starting", word: "Initializing", tone: "text-ink-3" };
    case "error":
      return { lamp: "needs_you", word: "Error", tone: "text-vermilion" };
    default:
      return { lamp: "stopped", word: "Ready", tone: "text-ink-3" };
  }
}

// A mounted drive.
export function driveLook(state: string): StatusLook {
  switch (state) {
    case "mounted":
      return { lamp: "online", word: "Mounted", tone: "text-emerald" };
    case "mounting":
      return { lamp: "starting", word: "Connecting…", tone: "text-ink-2" };
    case "needs_auth":
      return { lamp: "needs_you", word: "Reconnect needed", tone: "text-vermilion" };
    case "needs_setup":
      return { lamp: "needs_you", word: "Waiting for an admin", tone: "text-vermilion" };
    case "needs_input":
      return { lamp: "needs_you", word: "Needs details", tone: "text-vermilion" };
    case "error":
      return { lamp: "needs_you", word: "Error", tone: "text-vermilion" };
    default:
      return { lamp: "stopped", word: "Stopped", tone: "text-ink-3" };
  }
}

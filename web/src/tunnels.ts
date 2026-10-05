// Pure helpers for the Tunnels tab (kept out of the component so node can test
// them): the add form's port check and the notice for an unusable state.

// Ports the Bot's machine uses for itself; the CP and the worker refuse them too.
const RESERVED: Record<number, string> = {
  5900: "the Bot's desktop (x11vnc)",
  9222: "Chromium's debugging port",
};

export function parsePort(raw: string): { port: number } | { error: string } {
  const s = raw.trim();
  if (s === "") return { error: "Enter the port the service listens on." };
  if (!/^\d+$/.test(s)) return { error: "The port must be a number." };
  const port = Number(s);
  if (port < 1) return { error: "Ports start at 1." };
  if (port > 65535) return { error: "Ports go up to 65535." };
  if (RESERVED[port]) return { error: `Port ${port} is ${RESERVED[port]}, which cannot be tunnelled.` };
  return { port };
}

// The ListTunnels state, as words. admin says whether to offer the settings link.
export function tunnelNotice(state: string, isAdmin: boolean): { text: string; admin: boolean } | null {
  if (state === "off") return { text: "Tunnels are turned off by the operator (tunnels.enabled).", admin: isAdmin };
  if (state === "no_host") return { text: "Tunnels need a domain: the operator sets tunnels.host, the suffix tunnels are served under.", admin: isAdmin };
  return null;
}

// The two ways a tunnel can be opened, as the card and the add form say them.
const ACCESS = {
  private: { label: "Private", caption: "Only you, signed in to Silo, can open it" },
  public: { label: "Public", caption: "Anyone with the link can open it, no sign-in" },
} as const;

export function accessOf(isPublic: boolean) {
  return isPublic ? ACCESS.public : ACCESS.private;
}

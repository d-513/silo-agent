// Pure helpers for the Mail page (kept out of the component so node can test
// them): the notice for an unusable state, a sender's short name, the wake
// list as the owner typed it, and the link to a message's original.

// The Mailbox.state, as words. admin says whether to offer the settings link.
export function mailNotice(state: string, isAdmin: boolean): { text: string; admin: boolean } | null {
  if (state === "off") return { text: "Mail is turned off by the operator (mail.enabled).", admin: isAdmin };
  if (state === "no_domain") return { text: "Mail needs a domain: the operator sets mail.domain, the part of every Bot's address after the @.", admin: isAdmin };
  return null;
}

// The name a From header shows in a list: the display name, else the address.
export function senderName(from: string, address: string): string {
  const m = /^\s*"?([^"<]*?)"?\s*<[^>]*>\s*$/.exec(from);
  const name = m?.[1].trim();
  return name || address || from.trim() || "Unknown sender";
}

// What is wrong with a wake list, in the form's words; "" when it can be saved.
// The server checks again; this only saves the round trip.
export function wakeListError(on: boolean, raw: string): string {
  const entries = raw
    .split(/[\n,;]/)
    .map((s) => s.trim())
    .filter(Boolean);
  if (on && entries.length === 0) return "Name at least one sender to wake for.";
  for (const e of entries) {
    const domain = e.includes("@") ? e.slice(e.lastIndexOf("@") + 1) : e;
    if (/\s/.test(e) || e.split("@").length > 2 || !/^[^.\s]+(\.[^.\s]+)+$/.test(domain)) {
      return `“${e}” is not an address or a domain. Write ada@example.com or @example.com.`;
    }
  }
  return "";
}

export function rawMailHref(botId: string, id: string): string {
  return `/mail/raw?bot_id=${encodeURIComponent(botId)}&id=${encodeURIComponent(id)}`;
}

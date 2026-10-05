import type { ChannelAdapter } from "../gen/silo/v1/ui_pb";

// What each built-in adapter is good for, under its card in the picker (like a
// featured connector's points). An adapter not listed shows none.
const points: Record<string, string[]> = {
  telegram: ["Reads the chat's history", "Groups: every message or only a mention", "Files arrive as real uploads"],
  whatsapp: ["Links as a device on your own account", "Message yourself to talk to the Bot", "Use a spare number"],
  discord: ["Server channels and direct messages", "Answers an @mention or a reply", "Never pings @everyone"],
};

export function adapterPoints(slug: string): string[] {
  return points[slug] ?? [];
}

// Short facts about how an adapter signs in and what it binds to, derived from
// what it declares, so a new adapter gets chips without a change here.
export function adapterTraits(a: ChannelAdapter): string[] {
  const out: string[] = [];
  if (a.actions?.some((x) => x.kind === "qr")) out.push("QR login");
  else if (a.fields?.some((f) => f.secret && f.required)) out.push("Bot token");
  if (a.requiresTarget) out.push("One chat");
  return out;
}

// Adapters matching the search text, by name, slug or description.
export function filterAdapters(adapters: ChannelAdapter[], search: string): ChannelAdapter[] {
  const q = search.trim().toLowerCase();
  if (!q) return adapters;
  return adapters.filter((a) => `${a.name}\n${a.slug}\n${a.description}`.toLowerCase().includes(q));
}

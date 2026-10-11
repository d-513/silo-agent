import type { ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";

// What each built-in adapter is good for, under its card in the picker (like a
// featured connector's points). An adapter not listed shows none.
const points: Record<string, string[]> = {
  telegram: [msg("Reads the chat's history"), msg("Groups: every message or only a mention"), msg("Files arrive as real uploads")],
  whatsapp: [msg("Links as a device on your own account"), msg("Message yourself to talk to the Bot"), msg("Use a spare number")],
  discord: [msg("Server channels and direct messages"), msg("Answers an @mention or a reply"), msg("Never pings @everyone")],
};

export function adapterPoints(slug: string): string[] {
  return (points[slug] ?? []).map((p) => t(p));
}

// Short facts about how an adapter signs in and what it binds to, derived from
// what it declares, so a new adapter gets chips without a change here.
export function adapterTraits(a: ChannelAdapter): string[] {
  const out: string[] = [];
  if (a.actions?.some((x) => x.kind === "qr")) out.push(t("QR login"));
  else if (a.fields?.some((f) => f.secret && f.required)) out.push(t("Bot token"));
  if (a.requiresTarget) out.push(t("One chat"));
  return out;
}

// Adapters matching the search text, by name, slug or description.
export function filterAdapters(adapters: ChannelAdapter[], search: string): ChannelAdapter[] {
  const q = search.trim().toLowerCase();
  if (!q) return adapters;
  return adapters.filter((a) => `${a.name}\n${a.slug}\n${a.description}`.toLowerCase().includes(q));
}

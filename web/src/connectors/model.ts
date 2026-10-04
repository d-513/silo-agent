import type { BotConnector, Connector } from "../gen/silo/v1/ui_pb";

export type TabMode = "in_use" | "library" | "custom";

// Library presets shown in the Featured band, by their `builtin` key.
export const FEATURED = ["email", "calendar"];

// Short selling points under a featured card; falls back to none.
export const featuredPoints: Record<string, string[]> = {
  email: ["Any IMAP / SMTP inbox", "Gmail & Outlook presets", "Sends ask you first"],
  calendar: ["iCloud, Fastmail, Nextcloud & any CalDAV", "Recurring events & free/busy", "Changes ask you first"],
};

// A connector the owner wrote, not a copy of a library preset.
export function isCustom(c: Connector) {
  return c.kind === "custom" && !c.sourceId;
}

// "GitHub", then "GitHub 2", "GitHub 3": a name no attached copy uses yet.
export function nextCopyName(base: string, names: string[]) {
  const used = new Set(names.map((n) => n.trim().toLowerCase()));
  const stem = base.trim();
  if (!used.has(stem.toLowerCase())) return stem;
  for (let n = 2; n < 10000; n++) {
    const cand = `${stem} ${n}`;
    if (!used.has(cand.toLowerCase())) return cand;
  }
  return `${stem} ${names.length + 1}`;
}

export const categoryOf = (c: Connector) => c.category?.trim() || "General";

// Distinct categories in the library, sorted.
export function categoriesOf(catalog: Connector[]) {
  return Array.from(new Set(catalog.map(categoryOf))).sort();
}

// The library filtered by the picked category and the search text.
export function filterCatalog(catalog: Connector[], search: string, selectedCategory: string) {
  const q = search.trim().toLowerCase();
  return catalog.filter((c) => {
    const cat = categoryOf(c);
    if (selectedCategory !== "all" && cat.toLowerCase() !== selectedCategory.toLowerCase()) return false;
    if (!q) return true;
    return c.name.toLowerCase().includes(q) || c.description.toLowerCase().includes(q) || cat.toLowerCase().includes(q) || c.transport.toLowerCase().includes(q);
  });
}

// Attached connectors matching the search text.
export function filterAttached(attached: BotConnector[], search: string) {
  const q = search.trim().toLowerCase();
  if (!q) return attached;
  return attached.filter((r) => {
    const c = r.connector;
    if (!c) return false;
    return c.name.toLowerCase().includes(q) || c.description.toLowerCase().includes(q) || (c.category && c.category.toLowerCase().includes(q)) || r.authStatus.toLowerCase().includes(q);
  });
}

export function groupByCategory(list: Connector[]) {
  const map = new Map<string, Connector[]>();
  for (const c of list) {
    const cat = categoryOf(c);
    const items = map.get(cat) ?? [];
    items.push(c);
    map.set(cat, items);
  }
  return map;
}

// How many copies of a library connector are attached (by source id or name).
export function attachedCount(attached: BotConnector[], sourceIdOrName: string) {
  return attached.filter((r) => {
    const c = r.connector;
    if (!c) return false;
    return c.sourceId === sourceIdOrName || c.name.toLowerCase() === sourceIdOrName.toLowerCase();
  }).length;
}

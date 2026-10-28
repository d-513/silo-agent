import type { Connector } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";

// Library presets shown in the Featured band, by their `builtin` key.
export const FEATURED = ["email", "calendar"];

// Short selling points under a featured card; falls back to none.
export const featuredPoints: Record<string, string[]> = {
  email: [msg("Any IMAP / SMTP inbox"), msg("Gmail & Outlook presets"), msg("Sends ask you first")],
  calendar: [msg("iCloud, Fastmail, Nextcloud & any CalDAV"), msg("Recurring events & free/busy"), msg("Changes ask you first")],
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

export const categoryOf = (c: Connector) => c.category?.trim() || t("General");

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

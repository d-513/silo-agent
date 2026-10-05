// Skills matching the search text, by name, description or source.
export function filterSkills<T extends { name: string; description: string; source?: string }>(rows: T[], search: string): T[] {
  const q = search.trim().toLowerCase();
  if (!q) return rows;
  return rows.filter((r) => `${r.name}\n${r.description}\n${r.source ?? ""}`.toLowerCase().includes(q));
}

// A skill's install source for a card: a GitHub URL shrinks to owner/repo, any
// other URL loses its scheme, and anything else (a catalog or upload note) is
// shown as it is.
export function sourceLabel(source: string): string {
  const s = source.trim();
  const gh = s.match(/^(?:https?:\/\/)?(?:www\.)?github\.com\/([^/\s]+)\/([^/\s#?]+)/i);
  if (gh) return `${gh[1]}/${gh[2].replace(/\.git$/, "")}`;
  return s.replace(/^https?:\/\//i, "");
}

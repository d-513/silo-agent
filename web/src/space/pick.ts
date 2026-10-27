// Which Workspace the console is in. Pure, so node can test it (pick_test.ts).

/** The localStorage key of the Workspace last opened in this browser. */
export const spaceKey = "silo.space";

/**
 * The Workspace to show: the one the page names (a /w/ route or the open
 * Bot's), else the one last opened if it is still listed, else the first.
 * Empty while the list has not come.
 */
export function pickSpace(ids: readonly string[], named: string | undefined, last: string | null): string {
  if (named) return named;
  if (last && ids.includes(last)) return last;
  return ids[0] ?? "";
}

/** The letters a tile shows for a name, as the server makes them (space.Mark). */
export function markOf(name: string, given = ""): string {
  const letters = (s: string, n: number) =>
    Array.from(s)
      .filter((c) => /[\p{L}\p{N}]/u.test(c))
      .slice(0, n)
      .join("")
      .toUpperCase();
  const own = letters(given, 2);
  if (own) return own;
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length >= 2) {
    const two = letters(words[0], 1) + letters(words[1], 1);
    if (two) return two;
  }
  return letters(name, 2) || "W";
}

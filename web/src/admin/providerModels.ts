// Pure helpers for a provider's model list (kept out of the component so node
// can test them).

/** A full id without its provider prefix: "openrouter/openai/gpt" → "openai/gpt". */
export function bareModel(id: string): string {
  const i = id.indexOf("/");
  return i < 0 ? id : id.slice(i + 1);
}

/** A context window in tokens as a short word: 400000 → "400k", 1000000 → "1M". */
export function fmtContext(tokens: number): string {
  if (!(tokens > 0)) return "";
  const trim = (n: number) => String(Math.round(n * 10) / 10);
  if (tokens >= 1_000_000) return `${trim(tokens / 1_000_000)}M`;
  if (tokens >= 1000) return `${Math.round(tokens / 1000)}k`;
  return String(tokens);
}

/** The models whose id or name holds every word of the query, in the list's order. */
export function filterModels<T extends { id: string; name: string }>(list: T[], query: string): T[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return list;
  return list.filter((m) => {
    const hay = `${m.id} ${m.name}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}

/** The allowlist with `id` added at the end, or taken out; unchanged if there is nothing to do. */
export function withModel(allowed: string[], id: string, on: boolean): string[] {
  const has = allowed.includes(id);
  if (on) return has ? allowed : [...allowed, id];
  return has ? allowed.filter((m) => m !== id) : allowed;
}

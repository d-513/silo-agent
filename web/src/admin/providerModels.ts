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

/** The two kinds of model. A text model writes; a decision model answers typed questions. */
export type ModelKind = "text" | "decision";

export const KINDS: { id: ModelKind; label: string }[] = [
  { id: "text", label: "Text" },
  { id: "decision", label: "Decision" },
];

/** A listed model's kind; anything a provider does not mark as a decision model is text. */
export function kindOf(m: { kind?: string }): ModelKind {
  return m.kind === "decision" ? "decision" : "text";
}

/** The kinds a list holds, text first. */
export function kindsIn(list: { kind?: string }[]): ModelKind[] {
  return KINDS.map((k) => k.id).filter((k) => list.some((m) => kindOf(m) === k));
}

/** A role's picker rows when it takes either kind: decision models first, each saying what it is. */
export function eitherKind<T extends { id: string; label: string }>(text: T[], decision: T[]): { value: string; label: string; hint: string }[] {
  const rows = (list: T[], hint: string) => list.map((m) => ({ value: m.id, label: m.label || m.id, hint }));
  return [...rows(decision, "Decision model"), ...rows(text, "Text model")];
}

/** The allowlist with `id` added at the end, or taken out; unchanged if there is nothing to do. */
export function withModel(allowed: string[], id: string, on: boolean): string[] {
  const has = allowed.includes(id);
  if (on) return has ? allowed : [...allowed, id];
  return has ? allowed.filter((m) => m !== id) : allowed;
}

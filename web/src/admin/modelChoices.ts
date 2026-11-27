import type { ModelKind } from "./providerModels.ts";

// What a job's picker lists, as pure functions node can test.

/** One model a job could be given. */
export type Choice = {
  id: string;
  // The provider's own name for it, "" when it has none.
  name: string;
  kind: ModelKind;
  provider: string;
  live: boolean;
  words: boolean;
  endOfTurn: boolean;
  // Its context window in tokens and how many voices it has, 0 when unknown.
  context: number;
  voices: number;
  // On its kind's allowlist already.
  allowed: boolean;
};

/** Whether a model's id or name holds every word of the query. */
export function matches(m: { id: string; name: string }, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const hay = `${m.id} ${m.name}`.toLowerCase();
  return words.every((w) => hay.includes(w));
}

/**
 * The choice for an id typed by hand that no list holds: provider/model of a
 * provider that serves a kind the job takes. Its kind is what the server
 * takes an unlisted id for: text where its provider has text models, else
 * the first of the job's kinds the provider serves. null when the text is no
 * such id, or a list already has it.
 */
export function customChoice(query: string, kinds: ModelKind[], providers: { id: string; kinds: string[] }[], known: { id: string }[]): Choice | null {
  const id = query.trim();
  const cut = id.indexOf("/");
  if (cut <= 0 || cut === id.length - 1 || /\s/.test(id)) return null;
  if (known.some((m) => m.id === id)) return null;
  const p = providers.find((x) => x.id === id.slice(0, cut));
  if (!p) return null;
  const kind = kinds.find((k) => k === "text" && p.kinds.includes(k)) ?? kinds.find((k) => p.kinds.includes(k));
  if (!kind) return null;
  return { id, name: "", kind, provider: p.id, live: false, words: false, endOfTurn: false, context: 0, voices: 0, allowed: false };
}

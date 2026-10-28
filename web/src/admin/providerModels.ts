import { t } from "../i18n.ts";

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

/** The kinds of model (the server's llm.Kinds), in the order Admin shows them. */
export type ModelKind = "text" | "decision" | "transcription" | "speech" | "embedding";

export const KINDS: ModelKind[] = ["text", "decision", "transcription", "speech", "embedding"];

/** A listed model's kind; anything a provider does not mark is text. */
export function kindOf(m: { kind?: string }): ModelKind {
  return (KINDS as string[]).includes(m.kind ?? "") ? (m.kind as ModelKind) : "text";
}

/** The kinds a list holds, in registry order. */
export function kindsIn(list: { kind?: string }[]): ModelKind[] {
  return KINDS.filter((k) => list.some((m) => kindOf(m) === k));
}

/** A kind as a short word for a tab. */
export function kindWord(k: ModelKind): string {
  switch (k) {
    case "decision":
      return t("Decision", { context: "kind" });
    case "transcription":
      return t("Speech-to-text", { context: "kind" });
    case "speech":
      return t("Speech", { context: "kind" });
    case "embedding":
      return t("Embedding", { context: "kind" });
    default:
      return t("Text", { context: "kind" });
  }
}

/** The filter box's placeholder for a kind's list. */
export function filterWord(k: ModelKind, count: number): string {
  switch (k) {
    case "decision":
      return t("Filter {{count}} decision models", { count });
    case "transcription":
      return t("Filter {{count}} speech-to-text models", { count });
    case "speech":
      return t("Filter {{count}} speech models", { count });
    case "embedding":
      return t("Filter {{count}} embedding models", { count });
    default:
      return t("Filter {{count}} text models", { count });
  }
}

/** The title of the button that takes a model off its kind's list. */
export function removeTitle(k: ModelKind): string {
  switch (k) {
    case "decision":
      return t("Remove from the allowed decision models");
    case "transcription":
      return t("Remove from the allowed speech-to-text models");
    case "speech":
      return t("Remove from the allowed speech models");
    case "embedding":
      return t("Remove from the allowed embedding models");
    default:
      return t("Remove from the allowed text models");
  }
}

/** A role's picker rows when it takes either kind: decision models first, each saying what it is. */
export function eitherKind<T extends { id: string; label: string }>(text: T[], decision: T[]): { value: string; label: string; hint: string }[] {
  const rows = (list: T[], hint: string) => list.map((m) => ({ value: m.id, label: m.label || m.id, hint }));
  return [...rows(decision, t("Decision model")), ...rows(text, t("Text model"))];
}

/** The allowlist with `id` added at the end, or taken out; unchanged if there is nothing to do. */
export function withModel(allowed: string[], id: string, on: boolean): string[] {
  const has = allowed.includes(id);
  if (on) return has ? allowed : [...allowed, id];
  return has ? allowed.filter((m) => m !== id) : allowed;
}

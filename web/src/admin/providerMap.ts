import { msg } from "../i18n.ts";

// The model map, as pure functions node can test: which provider
// does each job (following each setting's fallbacks the way the server
// does), whether a provider is set up, and what a provider can do per kind.

/** The provider of a provider/model id ("" for none). */
export function providerOf(id: string): string {
  const i = id.indexOf("/");
  return i > 0 ? id.slice(0, i) : "";
}

/** The model part of a provider/model id. */
export function modelOf(id: string): string {
  const i = id.indexOf("/");
  return i > 0 ? id.slice(i + 1) : id;
}

export type Job = {
  key: string;
  label: string;
  // The Models page where it is set.
  page: JobPage;
  // The model doing it, "" when nobody does (off or unset).
  model: string;
  // inherited names the setting it follows when its own is unset.
  inherited: string;
  // A sentence for a job nobody does.
  idle: string;
};

export type JobPage = "text" | "decision" | "embedding" | "voice";

type JobDef = { key: string; label: string; page: JobPage; idle?: string };

// The jobs a model is given, in the order the map reads: a conversation
// first, then a call, then the quieter ones.
export const JOBS: JobDef[] = [
  { key: "model", label: msg("Chat"), page: "text" },
  { key: "model_voice", label: msg("Talker"), page: "voice", idle: msg("The Bot answers every turn") },
  { key: "voice.transcribe_model", label: msg("Hears calls"), page: "voice", idle: msg("Off") },
  { key: "voice.speech_model", label: msg("Speaks"), page: "voice", idle: msg("No voice") },
  { key: "transcribe_model", label: msg("Dictation"), page: "voice", idle: msg("Off") },
  { key: "model_approval", label: msg("Auto-approval"), page: "decision" },
  { key: "model_decision", label: msg("Decisions"), page: "decision", idle: msg("No decision model") },
  { key: "embedding_model", label: msg("Embeddings"), page: "embedding" },
];

// What a setting left empty follows, nearest first, as the server reads it.
export const FOLLOWS: Record<string, string[]> = {
  model_title: ["model"],
  model_approval: ["model_title", "model"],
  model_filter: ["model_approval", "model_title", "model"],
  model_memory: ["model_title", "model"],
  "voice.transcribe_model": ["transcribe_model"],
};

/**
 * The model a setting uses: its own value, else the first set one it follows
 * (from names that one), else for the decision model the first allowed one
 * (from = "decision_models"). "" when nobody does it; "off" reads as nobody.
 */
export function effective(values: Record<string, string>, key: string, decisionModels: string[] = []): { model: string; from: string } {
  const own = (k: string) => {
    const v = (values[k] ?? "").trim();
    return v.toLowerCase() === "off" ? "off" : v;
  };
  let model = own(key);
  let from = "";
  if (!model) {
    for (const f of FOLLOWS[key] ?? []) {
      const v = own(f);
      if (v) {
        model = v;
        from = f;
        break;
      }
    }
  }
  if (!model && key === "model_decision" && decisionModels.length) {
    model = decisionModels[0];
    from = "decision_models";
  }
  return { model: model === "off" ? "" : model, from };
}

/** What each job is done with, given the form's values and the allowed decision models. */
export function resolveJobs(values: Record<string, string>, decisionModels: string[]): Job[] {
  return JOBS.map((j) => {
    const { model, from } = effective(values, j.key, decisionModels);
    return { key: j.key, label: j.label, page: j.page, model, inherited: from, idle: j.idle ?? msg("Not set") };
  });
}

/** Providers in the order the map lists them: by the first job they do. */
export function providersInUse(jobs: Job[]): string[] {
  const out: string[] = [];
  for (const j of jobs) {
    const p = providerOf(j.model);
    if (p && !out.includes(p)) out.push(p);
  }
  return out;
}

type Field = { key: string; value: string; source: number };

/** Whether a provider has what it needs to be asked: a key (a local server: its address). */
export function isReady(providerID: string, fields: Field[], values: Record<string, string>, envSource: number): boolean {
  const has = (k: string) => {
    const key = `providers.${providerID}.${k}`;
    const f = fields.find((x) => x.key === key);
    return !!(values[key] ?? f?.value ?? "").trim() || f?.source === envSource;
  };
  return providerID === "local" ? has("base_url") : has("api_key");
}

export type Cap = { served: boolean; live: boolean; allowed: number };

/** What a provider can do for one kind: serve it, serve it live, and how many of its models are allowed. */
export function capOf(p: { id: string; kinds: string[]; liveKinds: string[] }, kind: string, allowed: string[], values: Record<string, string>): Cap {
  const served = p.kinds.includes(kind);
  // A local server streams only once its settings say it speaks Realtime.
  let live = served && p.liveKinds.includes(kind);
  if (live && p.id === "local") live = !!(values["providers.local.realtime"] ?? "").trim();
  return { served, live, allowed: allowed.filter((m) => providerOf(m) === p.id).length };
}

/** A provider's two-letter mark: initials of its name ("Open Router" → "OR", "Deepgram" → "DE"). */
export function markOf(name: string): string {
  const caps = name.replace(/[^A-Za-z0-9 ]/g, "").match(/[A-Z]/g) ?? [];
  if (caps.length >= 2) return (caps[0] + caps[1]).toUpperCase();
  return name.slice(0, 2).toUpperCase();
}

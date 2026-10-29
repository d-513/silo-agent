import { msg } from "../i18n.ts";

// The Providers page's map, as pure functions node can test: which provider
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
  // The model doing it, "" when nobody does (off or unset).
  model: string;
  // inherited names the setting it follows when its own is unset.
  inherited: string;
  // A sentence for a job nobody does.
  idle: string;
};

type JobDef = { key: string; label: string; follows?: string[]; idle?: string };

// The jobs a model is given, in the order the map reads: a conversation
// first, then a call, then the quieter ones.
export const JOBS: JobDef[] = [
  { key: "model", label: msg("Chat") },
  { key: "model_voice", label: msg("Talker"), idle: msg("The Bot answers every turn") },
  { key: "voice.transcribe_model", label: msg("Hears calls"), follows: ["transcribe_model"], idle: msg("Off") },
  { key: "voice.speech_model", label: msg("Speaks"), idle: msg("No voice") },
  { key: "transcribe_model", label: msg("Dictation"), idle: msg("Off") },
  { key: "model_approval", label: msg("Auto-approval"), follows: ["model_title", "model"] },
  { key: "model_decision", label: msg("Decisions"), idle: msg("No decision model") },
  { key: "embedding_model", label: msg("Embeddings") },
];

/** What each job is done with, given the form's values and the allowed decision models. */
export function resolveJobs(values: Record<string, string>, decisionModels: string[]): Job[] {
  const own = (key: string) => {
    const v = (values[key] ?? "").trim();
    return v.toLowerCase() === "off" ? "off" : v;
  };
  return JOBS.map((j) => {
    let model = own(j.key);
    let inherited = "";
    if (!model) {
      for (const f of j.follows ?? []) {
        const v = own(f);
        if (v) {
          model = v;
          inherited = f;
          break;
        }
      }
    }
    if (!model && j.key === "model_decision" && decisionModels.length) {
      model = decisionModels[0];
      inherited = "decision_models";
    }
    if (model === "off") model = "";
    return { key: j.key, label: j.label, model, inherited, idle: j.idle ?? msg("Not set") };
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

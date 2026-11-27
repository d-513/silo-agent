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

export type JobGroup = "chat" | "judges" | "voice" | "tools";

/** The groups the jobs are listed under, in the map's order. */
export const JOB_GROUPS: { id: JobGroup; label: string }[] = [
  { id: "chat", label: msg("Conversations") },
  { id: "judges", label: msg("Judges") },
  { id: "voice", label: msg("Voice") },
  { id: "tools", label: msg("Tools") },
];

/** The kinds of model a job takes; "either" is a decision model or a text model. */
export type Takes = "text" | "decision" | "transcription" | "speech" | "embedding" | "image" | "either";

// A job is a setting that names a model: what it is for, the kind of model
// it takes, and what its empty choice means.
export type JobDef = {
  key: string;
  // Its short name, on the map and in an allowlist's "used by" line.
  label: string;
  group: JobGroup;
  takes: Takes;
  // What the job is, in a sentence or two.
  hint: string;
  // The empty choice, in the picker's words, and its value when that is not "".
  unset: string;
  unsetValue?: string;
  // What the map says where nobody does the job.
  idle: string;
};

// Every job a model is given, in the order the map reads: a conversation
// first, then who rules on it, then a call, then the tools.
export const JOBS: JobDef[] = [
  {
    key: "model",
    label: msg("Chat"),
    group: "chat",
    takes: "text",
    hint: msg("What a chat writes with, unless its Bot or the chat picks another allowed text model."),
    unset: msg("None: each Bot or chat has to pick one"),
    idle: msg("Not set"),
  },
  {
    key: "model_title",
    label: msg("Titles"),
    group: "chat",
    takes: "text",
    hint: msg("Names a chat from its first message. A small, fast model is enough."),
    unset: msg("Same as Chat"),
    idle: msg("Not set"),
  },
  {
    key: "model_subagent",
    label: msg("Subagents"),
    group: "chat",
    takes: "text",
    hint: msg("Default for subagents a lead starts. A cheaper model keeps parallel work affordable; the lead can still pick another."),
    unset: msg("Same as the lead"),
    idle: msg("Same as the lead"),
  },
  {
    key: "model_memory",
    label: msg("Memory"),
    group: "chat",
    takes: "text",
    hint: msg("Reads chats once they go quiet and saves the facts the Bot missed. A cheap model is enough."),
    unset: msg("Same as Titles"),
    idle: msg("Not set"),
  },
  {
    key: "model_approval",
    label: msg("Auto-approval"),
    group: "judges",
    takes: "either",
    hint: msg("Decides rules set to Auto. A decision model answers in a fraction of a second and only a sure yes approves; a text model is asked for one word."),
    unset: msg("Same as Titles"),
    idle: msg("Not set"),
  },
  {
    key: "model_filter",
    label: msg("Output filter"),
    group: "judges",
    takes: "either",
    hint: msg("Judges tool results for rules set to Check, once per result. A decision model is the fast, cheap choice; a text model is asked for one word."),
    unset: msg("Same as Auto-approval"),
    idle: msg("Not set"),
  },
  {
    key: "model_voice",
    label: msg("Talker"),
    group: "voice",
    takes: "text",
    hint: msg("A fast text model that holds the conversation on a call and hands real work to the Bot, so answers start in under a second. Without one, the Bot answers every turn itself: smarter, slower."),
    unset: msg("None: the Bot answers every turn"),
    idle: msg("The Bot answers every turn"),
  },
  {
    key: "transcribe_model",
    label: msg("Dictation"),
    group: "voice",
    takes: "transcription",
    hint: msg("Hears what you say: dictation in the composer, the transcribe tool, and calls unless Hears calls names another."),
    unset: msg("Off: no dictation and no calls"),
    unsetValue: "off",
    idle: msg("Off"),
  },
  {
    key: "voice.transcribe_model",
    label: msg("Hears calls"),
    group: "voice",
    takes: "transcription",
    hint: msg("Hears every turn of a call. A live model hears you as you speak and has the words ready when you stop; dictation can stay on a cheaper one."),
    unset: msg("Same as Dictation"),
    idle: msg("Off"),
  },
  {
    key: "voice.speech_model",
    label: msg("Speaks"),
    group: "voice",
    takes: "speech",
    hint: msg("The voice a Bot speaks with unless its own Settings pick another allowed speech model."),
    unset: msg("None: no Read aloud and no calls"),
    idle: msg("No voice"),
  },
  {
    key: "model_decision",
    label: msg("Decisions"),
    group: "tools",
    takes: "decision",
    hint: msg("What the Bot's decide tool asks unless it names another allowed decision model."),
    unset: msg("None: no decide tool"),
    idle: msg("No decision model"),
  },
  {
    key: "embedding_model",
    label: msg("Embeddings"),
    group: "tools",
    takes: "embedding",
    hint: msg("For memories and indexed documents. OpenAI-compatible, 1536-wide; switching models makes old memories match poorly (documents are re-embedded on the next sync)."),
    unset: msg("None: no memories and no document search"),
    idle: msg("No embedding model"),
  },
  {
    key: "model_image",
    label: msg("Images"),
    group: "tools",
    takes: "image",
    hint: msg("What a Bot's generate_image tool asks unless it names another allowed image model. Each image is billed by its provider."),
    unset: msg("None: no image generation"),
    idle: msg("No image model"),
  },
];

/** The kinds of model a job's picker offers, the first being what an id nobody lists is taken for. */
export function kindsOf(takes: Takes): ("text" | "decision" | "transcription" | "speech" | "embedding" | "image")[] {
  return takes === "either" ? ["decision", "text"] : [takes];
}

/** A job's short name by its setting ("" for a setting that is no job). */
export function jobName(key: string): string {
  return JOBS.find((j) => j.key === key)?.label ?? "";
}

// A job as the map draws it: its definition, and what does it now.
export type Job = JobDef & {
  // The model doing it, "" when nobody does (off or unset).
  model: string;
  // inherited names the setting it follows when its own is unset.
  inherited: string;
  // Its own value: "" while it follows another setting or nobody does it.
  own: string;
};

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
 * (from names that one). "" when nobody does it; "off" reads as nobody.
 */
export function effective(values: Record<string, string>, key: string): { model: string; from: string } {
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
  return { model: model === "off" ? "" : model, from };
}

/** What each job is done with, given the form's values. */
export function resolveJobs(values: Record<string, string>): Job[] {
  return JOBS.map((j) => {
    const { model, from } = effective(values, j.key);
    return { ...j, model, inherited: from, own: from ? "" : model };
  });
}

/** The jobs whose own setting names this model: what a removal would leave without one. */
export function jobsUsing(values: Record<string, string>, id: string): JobDef[] {
  return JOBS.filter((j) => (values[j.key] ?? "").trim() === id);
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

/** Which sign-in providers have an account to work with: what a key is to the others. */
export function accountsOf(providers: { id: string; signIn: boolean; signedIn: boolean }[]): Record<string, boolean> {
  return Object.fromEntries(providers.filter((p) => p.signIn).map((p) => [p.id, p.signedIn]));
}

/**
 * Whether a provider has what it needs to be asked: a key (a local server: its
 * address), or for one the operator signs in to (accountsOf), an account.
 */
export function isReady(providerID: string, fields: Field[], values: Record<string, string>, envSource: number, accounts: Record<string, boolean> = {}): boolean {
  if (providerID in accounts) return accounts[providerID];
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

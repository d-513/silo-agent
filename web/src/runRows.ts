import { msg } from "./i18n.ts";

// What the Runs page says about a row, with no React in it. The kinds, the
// triggers and the call labels are the server's (internal/app/run/kind.go,
// models.Call.Label); the words are ours.

// KINDS are the chips, in order; one shows only while some listed run is of
// its kind.
export const KINDS: { key: string; label: string }[] = [
  { key: "chat", label: msg("Chats") },
  { key: "channel", label: msg("Channels") },
  { key: "automation", label: msg("Automations") },
  { key: "job", label: msg("Housekeeping") },
  { key: "subagent", label: msg("Subagents") },
  { key: "wake", label: msg("Subagent reports") },
  { key: "bot", label: msg("From other Bots") },
  { key: "compact", label: msg("Compactions") },
  { key: "command", label: msg("Commands") },
  { key: "quote", label: msg("Quotes") },
  { key: "voice", label: msg("Calls") },
];

/** The word a row carries for its kind. An unknown kind is shown as it is. */
export function kindWord(kind: string, job: string): string {
  switch (kind) {
    case "chat":
      return msg("Chat");
    case "channel":
      return msg("Channel");
    case "automation":
      return msg("Automation");
    case "job":
      return job === "memory" ? msg("Memory keeper") : msg("Housekeeping");
    case "subagent":
      return msg("Subagent");
    case "wake":
      return msg("Subagent report");
    case "bot":
      return msg("From another Bot");
    case "compact":
      return msg("Compaction");
    case "command":
      return msg("Command");
    case "quote":
      return msg("Quote");
    case "voice":
      return msg("Spoken on a call");
    default:
      return kind;
  }
}

/** What started a run, where that says more than its kind does. */
export function triggerWord(trigger: string): string {
  switch (trigger) {
    case "spoken":
      return msg("spoken");
    case "mail":
      return msg("woken by a mail");
    case "onboarding":
      return msg("the interview");
    case "retry":
      return msg("a retry");
    default:
      return "";
  }
}

/** What a model call beside a run's turns was for. */
export function callWord(label: string): string {
  switch (label) {
    case "title":
      return msg("Chat title");
    case "compact":
      return msg("Compaction");
    case "approval":
      return msg("Auto-approval");
    case "filter":
      return msg("Output filter");
    case "decide":
      return msg("Decision");
    case "memory":
      return msg("Memory digest");
    case "voice-talk":
      return msg("Call talker");
    case "embed":
      return msg("Embedding");
    case "speech":
      return msg("Speech");
    case "dictation":
      return msg("Dictation");
    case "channel":
      return msg("Voice memo");
    case "call":
      return msg("Hearing a call");
    case "transcribe":
      return msg("Transcription");
    default:
      return label;
  }
}

/** How long something took: "0.4 s", "12 s", "3 min 05 s", "1 h 04 min"; "" for none. */
export function took(ms: number): string {
  if (!(ms > 0)) return "";
  if (ms < 950) return `${(ms / 1000).toFixed(1)} s`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s} s`;
  const two = (n: number) => String(n).padStart(2, "0");
  if (s < 3600) return `${Math.floor(s / 60)} min ${two(s % 60)} s`;
  return `${Math.floor(s / 3600)} h ${two(Math.floor((s % 3600) / 60))} min`;
}

/** A token count a row has room for: "950", "12.3k", "1.2M". */
export function tokens(n: number): string {
  if (n < 1000) return String(Math.round(n));
  if (n < 999_950) return `${(n / 1000).toFixed(n < 99_950 ? 1 : 0)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

/** Everything a run's models read and wrote, its side calls included. */
export function spent(s: { input: bigint; output: bigint; sideInput: bigint; sideOutput: bigint } | undefined): { read: number; wrote: number } {
  if (!s) return { read: 0, wrote: 0 };
  return { read: Number(s.input + s.sideInput), wrote: Number(s.output + s.sideOutput) };
}

/** The kinds some listed run is of, in chip order. */
export function kindsOf(rows: { kind: string }[]): string[] {
  const seen = new Set(rows.map((r) => r.kind));
  return KINDS.filter((k) => seen.has(k.key)).map((k) => k.key);
}

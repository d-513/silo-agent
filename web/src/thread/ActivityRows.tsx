import { Eraser, Lightbulb, Minimize2, Phone, PhoneOff, ShieldCheck, ShieldX, Sparkles, Trash2 } from "lucide-react";
import { callClock } from "../call/audio";
import type { Decision, ReceiptBlock } from "../fold";
import { Md } from "../Md";
import { FoldRow } from "./FoldRow";
import { msg, t } from "../i18n.ts";

const receiptWord: Record<Decision, string> = {
  allow_once: msg("Allowed once"),
  always: msg("Always allowed"),
  deny: msg("Denied"),
  stopped: msg("Stopped"),
};

// One line per decision: shield, verdict, "· action · target", a hairline, "by you".
export function Receipt({ b }: { b: ReceiptBlock }) {
  const allowed = b.decision === "allow_once" || b.decision === "always";
  const Icon = allowed ? ShieldCheck : ShieldX;
  return (
    <div className="flex min-w-0 items-center gap-2 text-[12.5px] leading-[18px]">
      <Icon size={15} className={`shrink-0 ${allowed ? "text-emerald" : "text-ink-3"}`} aria-hidden />
      <span className="shrink-0 font-medium text-ink">{receiptWord[b.decision] ? t(receiptWord[b.decision]) : b.decision}</span>
      <span className="min-w-0 truncate text-ink-3">
        {b.title ? ` · ${b.title}` : ""}
        {b.target ? (
          <>
            {" · "}
            <span className="font-mono">{b.target}</span>
          </>
        ) : null}
      </span>
      <span aria-hidden className="h-px min-w-6 flex-1 bg-line" />
      <span className="shrink-0 text-ink-3">{t("by you")}</span>
    </div>
  );
}

// Streams open under a shimmering "Thinking", then folds into "Thought for Ns".
export function Thinking({ text, streaming, ms }: { text: string; streaming: boolean; ms?: number }) {
  const secs = ms && ms >= 1000 ? Math.round(ms / 1000) : 0;
  return (
    <FoldRow
      live={streaming}
      lead={
        <span className="flex h-4 w-4 shrink-0 items-center justify-center text-ink-2" aria-hidden>
          <Lightbulb size={15} strokeWidth={1.75} />
        </span>
      }
      title={
        streaming ? (
          <span className="shimmer-text shrink-0 font-medium">{t("Thinking")}</span>
        ) : (
          <span className="shrink-0 font-medium text-ink-3">{secs ? t("Thought for {{seconds}}s", { seconds: secs }) : t("Thought")}</span>
        )
      }
    >
      {text.trim() ? <div className="whitespace-pre-wrap break-words text-[13px] leading-[21px] text-ink-2">{text}</div> : null}
    </FoldRow>
  );
}

// Compaction marks where the model's history was replaced by a summary. The
// thread above stays as it was; the summary is what the model sees of it.
export function Compaction({ text, reason, running }: { text: string; reason: string; running: boolean }) {
  const title = running ? t("Compacting context") : text ? t("Context compacted") : t("Compaction did not finish");
  return (
    <FoldRow
      live={running}
      lead={
        <span className="flex h-4 w-4 shrink-0 items-center justify-center text-ink-2" aria-hidden>
          <Minimize2 size={14} strokeWidth={1.75} />
        </span>
      }
      title={running ? <span className="shimmer-text shrink-0 font-medium">{title}</span> : <span className="shrink-0 font-medium text-ink-3">{title}</span>}
      tail={reason === "auto" && !running ? <span className="truncate text-ink-3">· {t("the context window was nearly full")}</span> : null}
    >
      {text.trim() ? (
        <div className="silo-reply text-[13px] leading-[21px] text-ink-2">
          <Md text={text} />
        </div>
      ) : null}
    </FoldRow>
  );
}

// Boundary marks where a slash command changed the conversation: /clear (the
// Bot's context starts over below it; everything above stays) or /wipe
// (messages were deleted for good).
export function Boundary({ kind, count }: { kind: "cleared" | "wiped"; count: number }) {
  const label = kind === "cleared" ? t("Context cleared") : t("{{count}} messages wiped", { count });
  const hint = kind === "cleared" ? t("The Bot does not see anything above this line.") : t("Deleted for good by you.");
  return (
    <div className="flex items-center gap-3 pt-4 pb-1" title={hint}>
      <span aria-hidden className="h-px flex-1 bg-line" />
      <span className="flex items-center gap-1.5 text-[12px] leading-4 font-medium text-ink-3">
        {kind === "cleared" ? <Eraser size={12} /> : <Trash2 size={12} />}
        {label}
      </span>
      <span aria-hidden className="h-px flex-1 bg-line" />
    </div>
  );
}

// CallMark is where a voice call began, and where it ended with how long it
// ran. What was said sits between them as ordinary messages.
export function CallMark({ edge, seconds }: { edge: "start" | "end"; seconds: number }) {
  return (
    <div className="flex items-center gap-3 pt-3 pb-1">
      <span aria-hidden className="h-px flex-1 bg-line" />
      <span className="flex items-center gap-1.5 text-[12px] leading-4 font-medium text-ink-3">
        {edge === "start" ? <Phone size={12} /> : <PhoneOff size={12} />}
        {edge === "start" ? t("Call started") : t("Call ended · {{length}}", { length: callClock(seconds * 1000) })}
      </span>
      <span aria-hidden className="h-px flex-1 bg-line" />
    </div>
  );
}

// InterviewMark opens an interview (a new Bot's, or /onboard): Silo started
// it, so its brief to the Bot is not shown as the owner's message.
export function InterviewMark() {
  return (
    <div className="flex items-center gap-3 pt-2 pb-1">
      <span aria-hidden className="h-px flex-1 bg-line" />
      <span className="flex items-center gap-1.5 text-[12px] leading-4 font-medium text-ink-3">
        <Sparkles size={12} />
        {t("Getting to know you")}
      </span>
      <span aria-hidden className="h-px flex-1 bg-line" />
    </div>
  );
}

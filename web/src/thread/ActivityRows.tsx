import { Lightbulb, Minimize2, ShieldCheck, ShieldX } from "lucide-react";
import type { Decision, ReceiptBlock } from "../fold";
import { Md } from "../Md";
import { FoldRow } from "./FoldRow";

const receiptWord: Record<Decision, string> = {
  allow_once: "Allowed once",
  always: "Always allowed",
  deny: "Denied",
  stopped: "Stopped",
};

// One line per decision: shield, verdict, "· action · target", a hairline, "by you".
export function Receipt({ b }: { b: ReceiptBlock }) {
  const allowed = b.decision === "allow_once" || b.decision === "always";
  const Icon = allowed ? ShieldCheck : ShieldX;
  return (
    <div className="flex min-w-0 items-center gap-2 text-[12.5px] leading-[18px]">
      <Icon size={15} className={`shrink-0 ${allowed ? "text-emerald" : "text-ink-3"}`} aria-hidden />
      <span className="shrink-0 font-medium text-ink">{receiptWord[b.decision] ?? b.decision}</span>
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
      <span className="shrink-0 text-ink-3">by you</span>
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
          <span className="shimmer-text shrink-0 font-medium">Thinking</span>
        ) : (
          <span className="shrink-0 font-medium text-ink-3">{secs ? `Thought for ${secs}s` : "Thought"}</span>
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
  const title = running ? "Compacting context" : text ? "Context compacted" : "Compaction did not finish";
  return (
    <FoldRow
      live={running}
      lead={
        <span className="flex h-4 w-4 shrink-0 items-center justify-center text-ink-2" aria-hidden>
          <Minimize2 size={14} strokeWidth={1.75} />
        </span>
      }
      title={running ? <span className="shimmer-text shrink-0 font-medium">{title}</span> : <span className="shrink-0 font-medium text-ink-3">{title}</span>}
      tail={reason === "auto" && !running ? <span className="truncate text-ink-3">· the context window was nearly full</span> : null}
    >
      {text.trim() ? (
        <div className="silo-reply text-[13px] leading-[21px] text-ink-2">
          <Md text={text} />
        </div>
      ) : null}
    </FoldRow>
  );
}

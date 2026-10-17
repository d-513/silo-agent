import { BrainCircuit, Check } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Tip, TipAction, TipTitle } from "./Tip";
import type { CollectMemoriesResponse } from "./gen/silo/v1/ui_pb";
import { t } from "./i18n.ts";

type State = { kind: "idle" } | { kind: "running" } | { kind: "done"; res: CollectMemoriesResponse; fresh: boolean } | { kind: "error"; message: string };

// collectSummary is the one-line result of a collection.
export function collectSummary(res: Pick<CollectMemoriesResponse, "saved" | "updated" | "forgotten" | "note">): string {
  const parts: string[] = [];
  if (res.saved) parts.push(t("Saved {{n}}", { n: res.saved }));
  if (res.updated) parts.push(t("Updated {{n}}", { n: res.updated }));
  if (res.forgotten) parts.push(t("Forgot {{n}}", { n: res.forgotten }));
  if (parts.length) return parts.join(" · ");
  if (res.note === "nothing new") return t("Nothing new since the last save");
  if (res.note) return res.note[0].toUpperCase() + res.note.slice(1);
  return t("Nothing worth keeping");
}

/**
 * MemoryCollectButton reads the chat since its last collection and saves the
 * facts the Bot missed. It breathes while the model reads, then
 * shows a count badge for a moment; the tip keeps the last result.
 */
export function MemoryCollectButton({ chatId, onCollect }: { chatId?: string; onCollect: () => Promise<CollectMemoriesResponse> }) {
  const [state, setState] = useState<State>({ kind: "idle" });
  const chatRef = useRef(chatId);
  chatRef.current = chatId;

  // A result belongs to the chat it was read from.
  useEffect(() => setState({ kind: "idle" }), [chatId]);

  // The badge fades after a moment; the tip keeps the result.
  useEffect(() => {
    if (state.kind !== "done" || !state.fresh) return;
    const t = window.setTimeout(() => setState((s) => (s.kind === "done" ? { ...s, fresh: false } : s)), 2600);
    return () => window.clearTimeout(t);
  }, [state]);

  const running = state.kind === "running";
  const unavailable = !chatId;

  async function run() {
    if (running || unavailable) return;
    const from = chatId;
    setState({ kind: "running" });
    try {
      const res = await onCollect();
      if (chatRef.current === from) setState({ kind: "done", res, fresh: true });
    } catch (e) {
      const m = e instanceof Error ? e.message.replace(/^\[[^\]]+\]\s*/, "") : "failed";
      if (chatRef.current === from) setState({ kind: "error", message: m });
    }
  }

  const changed = state.kind === "done" ? state.res.saved + state.res.updated : 0;
  const aside = running ? t("Reading…") : state.kind === "done" ? collectSummary(state.res) : state.kind === "error" ? t("Failed") : undefined;

  return (
    <Tip
      closeOnClick={false}
      content={
        <>
          <TipTitle aside={aside}>{t("Save memories")}</TipTitle>
          <p className="mt-1">
            {running
              ? t("Reading this chat for facts worth keeping.")
              : t("Reads what was said since the last save and keeps the facts the Bot missed. Chats are also read on their own once they have been quiet for 10 minutes.")}
          </p>
          {state.kind === "done" && state.res.memories.length > 0 ? (
            <ul className="mt-2 grid gap-1">
              {state.res.memories.slice(0, 4).map((m) => (
                <li key={m.id} className="line-clamp-2 text-[12.5px]">
                  {m.content}
                </li>
              ))}
            </ul>
          ) : null}
          {state.kind === "error" ? <p className="mt-1 text-vermilion">{state.message}</p> : null}
          {unavailable ? null : running ? null : <TipAction>{t("Click to read this chat now")}</TipAction>}
        </>
      }
    >
      <button
        type="button"
        aria-label={t("Save memories from this chat")}
        aria-busy={running}
        aria-disabled={unavailable || running}
        className={`relative flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-control transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well active:scale-[.94] active:duration-[70ms] aria-disabled:cursor-not-allowed ${
          running ? "text-cobalt" : unavailable ? "text-ink-3 opacity-50" : "text-ink-3 hover:text-ink"
        }`}
        onClick={() => void run()}
      >
        {state.kind === "done" && state.fresh ? <span aria-hidden className="ping pointer-events-none absolute inset-[13px] rounded-full bg-cobalt" /> : null}
        <BrainCircuit size={16} className={running ? "breathe" : undefined} />
        {state.kind === "done" && state.fresh ? (
          <span className="pop absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-cobalt px-1 text-[10px] font-semibold text-on-accent">
            {changed > 0 ? `+${changed}` : <Check size={10} strokeWidth={3} />}
          </span>
        ) : null}
      </button>
    </Tip>
  );
}

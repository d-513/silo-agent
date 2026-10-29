import { useEffect, useRef, useState } from "react";
import type { CollectMemoriesResponse } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";

export type CollectState =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "done"; res: CollectMemoriesResponse; fresh: boolean }
  | { kind: "error"; message: string };

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

// useMemoryCollect runs the memory collector over a chat: it reads the chat
// since its last collection and saves the facts the Bot missed. A result is
// "fresh" for a moment after it lands (the trigger's badge); it belongs to the
// chat it was read from.
export function useMemoryCollect(chatId: string | undefined, onCollect: (() => Promise<CollectMemoriesResponse>) | undefined) {
  const [state, setState] = useState<CollectState>({ kind: "idle" });
  const chatRef = useRef(chatId);
  chatRef.current = chatId;

  useEffect(() => setState({ kind: "idle" }), [chatId]);

  useEffect(() => {
    if (state.kind !== "done" || !state.fresh) return;
    const timer = window.setTimeout(() => setState((s) => (s.kind === "done" ? { ...s, fresh: false } : s)), 2600);
    return () => window.clearTimeout(timer);
  }, [state]);

  async function run() {
    if (state.kind === "running" || !chatId || !onCollect) return;
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

  return { state, run, available: !!chatId && !!onCollect };
}

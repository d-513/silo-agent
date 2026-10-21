// foldEvents turns a conversation's raw events into the blocks the thread draws.
// Each kind of event has a small handler; this is the loop that feeds them.
import * as approvals from "./approvals.ts";
import * as lifecycle from "./lifecycle.ts";
import * as text from "./text.ts";
import * as tools from "./tools.ts";
import type { Block, Ev } from "./types.ts";

export * from "./types.ts";

const handlers: Record<string, text.Handler> = {
  user: text.user,
  subagent_report: text.subagentReport,
  feed_quote: text.feedQuote,
  card: text.card,
  card_state: text.cardState,
  setup_offer: text.offer,
  setup_state: text.offerState,
  thinking_chunk: text.thinkingChunk,
  thinking: text.thinking,
  chunk: text.chunk,
  assistant: text.assistant,
  section: text.assistant,
  section_live: text.assistant,
  tool_args_chunk: tools.toolArgsChunk,
  tool: tools.tool,
  tool_chunk: tools.toolChunk,
  tool_result: tools.toolResult,
  call: tools.call,
  call_result: tools.callResult,
  filtered: tools.filtered,
  approval: approvals.approval,
  decision: approvals.decision,
  context_cleared: lifecycle.boundary,
  wiped: lifecycle.boundary,
  compacting: lifecycle.compacting,
  compaction: lifecycle.compaction,
  done: lifecycle.done,
  artifact: lifecycle.artifact,
  error: lifecycle.error,
};

// An old "no API key" error the operator has since fixed.
const staleKey = /openrouter api key|set openrouter|silo_openrouter|api key in admin/i;

// first is the key index of events[0]: a page loaded in front of a thread
// starts below zero, so the blocks already drawn keep their keys.
export function foldEvents(events: Ev[], first = 0): Block[] {
  const out: Block[] = [];
  let i = first;
  for (const e of events) {
    if (e.kind === "chat_title" || e.kind === "usage") continue;
    if (e.kind === "error" && staleKey.test(e.body)) continue;
    // Every event uses up a key, even a kind with no handler.
    const key = `${e.kind}-${i++}`;
    handlers[e.kind]?.(out, e, key);
  }
  return out;
}

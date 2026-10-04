import type { Block, CompactionBlock, ToolBlock } from "./types.ts";

// Lookups over the blocks folded so far: which row a later event belongs to.

// runOk: a row belongs to a run when either side does not name one, or both agree.
export function runOk(b: ToolBlock, runId?: string) {
  return !runId || !b.runId || b.runId === runId;
}

// isPartialArgs: a tool's arguments that are still streaming (not yet JSON).
export function isPartialArgs(args: string) {
  if (!args.trim()) return true;
  try {
    JSON.parse(args);
    return false;
  } catch {
    return true;
  }
}

export function lastRunning(out: Block[], pred: (b: ToolBlock) => boolean): ToolBlock | undefined {
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type === "tool" && b.running && pred(b)) return b;
  }
}

export function lastThinking(out: Block[]) {
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type === "thinking") return b;
  }
}

// closeThinking ends the live thinking block, recording how long it ran.
export function closeThinking(out: Block[], at?: number) {
  const t = lastThinking(out);
  if (t && t.streaming) {
    t.streaming = false;
    if (t.startAt && at) t.ms = at - t.startAt;
  }
}

export function lastOf<T>(xs: T[] | undefined, pred: (x: T) => boolean): T | undefined {
  for (let j = (xs?.length ?? 0) - 1; j >= 0; j--) if (pred(xs![j])) return xs![j];
}

// waitingTool is the tool row paused on an approval: the one with that id, or
// any paused one.
export function waitingTool(out: Block[], runId?: string, approvalId?: string): ToolBlock | undefined {
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type !== "tool" || !runOk(b, runId)) continue;
    if (approvalId ? b.approvalId === approvalId : b.waiting) return b;
  }
}

export function runningCompaction(out: Block[], runId?: string): CompactionBlock | undefined {
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type === "compaction" && b.running && (!runId || !b.runId || b.runId === runId)) return b;
  }
}

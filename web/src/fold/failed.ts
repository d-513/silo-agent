import type { Ev } from "./types.ts";

// lastReplyFailed says the chat's last reply ended in an error (or the
// server went down under it) and nothing has wiped the slate since, so
// Retry can answer it again. It mirrors the server's check (RetryRun):
// the newest run that ended is what counts, a manual compaction is not a
// reply, and the rows a command writes end no run.
export function lastReplyFailed(events: Ev[]): boolean {
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];
    // A /clear or /wipe after the failure leaves nothing to answer.
    if (e.kind === "context_cleared" || e.kind === "wiped") return false;
    if (e.kind !== "done") continue;
    const run = e.runId;
    if (run && events.some((x) => x.runId === run && x.kind === "compacting" && x.tool === "manual")) continue;
    return e.body === "error" || e.body === "interrupted";
  }
  return false;
}

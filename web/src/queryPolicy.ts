import { isTransient } from "./errors.ts";

// queryPolicy.ts is the part of the query layer with no React and no generated
// code in it: when to retry, how fast to poll. It is unit tested with node.

/** Retry only a call that never reached a working CP, and only twice. */
export function retryable(failures: number, e: unknown): boolean {
  return failures < 2 && isTransient(e);
}

/** A Bot's row is polled fast until the box settles (online or stopped). */
export function botPollMs(bot: { workerConnected: boolean; status: string } | undefined): number {
  return bot && (bot.workerConnected || bot.status === "stopped") ? 3000 : 500;
}

/**
 * A lead chat hears about its subagents through stream pings, so it only polls
 * while one runs (the tray's activity lines move). A subagent's own log has no
 * pings and polls the board slowly.
 */
export function subagentPollMs(agents: { running: boolean }[], lead: boolean): number | false {
  if (agents.some((a) => a.running)) return 2500;
  return lead ? false : 5000;
}

/** The list with `row` in place of the one sharing its id; unknown ids are not added. */
export function putRow<T extends { id: string }>(list: T[] | undefined, row: T): T[] | undefined {
  return list?.map((x) => (x.id === row.id ? row : x));
}

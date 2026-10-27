import { useQuery } from "@connectrpc/connect-query";
import { useParams } from "@tanstack/react-router";
import { useEffect } from "react";
import { useBots } from "../bots";
import { fail } from "../errors";
import { UI, type Space } from "../gen/silo/v1/ui_pb";
import { pickSpace, spaceKey } from "./pick.ts";

// useSpaces is the Workspaces the user is in. Listing makes their first one,
// so it is never empty once it answers.
export function useSpaces() {
  const q = useQuery(UI.method.listSpaces, {}, { refetchInterval: 30_000 });
  return { spaces: q.data?.spaces ?? null, err: q.error ? fail(q.error) : "" };
}

function lastSpace(): string | null {
  try {
    return localStorage.getItem(spaceKey);
  } catch {
    return null;
  }
}

/** Forget the remembered Workspace (it was deleted). */
export function forgetSpace(id: string) {
  try {
    if (localStorage.getItem(spaceKey) === id) localStorage.removeItem(spaceKey);
  } catch {
    // Storage off: nothing was remembered.
  }
}

/**
 * useCurrentSpace is the Workspace the console is in: the one a /w/ page
 * names, else the open Bot's, else the last one opened here, else the first.
 * It is remembered for the next visit.
 */
export function useCurrentSpace(): { space: Space | undefined; spaces: Space[] | null } {
  const { spaces } = useSpaces();
  const { bots } = useBots();
  const params = useParams({ strict: false }) as { spaceId?: string; botId?: string };
  const botSpace = params.botId ? bots?.find((b) => b.id === params.botId)?.spaceId : undefined;
  const ids = (spaces ?? []).map((s) => s.id);
  const id = pickSpace(ids, params.spaceId || botSpace, lastSpace());
  const space = spaces?.find((s) => s.id === id);
  useEffect(() => {
    if (!space) return;
    try {
      localStorage.setItem(spaceKey, space.id);
    } catch {
      // Storage off: the console starts in the first Workspace next time.
    }
  }, [space]);
  return { space, spaces };
}

/** canAdmin: the role may rename the Workspace and, later, manage its people. */
export function canAdmin(role: string | undefined) {
  return role === "owner" || role === "admin";
}

/** canBuild: the role may make Bots and use the hub (everyone but a guest). */
export function canBuild(role: string | undefined) {
  return !!role && role !== "guest";
}

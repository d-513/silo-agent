import type { DescMessage, DescMethodUnary, MessageInitShape, MessageShape } from "@bufbuild/protobuf";
import { createConnectQueryKey } from "@connectrpc/connect-query";
import { QueryClient } from "@tanstack/react-query";
import { transport } from "./api";
import { UI, type Bot } from "./gen/silo/v1/ui_pb";
import { putRow, retryable } from "./queryPolicy";

// query.ts is the one cache of what the server said. Components read it with
// connect-query's useQuery(UI.method.x, input) and never keep their own copy of
// a row or their own fetch timer; whoever changes something writes the answer
// here (or invalidates), and every reader follows.
//
// A poll pauses while the tab is hidden and catches up on focus.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: retryable },
    mutations: { retry: false },
  },
});

/** The cache key of one call. Without `input` it matches every call of that method. */
export function key<I extends DescMessage, O extends DescMessage>(method: DescMethodUnary<I, O>, input?: MessageInitShape<I>) {
  return createConnectQueryKey({ schema: method, input, transport, cardinality: "finite" });
}

/** Ask again: every query of `method` (narrowed to `input` when given) refetches. */
export function reload<I extends DescMessage, O extends DescMessage>(method: DescMethodUnary<I, O>, input?: MessageInitShape<I>) {
  return queryClient.invalidateQueries({ queryKey: key(method, input) });
}

/** Like reload, but a load that is already out is left to finish (for "the page was opened" rereads). */
export function recheck<I extends DescMessage, O extends DescMessage>(method: DescMethodUnary<I, O>, input?: MessageInitShape<I>) {
  return queryClient.invalidateQueries({ queryKey: key(method, input) }, { cancelRefetch: false });
}

/** Write the cached answer of one call, from the previous one. */
export function patch<I extends DescMessage, O extends DescMessage>(
  method: DescMethodUnary<I, O>,
  input: MessageInitShape<I>,
  update: (prev: MessageShape<O>) => MessageShape<O>,
) {
  queryClient.setQueryData(key(method, input), (prev: MessageShape<O> | undefined) => (prev ? update(prev) : prev));
}

/** Store the answer of one call that a save just returned. */
export function put<I extends DescMessage, O extends DescMessage>(method: DescMethodUnary<I, O>, input: MessageInitShape<I>, data: MessageShape<O>) {
  queryClient.setQueryData(key(method, input), data);
}

/** A Bot row the server just returned: the Bot page and the rail both get it. */
export function setBot(bot: Bot) {
  queryClient.setQueryData(key(UI.method.getBot, { id: bot.id }), bot);
  patch(UI.method.listBots, {}, (r) => ({ ...r, bots: putRow(r.bots, bot) ?? r.bots }));
}

/** Something happened to a Bot (a run started, a box moved): reread its row and the rail. */
export function reloadBot(id: string) {
  void reload(UI.method.getBot, { id });
  void reload(UI.method.listBots);
}

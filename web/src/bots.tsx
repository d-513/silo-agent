import { useQuery } from "@connectrpc/connect-query";
import { fail } from "./errors";
import { UI } from "./gen/silo/v1/ui_pb";

// useBots is every Bot the user can see, for the sidebar. It shares the query
// cache with the open Bot's own row (query.ts `setBot`). callEnabled: a call
// can work here, so a Bot's row offers Call.
export function useBots() {
  const q = useQuery(UI.method.listBots, {}, { refetchInterval: 4000 });
  return { bots: q.data?.bots ?? null, callEnabled: q.data?.callEnabled ?? false, err: q.error ? fail(q.error) : "" };
}

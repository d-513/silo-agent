import { useQuery } from "@connectrpc/connect-query";
import { useEffect } from "react";
import { fail } from "../errors";
import { UI, type BotConnector, type Connector } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";

const noRows: BotConnector[] = [];
const noCatalog: Connector[] = [];

// The Bot's attached connectors and the library, polled: quickly while any is
// still initializing, slowly otherwise.
export function useBotConnectors(botId: string, onError: (message: string) => void) {
  const attachedQ = useQuery(
    UI.method.listBotConnectors,
    { botId },
    { refetchInterval: (q) => (q.state.data?.connectors.some((r) => r.authStatus === "initializing") ? 1000 : 3000) },
  );
  const catalogQ = useQuery(UI.method.listConnectors, {}, { refetchInterval: 3000 });

  // Only a first load that fails is worth a banner; a later blip keeps the rows.
  const firstErr = (!attachedQ.data && attachedQ.error) || (!catalogQ.data && catalogQ.error) || null;
  useEffect(() => {
    if (firstErr) onError(fail(firstErr));
  }, [firstErr]);

  async function load() {
    await Promise.all([reload(UI.method.listBotConnectors, { botId }), reload(UI.method.listConnectors)]);
  }

  return { attached: attachedQ.data?.connectors ?? noRows, catalog: catalogQ.data?.connectors ?? noCatalog, load };
}

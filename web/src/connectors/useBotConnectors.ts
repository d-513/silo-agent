import { useEffect, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import type { BotConnector, Connector } from "../gen/silo/v1/ui_pb";

// The Bot's attached connectors and the library, polled: quickly while any is
// still initializing, slowly otherwise.
export function useBotConnectors(botId: string, onError: (message: string) => void) {
  const [attached, setAttached] = useState<BotConnector[]>([]);
  const [catalog, setCatalog] = useState<Connector[]>([]);
  const hasInit = attached.some((r) => r.authStatus === "initializing");

  async function load() {
    const [a, c] = await Promise.all([ui.listBotConnectors({ botId }), ui.listConnectors({})]);
    setAttached(a.connectors);
    setCatalog(c.connectors);
  }

  useEffect(() => {
    load().catch((e) => onError(fail(e)));
    const t = setInterval(() => load().catch(() => {}), hasInit ? 1000 : 3000);
    return () => clearInterval(t);
  }, [botId, hasInit]);

  return { attached, catalog, load };
}

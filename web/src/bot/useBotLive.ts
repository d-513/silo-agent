import { useCallback, useEffect, useState } from "react";
import { ui } from "../api";
import { fail, isGone } from "../errors";
import type { Approval, Bot } from "../gen/silo/v1/ui_pb";

// useBotLive keeps a Bot's row and its pending approvals current: polled fast
// until the box settles (online or stopped), slowly after. Only a Bot that is
// gone (deleted, not ours) ends the page; a blip keeps the last row and the
// next tick recovers.
export function useBotLive(id: string | undefined, chatId: string | undefined) {
  const [bot, setBot] = useState<Bot | null>(null);
  const [loadErr, setLoadErr] = useState("");
  const [pending, setPending] = useState<Approval[]>([]);

  useEffect(() => {
    setLoadErr("");
  }, [id]);

  const settled = Boolean(bot && (bot.workerConnected || bot.status === "stopped"));
  useEffect(() => {
    if (!id) return;
    let dead = false;
    const tick = () => {
      ui.getBot({ id })
        .then((b) => {
          if (dead) return;
          setBot(b);
          setLoadErr("");
        })
        .catch((e) => {
          if (dead || !isGone(e)) return;
          setLoadErr(fail(e));
          clearInterval(t);
        });
      ui.listApprovals({ botId: id })
        .then((r) => {
          if (!dead) setPending(r.approvals);
        })
        .catch(() => {});
    };
    const t = setInterval(tick, settled ? 3000 : 500);
    tick();
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [id, settled]);

  // Opening a chat looks for approvals right away instead of waiting for a tick.
  useEffect(() => {
    if (!id || !chatId) return;
    ui.listApprovals({ botId: id }).then((r) => setPending(r.approvals)).catch(() => {});
  }, [id, chatId]);

  const reloadApprovals = useCallback(() => {
    if (id) ui.listApprovals({ botId: id }).then((r) => setPending(r.approvals)).catch(() => {});
  }, [id]);

  return { bot, setBot, loadErr, pending, setPending, reloadApprovals };
}

import { useState } from "react";
import { ui } from "../api";
import { AskSlip } from "../dashboard/Asks";
import { fail } from "../errors";
import type { DashboardCard } from "../gen/silo/v1/ui_pb";
import { reloadBot } from "../query";

// ThreadCard is an ask shown in a chat: the same card as on the Dashboard,
// answered in place. The answer goes to this chat as the owner's turn; the
// card's new state comes back on the stream (card_state).
export function ThreadCard({ botId, card }: { botId: string; card: DashboardCard }) {
  const [err, setErr] = useState("");
  const guard = async (fn: () => Promise<unknown>) => {
    setErr("");
    try {
      await fn();
      reloadBot(botId);
    } catch (e) {
      setErr(fail(e));
    }
  };
  return (
    <div className="space-y-1.5">
      <AskSlip
        botId={botId}
        card={card}
        place="thread"
        onSubmit={(c, values) => guard(() => ui.submitDashboardCard({ botId, id: c.id, valuesJson: JSON.stringify(values) }))}
        onDismiss={(c, suppressKind) => guard(() => ui.dismissDashboardCard({ botId, id: c.id, suppressKind }))}
        onSetup={(c, index, outcome) => guard(() => ui.resolveCardSetup({ botId, id: c.id, index, outcome }))}
      />
      {err ? <p className="px-1 text-[12px] text-vermilion">{err}</p> : null}
    </div>
  );
}

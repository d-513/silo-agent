import { ui } from "../api";
import { ApprovalSlip, SlipPresence, answer, approveSetAuto } from "../Approval";
import { UI, type Approval, type Bot } from "../gen/silo/v1/ui_pb";
import { patch } from "../query";

// The right-hand "Needs you" slip: the oldest pending approval.
// A failed answer is said on the slip itself.
export function BotSlip({ bot, pending }: { bot: Bot; pending: Approval[] }) {
  // A decided approval leaves the slip at once; the next poll confirms it.
  const decided = (id: string) => patch(UI.method.listApprovals, { botId: bot.id }, (r) => ({ ...r, approvals: r.approvals.filter((a) => a.id !== id) }));
  return (
    <SlipPresence show={!!pending[0]}>
      {pending[0] ? (
        <ApprovalSlip
          key={pending[0].id}
          bot={bot}
          approval={pending[0]}
          onDecide={async (decision) => {
            const { id } = pending[0];
            await answer(() => ui.decideApproval({ id, decision }));
            decided(id);
          }}
          onAutoApprove={async () => {
            const ap = pending[0];
            await answer(() => approveSetAuto(bot.id, ap));
            decided(ap.id);
          }}
        />
      ) : null}
    </SlipPresence>
  );
}

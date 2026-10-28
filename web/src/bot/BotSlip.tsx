import { ui } from "../api";
import { ApprovalSlip, SlipPresence, approveSetAuto } from "../Approval";
import { fail } from "../errors";
import { UI, type Approval, type Bot } from "../gen/silo/v1/ui_pb";
import { patch } from "../query";

// The right-hand "Needs you" slip: the oldest pending approval.
export function BotSlip({ bot, pending, onError }: { bot: Bot; pending: Approval[]; onError: (message: string) => void }) {
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
            await ui.decideApproval({ id, decision });
            decided(id);
          }}
          onAutoApprove={async () => {
            try {
              await approveSetAuto(bot.id, pending[0]);
              decided(pending[0].id);
            } catch (e) {
              onError(fail(e));
            }
          }}
        />
      ) : null}
    </SlipPresence>
  );
}

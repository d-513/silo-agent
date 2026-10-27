import { ui } from "../api";
import { ApprovalSlip, ConnectorAuthSlip, SlipPresence, approveSetAuto } from "../Approval";
import { startConnectorAuth } from "../connectorAuth";
import { fail } from "../errors";
import { UI, type Approval, type Bot, type BotConnector } from "../gen/silo/v1/ui_pb";
import { patch } from "../query";

// The right-hand "Needs you" slip: the oldest pending approval, else the
// connector sign-in the Connectors tab asked for.
export function BotSlip({
  bot,
  pending,
  authPrompt,
  setAuthPrompt,
  onError,
}: {
  bot: Bot;
  pending: Approval[];
  authPrompt: BotConnector | null;
  setAuthPrompt: (c: BotConnector | null) => void;
  onError: (message: string) => void;
}) {
  // A decided approval leaves the slip at once; the next poll confirms it.
  const decided = (id: string) => patch(UI.method.listApprovals, { botId: bot.id }, (r) => ({ ...r, approvals: r.approvals.filter((a) => a.id !== id) }));
  return (
    <SlipPresence show={!!(pending[0] || authPrompt?.connector)}>
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
      ) : authPrompt?.connector ? (
        <ConnectorAuthSlip
          bot={bot}
          name={authPrompt.connector.name}
          onAuthorize={async () => {
            try {
              await startConnectorAuth({ botId: bot.id, id: authPrompt.id });
              setAuthPrompt(null);
            } catch (e) {
              onError(fail(e));
            }
          }}
          onLater={() => setAuthPrompt(null)}
        />
      ) : null}
    </SlipPresence>
  );
}

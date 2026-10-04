import { ui } from "../api";
import { ApprovalSlip, ConnectorAuthSlip, SlipPresence } from "../Approval";
import { startConnectorAuth } from "../connectorAuth";
import { fail } from "../errors";
import type { Approval, Bot, BotConnector } from "../gen/silo/v1/ui_pb";

// The right-hand "Needs you" slip: the oldest pending approval, else the
// connector sign-in the Connectors tab asked for.
export function BotSlip({
  bot,
  pending,
  setPending,
  authPrompt,
  setAuthPrompt,
  onError,
}: {
  bot: Bot;
  pending: Approval[];
  setPending: (f: (xs: Approval[]) => Approval[]) => void;
  authPrompt: BotConnector | null;
  setAuthPrompt: (c: BotConnector | null) => void;
  onError: (message: string) => void;
}) {
  return (
    <SlipPresence show={!!(pending[0] || authPrompt?.connector)}>
      {pending[0] ? (
        <ApprovalSlip
          key={pending[0].id}
          bot={bot}
          approval={pending[0]}
          onDecide={async (decision) => {
            await ui.decideApproval({ id: pending[0].id, decision });
            setPending((xs) => xs.slice(1));
          }}
          onAutoApprove={async () => {
            try {
              await ui.setRule({
                botId: bot.id,
                connector: pending[0].connector,
                action: pending[0].action,
                decision: "auto",
              });
              await ui.decideApproval({ id: pending[0].id, decision: "allow_once" });
              setPending((xs) => xs.slice(1));
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
              await startConnectorAuth(bot.id, authPrompt.id);
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

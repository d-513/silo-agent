import { useQuery } from "@connectrpc/connect-query";
import { useContext, useState } from "react";
import { ui } from "../api";
import { BotPageCtx } from "../bot/context";
import { Btn } from "../Btn";
import { ChannelForm } from "../channels/ChannelForm";
import { ChannelSetup } from "../channels/ChannelSetup";
import { ConnectorFields, draftFrom, specOf, type ConnectorDraft } from "../connectors/form";
import { nextCopyName } from "../connectors/model";
import { DriveForm } from "../drives/DriveForm";
import { fail } from "../errors";
import { Spinner } from "../Feedback";
import { ErrorWell } from "../Field";
import { UI, type Channel, type Connector, type SetupOffer } from "../gen/silo/v1/ui_pb";
import { Modal } from "../Modal";
import { reload } from "../query";
import { t } from "../i18n.ts";
import { splitPrefill } from "./prefill";

// SetupModal is a Bot's setup offer opened: Silo's own add form for that
// connector, channel or drive, with the values the Bot filled in, over the
// whole page (Modal portals it: never inside the card that opened it). The
// owner submits it; onDone says it was added.
export function SetupModal({ botId, offer, onClose, onDone }: { botId: string; offer: SetupOffer; onClose: () => void; onDone: () => void }) {
  const props = { botId, offer, onClose, onDone };
  return offer.kind === "connector" ? <ConnectorSetup {...props} /> : offer.kind === "channel" ? <ChannelSetupFlow {...props} /> : <DriveSetup {...props} />;
}

type Props = { botId: string; offer: SetupOffer; onClose: () => void; onDone: () => void };

function Missing({ onClose, what }: { onClose: () => void; what: string }) {
  return (
    <Modal onClose={onClose}>
      <p className="text-[13.5px] text-ink-2">{t("{{what}} is not available on this server any more.", { what })}</p>
    </Modal>
  );
}

function ConnectorSetup(props: Props) {
  const { botId, offer, onClose } = props;
  const catalogQ = useQuery(UI.method.listConnectors, {});
  const attachedQ = useQuery(UI.method.listBotConnectors, { botId });
  const preset = catalogQ.data?.connectors.find((c) => c.identifier === offer.target);
  if (catalogQ.isFetched && !preset) return <Missing onClose={onClose} what={offer.label} />;
  if (!preset || !attachedQ.data) return null;
  return <ConnectorSetupForm {...props} preset={preset} taken={attachedQ.data.connectors.map((x) => x.connector?.name ?? "")} />;
}

// The form starts from the offer once, when the catalog and the Bot's
// connectors are in: a later refetch must not rename the copy under the owner.
function seedDraft(preset: Connector, options: Record<string, string>, taken: string[]): ConnectorDraft {
  const d = draftFrom(preset);
  const { own, fields } = splitPrefill("connector", options);
  const plain = new Set(d.fields.filter((f) => !f.secret).map((f) => f.key));
  const config = { ...d.config };
  for (const [k, v] of Object.entries(fields)) if (plain.has(k)) config[k] = v;
  return {
    ...d,
    config,
    name: nextCopyName(own.name || preset.name, taken),
    defaultMode: own.default_mode || d.defaultMode,
    exposeCompletion: own.expose_completion ? own.expose_completion === "true" : d.exposeCompletion,
  };
}

function ConnectorSetupForm({ botId, offer, onClose, onDone, preset, taken }: Props & { preset: Connector; taken: string[] }) {
  const page = useContext(BotPageCtx);
  const [draft, setDraft] = useState<ConnectorDraft>(() => seedDraft(preset, offer.options, taken));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    // A second press while the first is out would add a second copy.
    if (busy) return;
    setBusy(true);
    setErr("");
    try {
      const row = await ui.createBotConnector({ botId, ...specOf(draft), sourceId: preset.id });
      void reload(UI.method.listBotConnectors, { botId });
      if (row.authStatus === "needs_auth" && row.connector?.auth === "oauth") page?.needAuth(row);
      onDone();
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal rise onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <ConnectorFields value={draft} onChange={setDraft} fromCatalog catalogGuide={preset.catalogGuide} />
        {err ? <ErrorWell className="mt-4">{err}</ErrorWell> : null}
        <div className="mt-6 flex items-center gap-2">
          <Btn kind="primary" type="submit" disabled={busy} icon={busy ? <Spinner size={12} tone="on-ink" /> : undefined}>
            {t("Add to Bot")}
          </Btn>
          <Btn kind="ghost" type="button" onClick={onClose}>
            {t("Cancel")}
          </Btn>
        </div>
      </form>
    </Modal>
  );
}

function ChannelSetupFlow({ botId, offer, onClose, onDone }: Props) {
  const adaptersQ = useQuery(UI.method.listChannelAdapters, {});
  const [made, setMade] = useState<Channel | null>(null);
  // Poll only once a channel was made: its setup (a QR, a target) moves on its own.
  const channelsQ = useQuery(UI.method.listBotChannels, { botId }, { enabled: !!made, refetchInterval: made ? 3000 : false });
  const adapter = adaptersQ.data?.adapters.find((a) => a.slug === offer.target);
  if (adaptersQ.isFetched && !adapter) return <Missing onClose={onClose} what={offer.label} />;
  if (!adapter) return null;
  // Once made, a channel that signs in or binds to a conversation is set up
  // here too, so it works when the sheet closes.
  const live = made ? (channelsQ.data?.channels.find((c) => c.id === made.id) ?? made) : null;
  const needsSetup = !!adapter.requiresTarget || adapter.actions.length > 0;
  return (
    <Modal size="page" onClose={made ? onDone : onClose}>
      {live && needsSetup ? (
        <>
          <ChannelSetup botId={botId} channel={live} adapter={adapter} onBack={onDone} onChanged={() => void reload(UI.method.listBotChannels, { botId })} />
          <div className="flex justify-end px-6 pb-5">
            <Btn kind="primary" onClick={onDone}>
              {t("Done")}
            </Btn>
          </div>
        </>
      ) : (
        <ChannelForm
          botId={botId}
          adapter={adapter}
          prefill={offer.options}
          onBack={onClose}
          onSaved={(ch) => {
            void reload(UI.method.listBotChannels, { botId });
            if (needsSetup) setMade(ch);
            else onDone();
          }}
        />
      )}
    </Modal>
  );
}

function DriveSetup({ botId, offer, onClose, onDone }: Props) {
  const templatesQ = useQuery(UI.method.listDriveTemplates, {});
  const drivesQ = useQuery(UI.method.listDrives, { botId, draftId: "" });
  const tpl = templatesQ.data?.templates.find((x) => x.key === offer.target);
  if (templatesQ.isFetched && !tpl) return <Missing onClose={onClose} what={offer.label} />;
  if (!tpl || !drivesQ.data) return null;
  const taken = new Set(drivesQ.data.drives.map((d) => d.name));
  return (
    <Modal size="page" onClose={onClose}>
      <DriveForm
        botId={botId}
        t={tpl}
        taken={taken}
        prefill={offer.options}
        onDone={onClose}
        onSaved={() => {
          void reload(UI.method.listDrives, { botId });
          onDone();
        }}
      />
    </Modal>
  );
}

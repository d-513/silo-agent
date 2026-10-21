import { useQuery } from "@connectrpc/connect-query";
import { X } from "lucide-react";
import { useContext, useState, type ReactNode } from "react";
import { ui } from "../api";
import { BotPageCtx } from "../bot/context";
import { Btn } from "../Btn";
import { ChannelForm } from "../channels/ChannelForm";
import { ChannelSetup } from "../channels/ChannelSetup";
import { ConnectorModal } from "../connectors/ConnectorModal";
import { ConnectorFields, draftFrom, specOf, type ConnectorDraft } from "../connectors/form";
import { nextCopyName } from "../connectors/model";
import type { SetupBlock } from "../dashboardLayout";
import { DriveForm } from "../drives/DriveForm";
import { fail } from "../errors";
import { ErrorWell } from "../Field";
import { UI, type Channel } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { t } from "../i18n.ts";

// SetupModal is a Bot's setup offer opened: Silo's own add form for that
// connector, channel or drive, with the Bot's plain fields filled in, over the
// page. The owner submits it; onDone says it was added.
export function SetupModal({ botId, offer, onClose, onDone }: { botId: string; offer: SetupBlock; onClose: () => void; onDone: () => void }) {
  if (offer.kind === "connector") return <ConnectorSetup botId={botId} offer={offer} onClose={onClose} onDone={onDone} />;
  if (offer.kind === "channel") return <ChannelSetupFlow botId={botId} offer={offer} onClose={onClose} onDone={onDone} />;
  return <DriveSetup botId={botId} offer={offer} onClose={onClose} onDone={onDone} />;
}

type Props = { botId: string; offer: SetupBlock; onClose: () => void; onDone: () => void };

// Sheet is the wide overlay the page-sized forms (channels, drives) sit in.
function Sheet({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-ink/30 px-4 py-[4vh] backdrop-blur-xs">
      <div className="rise relative w-full max-w-[1040px] rounded-card bg-surface p-2 shadow-slip">
        <button type="button" className="absolute top-4 right-4 z-10 rounded-md p-1 text-ink-2 transition-colors hover:text-ink" onClick={onClose} title={t("Close")}>
          <X size={18} />
        </button>
        {children}
      </div>
    </div>
  );
}

function Missing({ onClose, what }: { onClose: () => void; what: string }) {
  return (
    <ConnectorModal onClose={onClose}>
      <p className="text-[13.5px] text-ink-2">{t("{{what}} is not available on this server any more.", { what })}</p>
    </ConnectorModal>
  );
}

function ConnectorSetup({ botId, offer, onClose, onDone }: Props) {
  const catalogQ = useQuery(UI.method.listConnectors, {});
  const attachedQ = useQuery(UI.method.listBotConnectors, { botId });
  const page = useContext(BotPageCtx);
  const preset = catalogQ.data?.connectors.find((c) => c.identifier === offer.target);
  const [draft, setDraft] = useState<ConnectorDraft | null>(null);
  const [err, setErr] = useState("");
  if (catalogQ.isFetched && !preset) return <Missing onClose={onClose} what={offer.label} />;
  if (!preset || !attachedQ.data) return null;
  const value =
    draft ??
    (() => {
      const d = draftFrom(preset);
      const fields = new Set(d.fields.filter((f) => !f.secret).map((f) => f.key));
      const config = { ...d.config };
      for (const [k, v] of Object.entries(offer.prefill ?? {})) if (fields.has(k)) config[k] = v;
      return { ...d, config, name: nextCopyName(preset.name, attachedQ.data.connectors.map((x) => x.connector?.name ?? "")) };
    })();

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      const row = await ui.createBotConnector({ botId, ...specOf(value), sourceId: preset!.id });
      void reload(UI.method.listBotConnectors, { botId });
      if (row.authStatus === "needs_auth" && row.connector?.auth === "oauth") page?.needAuth(row);
      onDone();
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  return (
    <ConnectorModal rise onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <ConnectorFields value={value} onChange={setDraft} fromCatalog catalogGuide={preset.catalogGuide} />
        {err ? <ErrorWell className="mt-4">{err}</ErrorWell> : null}
        <div className="mt-6 flex items-center gap-2">
          <Btn kind="primary" type="submit">
            {t("Add to Bot")}
          </Btn>
          <Btn kind="ghost" type="button" onClick={onClose}>
            {t("Cancel")}
          </Btn>
        </div>
      </form>
    </ConnectorModal>
  );
}

function ChannelSetupFlow({ botId, offer, onClose, onDone }: Props) {
  const adaptersQ = useQuery(UI.method.listChannelAdapters, {});
  const channelsQ = useQuery(UI.method.listBotChannels, { botId }, { refetchInterval: 3000 });
  const [made, setMade] = useState<Channel | null>(null);
  const adapter = adaptersQ.data?.adapters.find((a) => a.slug === offer.target);
  if (adaptersQ.isFetched && !adapter) return <Missing onClose={onClose} what={offer.label} />;
  if (!adapter) return null;
  // Once made, a channel that signs in or binds to a conversation is set up
  // here too, so it works when the sheet closes.
  const live = made ? (channelsQ.data?.channels.find((c) => c.id === made.id) ?? made) : null;
  const needsSetup = !!adapter.requiresTarget || adapter.actions.length > 0;
  return (
    <Sheet onClose={made ? onDone : onClose}>
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
          prefill={offer.prefill}
          onBack={onClose}
          onSaved={(ch) => {
            void reload(UI.method.listBotChannels, { botId });
            if (needsSetup) setMade(ch);
            else onDone();
          }}
        />
      )}
    </Sheet>
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
    <Sheet onClose={onClose}>
      <DriveForm
        botId={botId}
        t={tpl}
        taken={taken}
        prefill={offer.prefill}
        onDone={onClose}
        onSaved={() => {
          void reload(UI.method.listDrives, { botId });
          onDone();
        }}
      />
    </Sheet>
  );
}

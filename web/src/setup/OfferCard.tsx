import { useQuery } from "@connectrpc/connect-query";
import { Check, Plus } from "lucide-react";
import { useState, type ReactNode } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { AdapterLogo } from "../channels/AdapterLogo";
import { ConnectorMark } from "../connectors/form";
import { DriveMark } from "../drives/DriveMark";
import { fail } from "../errors";
import { UI, type SetupOffer } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";
import { SetupModal } from "./SetupModal";

const kindWord: Record<string, string> = {
  connector: msg("Connector"),
  channel: msg("Channel"),
  drive: msg("Drive"),
};

// The form's own options, as the card names them.
const optionWord: Record<string, string> = {
  name: msg("Name"),
  default_mode: msg("Default rule"),
  expose_completion: msg("Also chat tools"),
  inbound: msg("Deliver messages to the Bot"),
  enabled: msg("Enabled"),
  prompt: msg("Prompt"),
  read_only: msg("Read-only"),
};

function shown(v: string): string {
  if (v === "true") return t("Yes");
  if (v === "false") return t("No");
  return v;
}

// What the catalog says about an offer's target: its mark, its blurb, and
// what its fields are called.
function useTarget(offer: SetupOffer): { mark: ReactNode; blurb: string; fieldLabel: (k: string) => string } {
  const connectors = useQuery(UI.method.listConnectors, {}, { enabled: offer.kind === "connector" }).data?.connectors;
  const adapters = useQuery(UI.method.listChannelAdapters, {}, { enabled: offer.kind === "channel" }).data?.adapters;
  const templates = useQuery(UI.method.listDriveTemplates, {}, { enabled: offer.kind === "drive" }).data?.templates;
  if (offer.kind === "connector") {
    const c = connectors?.find((x) => x.identifier === offer.target);
    return {
      mark: c ? <ConnectorMark id={c.id} hasImage={c.hasImage} size={44} /> : <span className="h-11 w-11 rounded-control bg-well" />,
      blurb: c?.description ?? "",
      fieldLabel: (k) => c?.fields.find((f) => f.key === k)?.label ?? k,
    };
  }
  if (offer.kind === "channel") {
    const a = adapters?.find((x) => x.slug === offer.target);
    return { mark: <AdapterLogo adapter={a} size={44} />, blurb: a?.description ?? "", fieldLabel: (k) => a?.fields.find((f) => f.key === k)?.label ?? k };
  }
  const d = templates?.find((x) => x.key === offer.target);
  return { mark: <DriveMark svg={d?.iconSvg} size={44} />, blurb: d?.blurb ?? "", fieldLabel: (k) => d?.vars.find((v) => v.key === k)?.label ?? k };
}

// OfferCard is a setup the Bot offers in the chat: the provider's mark and
// name, why the Bot wants it, what it filled in, and Set up (Silo's own form,
// over the page) or Not now. Either answer is told to the Bot. The offer's
// new state comes back on the stream (setup_state).
export function OfferCard({ botId, offer }: { botId: string; offer: SetupOffer }) {
  const target = useTarget(offer);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const filled = Object.entries(offer.options).sort(([a], [b]) => (a === "name" ? -1 : b === "name" ? 1 : a.localeCompare(b)));

  async function resolve(outcome: "done" | "skipped") {
    setBusy(true);
    setErr("");
    try {
      await ui.resolveSetupOffer({ botId, id: offer.id, outcome });
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <article className="w-full max-w-[460px] overflow-hidden rounded-card bg-surface shadow-card">
      <div className="flex items-start gap-3 p-3.5">
        <span className="shrink-0">{target.mark}</span>
        <div className="min-w-0 flex-1">
          <p className="text-[11.5px] leading-4 font-medium text-ink-3 uppercase">{t(kindWord[offer.kind] ?? kindWord.connector)}</p>
          <h3 className="truncate text-[15px] leading-5 font-medium text-ink">{offer.label}</h3>
          {target.blurb ? <p className="mt-0.5 line-clamp-2 text-[12.5px] leading-[18px] text-ink-3">{target.blurb}</p> : null}
        </div>
      </div>
      {offer.why ? <p className="px-3.5 pb-2.5 text-[13.5px] leading-5 text-ink-2">{offer.why}</p> : null}
      {filled.length ? (
        <dl className="mx-3.5 mb-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 rounded-control bg-well px-3 py-2 text-[12.5px] leading-[18px]">
          {filled.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-ink-3">{optionWord[k] ? t(optionWord[k]) : target.fieldLabel(k)}</dt>
              <dd className="truncate text-ink">{shown(v)}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      <div className="flex items-center gap-1.5 border-t border-line px-3.5 py-2.5">
        {offer.status === "done" ? (
          <span className="flex items-center gap-1 text-[12.5px] font-medium text-emerald">
            <Check size={13} /> {t("Added")}
          </span>
        ) : offer.status === "skipped" ? (
          <span className="text-[12.5px] text-ink-3">{t("Not now")}</span>
        ) : (
          <>
            <Btn kind="primary" size="sm" disabled={busy} icon={<Plus size={13} />} onClick={() => setOpen(true)}>
              {t("Set up")}
            </Btn>
            <Btn kind="ghost" size="sm" disabled={busy} onClick={() => void resolve("skipped")}>
              {t("Not now")}
            </Btn>
          </>
        )}
        {err ? <span className="ml-auto truncate text-[12px] text-vermilion">{err}</span> : null}
      </div>
      {open ? (
        <SetupModal
          botId={botId}
          offer={offer}
          onClose={() => setOpen(false)}
          onDone={() => {
            setOpen(false);
            void resolve("done");
          }}
        />
      ) : null}
    </article>
  );
}


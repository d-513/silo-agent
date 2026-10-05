import { ChevronRight, Plus, Radio } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import type { SubPage } from "../bot/context";
import { btnClass } from "../Btn";
import { fail } from "../errors";
import { ErrorWell } from "../Field";
import { UI, type Channel, type ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { PageHead, widePage } from "../PageHead";
import { reload } from "../query";
import { AdapterLogo } from "./AdapterLogo";
import { AdapterPicker } from "./AdapterPicker";
import { ChannelCard } from "./ChannelCard";
import { ChannelForm } from "./ChannelForm";
import { ChannelLog } from "./ChannelLog";
import { ChannelSetup } from "./ChannelSetup";

const noAdapters: ChannelAdapter[] = [];
const noChannels: Channel[] = [];

function Opening({ onBack }: { onBack: () => void }) {
  return (
    <div className={widePage}>
      <PageHead title="Channels" onBack={onBack} />
      <p className="text-ink-2 text-[13px]">Opening…</p>
    </div>
  );
}

// The Channels tab: the list, and the pages it opens (`at`: new, add an
// adapter, edit, setup). Browser back works because each page is a route.
export function BotChannels({ botId, at }: { botId: string; at: SubPage }) {
  const navigate = useNavigate();
  // The Bot can be given channels from a chat too, so the list is polled.
  const adaptersQ = useQuery(UI.method.listChannelAdapters, {});
  const channelsQ = useQuery(UI.method.listBotChannels, { botId }, { refetchInterval: 4000 });
  const adapters = adaptersQ.data?.adapters ?? noAdapters;
  const channels = channelsQ.data?.channels ?? noChannels;
  const loaded = adaptersQ.isFetched && channelsQ.isFetched;
  const failed = adaptersQ.error ?? channelsQ.error;
  const err = failed ? fail(failed) : "";
  const [log, setLog] = useState<Channel | null>(null);

  const refresh = () => reload(UI.method.listBotChannels, { botId });
  // Leaving a form rereads the list: it may have added or changed a channel.
  const back = () => {
    void refresh();
    void navigate({ to: "/bots/$botId/channels", params: { botId } });
  };
  const add = (adapter?: string) =>
    void navigate(adapter ? { to: "/bots/$botId/channels/new/$adapter", params: { botId, adapter } } : { to: "/bots/$botId/channels/new", params: { botId } });

  const counts = channels.reduce<Record<string, number>>((m, c) => ({ ...m, [c.adapter]: (m[c.adapter] ?? 0) + 1 }), {});

  if (at.view === "new") {
    return <AdapterPicker botId={botId} adapters={adapters} counts={counts} loaded={loaded} onBack={back} />;
  }

  if (at.view === "add") {
    const adapter = adapters.find((a) => a.slug === at.id);
    if (!adapter) return <Opening onBack={back} />;
    return <ChannelForm botId={botId} adapter={adapter} onBack={back} />;
  }

  if (at.view === "setup") {
    const channel = channels.find((c) => c.id === at.id);
    const adapter = channel && adapters.find((a) => a.slug === channel.adapter);
    if (!channel || !adapter) return <Opening onBack={back} />;
    return <ChannelSetup botId={botId} channel={channel} adapter={adapter} onBack={back} onChanged={() => void refresh()} />;
  }

  if (at.view === "edit") {
    const channel = channels.find((c) => c.id === at.id);
    const adapter = channel && adapters.find((a) => a.slug === channel.adapter);
    if (!channel || !adapter) return <Opening onBack={back} />;
    return <ChannelForm botId={botId} adapter={adapter} channel={channel} onBack={back} />;
  }

  return (
    <div className={widePage}>
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1.5">
          <div className="flex items-center gap-2.5">
            <h1 className="text-title text-ink">Channels</h1>
            {loaded ? <span className="rounded-full bg-well px-2 py-0.5 text-[11px] font-semibold text-ink-3">{channels.length}</span> : null}
          </div>
          <p className="max-w-3xl text-[13px] leading-relaxed text-ink-2">
            Channels let you talk to this Bot from chat apps like Telegram, WhatsApp or Discord. Each one binds an adapter to a single conversation: the Bot answers
            there, and can send to it from its tools.
          </p>
        </div>

        <button type="button" className={btnClass("primary")} onClick={() => add()}>
          <Plus size={15} />
          <span>Add channel</span>
        </button>
      </div>

      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}

      {!loaded ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:gap-5" aria-busy="true" aria-label="Loading">
          {[0, 1].map((i) => (
            <div key={i} className="skeleton h-[160px] rounded-card" />
          ))}
        </div>
      ) : channels.length === 0 ? (
        <div className="rounded-card border border-dashed border-line bg-surface px-6 py-12 text-center">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-well text-ink-3">
            <Radio size={28} />
          </div>
          <h3 className="text-base font-semibold text-ink">No channels yet</h3>
          <p className="mx-auto mt-1 max-w-md text-[13px] text-ink-2">Pick an app to start with. You can add more later, one conversation each.</p>
          {adapters.length > 0 ? (
            <div className="mx-auto mt-6 grid max-w-2xl grid-cols-1 gap-2.5 text-left sm:grid-cols-3">
              {adapters.map((a) => (
                <button
                  key={a.slug}
                  type="button"
                  className="group flex items-center gap-3 rounded-card bg-surface p-3 shadow-card transition-[box-shadow,transform] duration-[160ms] ease-quiet hover:-translate-y-px hover:shadow-float active:scale-[.995]"
                  onClick={() => add(a.slug)}
                >
                  <AdapterLogo adapter={a} size={36} />
                  <span className="min-w-0 flex-1 truncate text-[14px] font-medium text-ink">{a.name}</span>
                  <ChevronRight size={15} className="shrink-0 text-ink-3 transition-transform duration-[160ms] ease-quiet group-hover:translate-x-0.5" />
                </button>
              ))}
            </div>
          ) : null}
          <button type="button" className="mt-5 text-[12.5px] font-medium text-cobalt hover:underline" onClick={() => add()}>
            Compare channels
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:gap-5">
          {channels.map((c) => (
            <ChannelCard key={c.id} botId={botId} c={c} a={adapters.find((x) => x.slug === c.adapter)} onLog={() => setLog(c)} onDeleted={() => void refresh()} />
          ))}
        </div>
      )}

      {log ? <ChannelLog botId={botId} channel={log} onClose={() => setLog(null)} /> : null}
    </div>
  );
}

import { ChevronRight, Plus, Radio } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ui } from "../api";
import { btnClass } from "../Btn";
import { fail } from "../errors";
import { ErrorWell } from "../Field";
import type { Channel, ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { PageHead, widePage } from "../PageHead";
import { AdapterLogo } from "./AdapterLogo";
import { AdapterPicker } from "./AdapterPicker";
import { ChannelCard } from "./ChannelCard";
import { ChannelForm } from "./ChannelForm";
import { ChannelLog } from "./ChannelLog";
import { ChannelSetup } from "./ChannelSetup";

function Opening({ onBack }: { onBack: () => void }) {
  return (
    <div className={widePage}>
      <PageHead title="Channels" onBack={onBack} />
      <p className="text-ink-2 text-[13px]">Opening…</p>
    </div>
  );
}

// /bots/:id/channels[/new[/:adapter]|/:channelID[/setup]]: the list, and the
// pages it opens. Browser back works because each page is a route.
export function BotChannels({ botId, sub }: { botId: string; sub: string[] }) {
  const navigate = useNavigate();
  const [adapters, setAdapters] = useState<ChannelAdapter[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [log, setLog] = useState<Channel | null>(null);
  const [err, setErr] = useState("");

  const back = () => navigate(`/bots/${botId}/channels`);
  const newPath = `/bots/${botId}/channels/new`;

  async function refresh() {
    try {
      const [a, c] = await Promise.all([ui.listChannelAdapters({}), ui.listBotChannels({ botId })]);
      setAdapters(a.adapters);
      setChannels(c.channels);
      setErr("");
    } catch (e) {
      setErr(fail(e));
    } finally {
      setLoaded(true);
    }
  }

  useEffect(() => {
    void refresh();
    const t = setInterval(() => {
      void refresh();
    }, 4000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [botId]);

  const segs = sub ?? [];
  const counts = channels.reduce<Record<string, number>>((m, c) => ({ ...m, [c.adapter]: (m[c.adapter] ?? 0) + 1 }), {});

  // /bots/:id/channels/new
  if (segs[0] === "new" && segs.length === 1) {
    return <AdapterPicker botId={botId} adapters={adapters} counts={counts} loaded={loaded} onBack={back} />;
  }

  // /bots/:id/channels/new/:adapter
  if (segs[0] === "new" && segs.length >= 2) {
    const adapter = adapters.find((a) => a.slug === segs[1]);
    if (!adapter) return <Opening onBack={back} />;
    return <ChannelForm botId={botId} adapter={adapter} onBack={back} />;
  }

  // /bots/:id/channels/:id/setup
  if (segs.length === 2 && segs[1] === "setup") {
    const channel = channels.find((c) => c.id === segs[0]);
    const adapter = channel && adapters.find((a) => a.slug === channel.adapter);
    if (!channel || !adapter) return <Opening onBack={back} />;
    return <ChannelSetup botId={botId} channel={channel} adapter={adapter} onBack={back} onChanged={() => void refresh()} />;
  }

  // /bots/:id/channels/:id
  if (segs.length === 1 && segs[0] !== "new") {
    const channel = channels.find((c) => c.id === segs[0]);
    const adapter = channel && adapters.find((a) => a.slug === channel.adapter);
    if (!channel || !adapter) return <Opening onBack={back} />;
    return <ChannelForm botId={botId} adapter={adapter} channel={channel} onBack={back} />;
  }

  // /bots/:id/channels
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

        <button type="button" className={btnClass("primary")} onClick={() => navigate(newPath)}>
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
                  onClick={() => navigate(`${newPath}/${a.slug}`)}
                >
                  <AdapterLogo adapter={a} size={36} />
                  <span className="min-w-0 flex-1 truncate text-[14px] font-medium text-ink">{a.name}</span>
                  <ChevronRight size={15} className="shrink-0 text-ink-3 transition-transform duration-[160ms] ease-quiet group-hover:translate-x-0.5" />
                </button>
              ))}
            </div>
          ) : null}
          <button type="button" className="mt-5 text-[12.5px] font-medium text-cobalt hover:underline" onClick={() => navigate(newPath)}>
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

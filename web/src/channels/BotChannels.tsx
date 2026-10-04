import { Plus, Radio } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ui } from "../api";
import { btnClass } from "../Btn";
import { fail } from "../errors";
import { ErrorWell } from "../Field";
import type { Channel, ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { PageHead } from "../PageHead";
import { AdapterPicker } from "./AdapterPicker";
import { ChannelCard } from "./ChannelCard";
import { ChannelForm } from "./ChannelForm";
import { ChannelLog } from "./ChannelLog";
import { ChannelSetup } from "./ChannelSetup";

function Opening({ onBack }: { onBack: () => void }) {
  return (
    <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
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

  // /bots/:id/channels/new
  if (segs[0] === "new" && segs.length === 1) {
    return <AdapterPicker botId={botId} adapters={adapters} loaded={loaded} onBack={back} />;
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
    <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
      {/* Header */}
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1.5">
          <div className="flex items-center gap-2.5">
            <h1 className="text-title text-ink">Channels</h1>
            <span className="rounded-full bg-well px-2 py-0.5 text-[11px] font-semibold text-ink-3">{channels.length}</span>
          </div>
          <p className="max-w-3xl text-[13px] leading-relaxed text-ink-2">
            Ways to talk to this Bot. Connect Telegram, Slack, or other platforms to talk with this Bot from your favorite chat apps.
          </p>
        </div>

        <button type="button" className={btnClass("primary")} onClick={() => navigate(newPath)}>
          <Plus size={15} />
          <span>Add channel</span>
        </button>
      </div>

      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}

      {channels.length === 0 ? (
        <div className="rounded-card border border-dashed border-line bg-surface p-12 text-center">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-well text-ink-3">
            <Radio size={28} />
          </div>
          <h3 className="text-base font-semibold text-ink">No channels connected yet</h3>
          <p className="mx-auto mt-1 max-w-md text-[13px] text-ink-2">
            Connect external chat adapters to talk with this Bot directly from mobile or desktop messengers.
          </p>
          <div className="mt-6">
            <button type="button" className={btnClass("primary")} onClick={() => navigate(newPath)}>
              <Plus size={15} />
              Add channel
            </button>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 lg:gap-5">
          {channels.map((c) => (
            <ChannelCard key={c.id} botId={botId} c={c} a={adapters.find((x) => x.slug === c.adapter)} onLog={() => setLog(c)} onDeleted={() => void refresh()} />
          ))}
        </div>
      )}

      {log ? <ChannelLog botId={botId} channel={log} onClose={() => setLog(null)} /> : null}
    </div>
  );
}

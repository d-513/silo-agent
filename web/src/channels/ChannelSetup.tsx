import { CircleAlert, CircleCheck } from "lucide-react";
import { useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { ErrorWell, inputClass } from "../Field";
import type { Channel, ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { PageHead } from "../PageHead";
import { channelLook } from "../statusLook";
import { AdapterLogo } from "./AdapterLogo";

export function ChannelSetup({
  botId,
  channel,
  adapter,
  onBack,
  onChanged,
}: {
  botId: string;
  channel: Channel;
  adapter: ChannelAdapter;
  onBack: () => void;
  onChanged: () => void;
}) {
  const [state, setState] = useState(channel.state);
  const [target, setTarget] = useState({ id: channel.externalId, title: channel.targetTitle });
  const [manual, setManual] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function run(key: string) {
    setBusy(true);
    setErr("");
    try {
      const res = await ui.channelAction({ botId, id: channel.id, action: key, payload: {} });
      setState(res.state);
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  async function choose(value: string, label: string) {
    setBusy(true);
    setErr("");
    try {
      const res = await ui.channelAction({ botId, id: channel.id, action: "set_target", payload: { value, label } });
      setTarget({ id: value, title: label });
      setState(res.state);
      onChanged();
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
      <PageHead
        mark={<AdapterLogo adapter={adapter} size={40} />}
        title={channel.name}
        subtitle={`${adapter.name} Setup`}
        onBack={onBack}
      />

      <div className="max-w-2xl rounded-card shadow-card bg-surface p-6 space-y-5">
        {/* Status line */}
        <div className="flex items-center gap-2 rounded-sm bg-well px-3.5 py-2.5 text-[12px] font-medium text-ink-3">
          <Lamp status={channelLook(channel.status).lamp} />
          <span className="text-ink">{channelLook(channel.status).word}</span>
          {channel.statusDetail && <span className="text-ink-3">· {channel.statusDetail}</span>}
        </div>

        {adapter.requiresTarget ? (
          <div className="rounded-card bg-well p-4">
            <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-ink-3">Target Chat</div>
            <p className="text-[14px]">
              {target.id ? (
                <span className="flex items-center gap-1.5 text-ink font-medium">
                  <CircleCheck size={16} className="text-emerald shrink-0" />
                  Bound to <span className="font-semibold">{target.title || target.id}</span>
                </span>
              ) : (
                <span className="flex items-center gap-1.5 text-vermilion">
                  <CircleAlert size={16} className="shrink-0" />
                  No chat picked yet — this channel is currently inactive.
                </span>
              )}
            </p>
          </div>
        ) : null}

        {adapter.actions?.length ? (
          <div className="space-y-2">
            <div className="text-[11px] font-semibold uppercase tracking-wider text-ink-3">Actions</div>
            <div className="flex flex-wrap items-center gap-2.5">
              {adapter.actions.map((a) => (
                <Btn key={a.key} kind="secondary" type="button" disabled={busy} onClick={() => void run(a.key)}>
                  {a.label}
                </Btn>
              ))}
            </div>
          </div>
        ) : null}

        {adapter.requiresTarget ? (
          <div className="space-y-2 pt-1 border-t border-line/80">
            <div className="text-[11px] font-semibold uppercase tracking-wider text-ink-3">Set chat directly</div>
            <div className="flex items-center gap-2">
              <input
                className={`${inputClass} min-w-0 flex-1`}
                placeholder="@username, t.me/link, or chat id"
                value={manual}
                onChange={(e) => setManual(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && manual.trim()) void choose(manual.trim(), manual.trim());
                }}
              />
              <Btn kind="secondary" type="button" disabled={busy || !manual.trim()} onClick={() => void choose(manual.trim(), manual.trim())}>
                Set Target
              </Btn>
            </div>
          </div>
        ) : null}

        {state?.message ? (
          <div className="rounded-sm bg-well p-3 text-[13px] text-ink-3">
            {state.message}
          </div>
        ) : null}

        {state?.kind === "select" && state.options.length > 0 ? (
          <div className="space-y-2 pt-2 border-t border-line/80">
            <div className="text-[11px] font-semibold uppercase tracking-wider text-ink-3">Available chats</div>
            <div className="grid max-h-[320px] gap-2 overflow-auto">
              {state.options.map((o) => {
                const isSelected = o.value === target.id;
                return (
                  <button
                    key={o.value}
                    type="button"
                    disabled={busy}
                    className={`flex items-center justify-between rounded-sm border p-3 text-left text-[13px] transition-[background-color,color,box-shadow] ${ isSelected ? "border-cobalt bg-cobalt-pale text-ink font-medium"
                        : "border-line bg-surface hover:border-cobalt"
                    }`}
                    onClick={() => void choose(o.value, o.label)}
                  >
                    <span className="truncate">{o.label}</span>
                    {isSelected ? <CircleCheck size={16} className="shrink-0 text-cobalt" /> : null}
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}

        {state?.kind === "qr" && state.qr ? (
          <div className="flex flex-col items-center gap-3 pt-3 border-t border-line/80">
            <img src={state.qr} alt="Scan QR" className="w-52 rounded-card shadow-card bg-surface p-2" />
            <span className="text-xs text-ink-3">Scan with Telegram or your camera app</span>
          </div>
        ) : null}

        {state?.kind === "error" ? <ErrorWell>{state.message}</ErrorWell> : null}

        {err && <ErrorWell>{err}</ErrorWell>}
      </div>
    </div>
  );
}

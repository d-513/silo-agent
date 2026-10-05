import { CircleAlert, CircleCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { ErrorWell, inputClass, Panel } from "../Field";
import type { Channel, ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { widePage } from "../PageHead";
import { channelLook } from "../statusLook";
import { AdapterHead } from "./AdapterHead";
import { GuideCard } from "./GuideCard";

const caps = "text-label-caps uppercase text-ink-3";

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

  // A QR login rotates its code by itself, so the page follows the server's
  // state while a login is in progress (and for the step that ends it). Any
  // other state is left alone: the chat picker's list is the user's to keep.
  useEffect(() => {
    const pairing = (kind?: string) => kind === "qr" || kind === "auth";
    setState((cur) => (pairing(channel.state?.kind) || pairing(cur?.kind) ? channel.state : cur));
  }, [channel.state]);
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

  const look = channelLook(channel.status);
  const buttons = adapter.actions?.length ? (
    <div className="flex flex-wrap items-center gap-2.5">
      {adapter.actions.map((a) => (
        <Btn key={a.key} kind="secondary" type="button" disabled={busy} title={a.description || undefined} onClick={() => void run(a.key)}>
          {a.label}
        </Btn>
      ))}
    </div>
  ) : null;
  const message =
    state?.message && state.kind !== "qr" && state.kind !== "error" ? <div className="rounded-sm bg-well p-3 text-[13px] text-ink-2">{state.message}</div> : null;

  return (
    <div className={widePage}>
      <AdapterHead adapter={adapter} title={channel.name} subtitle={`${adapter.name} setup`} onBack={onBack} />

      <div className={`grid items-start gap-6 ${adapter.guide ? "lg:grid-cols-[minmax(0,1fr)_360px]" : "max-w-3xl"}`}>
        {adapter.guide ? (
          <div className="min-w-0 lg:order-2">
            <GuideCard guide={adapter.guide} title={`${adapter.name} setup guide`} />
          </div>
        ) : null}

        <div className="min-w-0 space-y-4 lg:order-1">
          <div className="flex items-center gap-2 rounded-card bg-surface px-4 py-3 text-[13px] shadow-card">
            <Lamp status={look.lamp} />
            <span className={`font-medium ${look.tone === "text-ink-3" ? "text-ink" : look.tone}`}>{look.word}</span>
            {channel.statusDetail ? <span className="min-w-0 truncate text-ink-3">· {channel.statusDetail}</span> : null}
          </div>

          {/* A login in progress comes first: nothing else works until it is scanned. */}
          {state?.kind === "qr" && state.qr ? (
            <Panel title="Link your device" note="Scan this code in the app. It refreshes by itself until you do.">
              <div className="flex flex-col items-center gap-3 rounded-card bg-well p-5">
                <img src={state.qr} alt="Login QR code" className="w-56 rounded-card bg-surface p-2 shadow-card" />
                <span className="max-w-sm text-center text-[13px] text-ink-2">{state.message}</span>
              </div>
            </Panel>
          ) : null}

          {adapter.requiresTarget ? (
            <Panel title="Target chat" note="The one conversation this channel reads and writes. The Bot ignores every other chat.">
              <div className="space-y-5">
                <div className="rounded-sm bg-well px-3.5 py-3 text-[14px]">
                  {target.id ? (
                    <span className="flex items-center gap-1.5 font-medium text-ink">
                      <CircleCheck size={16} className="shrink-0 text-emerald" />
                      <span>
                        Bound to <span className="font-semibold">{target.title || target.id}</span>
                      </span>
                    </span>
                  ) : (
                    <span className="flex items-center gap-1.5 text-vermilion">
                      <CircleAlert size={16} className="shrink-0" />
                      No chat picked yet. This channel is inactive until you pick one.
                    </span>
                  )}
                </div>

                {buttons ? (
                  <div className="space-y-2">
                    <div className={caps}>Actions</div>
                    {buttons}
                  </div>
                ) : null}
                {message}

                {state?.kind === "select" && state.options.length > 0 ? (
                  <div className="space-y-2">
                    <div className={caps}>Available chats</div>
                    <div className="grid max-h-[320px] gap-2 overflow-auto sm:grid-cols-2">
                      {state.options.map((o) => {
                        const on = o.value === target.id;
                        return (
                          <button
                            key={o.value}
                            type="button"
                            disabled={busy}
                            aria-pressed={on}
                            className={`flex items-center justify-between gap-2 rounded-control p-3 text-left text-[13px] transition-[background-color,box-shadow] duration-[160ms] ease-quiet disabled:cursor-not-allowed disabled:opacity-60 ${
                              on ? "bg-cobalt-pale font-medium text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "bg-surface shadow-card hover:shadow-float"
                            }`}
                            onClick={() => void choose(o.value, o.label)}
                          >
                            <span className="truncate">{o.label}</span>
                            {on ? <CircleCheck size={16} className="shrink-0 text-cobalt" /> : null}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                ) : null}

                <div className="space-y-2 border-t border-line pt-5">
                  <div className={caps}>Set chat directly</div>
                  <div className="flex items-center gap-2">
                    <input
                      className={`${inputClass} min-w-0 flex-1`}
                      placeholder="Chat id, @username, link, or phone number"
                      aria-label="Chat id, @username, link, or phone number"
                      value={manual}
                      onChange={(e) => setManual(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && manual.trim()) void choose(manual.trim(), manual.trim());
                      }}
                    />
                    <Btn kind="secondary" type="button" disabled={busy || !manual.trim()} onClick={() => void choose(manual.trim(), manual.trim())}>
                      Set target
                    </Btn>
                  </div>
                </div>
              </div>
            </Panel>
          ) : buttons || message ? (
            <Panel title="Actions">
              <div className="space-y-5">
                {buttons}
                {message}
              </div>
            </Panel>
          ) : null}

          {state?.kind === "error" ? <ErrorWell>{state.message}</ErrorWell> : null}
          {err ? <ErrorWell>{err}</ErrorWell> : null}
        </div>
      </div>
    </div>
  );
}

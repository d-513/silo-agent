import { ArrowLeft, Broadcast, CheckCircle, ClockCounterClockwise, GearSix, PencilSimple, Plus, Trash, WarningCircle } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import Markdown from "react-markdown";
import { useNavigate } from "react-router-dom";
import remarkGfm from "remark-gfm";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Switch } from "./Switch";
import { Thread, type Ev } from "./Thread";
import type { Channel, ChannelAdapter, ChannelField, Chat } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function AdapterLogo({ adapter, size = 16 }: { adapter?: ChannelAdapter; size?: number }) {
  if (adapter?.logo) {
    return <img src={adapter.logo} alt="" width={size} height={size} className="shrink-0 rounded-[6px]" />;
  }
  return <Broadcast size={size} className="shrink-0 text-bindery" />;
}

function statusDot(status: string) {
  if (status === "connected") return "bg-pine";
  if (status === "starting") return "bg-pine lamp-working";
  if (status === "error") return "bg-carmine";
  return "bg-thread";
}

function Opening({ onBack }: { onBack: () => void }) {
  return (
    <div className="silo-page">
      <div className="mb-4 flex items-center gap-2">
        <button className="text-stone hover:text-iron" onClick={onBack} title="Back">
          <ArrowLeft size={16} />
        </button>
        <h2 className="text-[22px] font-medium tracking-tight">Channels</h2>
      </div>
      <p className="text-stone">Opening…</p>
    </div>
  );
}

function PageHead({ logo, title, subtitle, onBack }: { logo?: ChannelAdapter; title: string; subtitle?: string; onBack: () => void }) {
  return (
    <div className="mb-5 flex items-center gap-2">
      <button className="text-stone hover:text-iron" onClick={onBack} title="Back">
        <ArrowLeft size={16} />
      </button>
      <AdapterLogo adapter={logo} size={28} />
      <div className="min-w-0">
        <h2 className="truncate text-[22px] font-medium tracking-tight">{title}</h2>
        {subtitle ? <div className="text-[12px] text-stone">{subtitle}</div> : null}
      </div>
    </div>
  );
}

function ToggleRow({
  label,
  hint,
  on,
  onChange,
}: {
  label: string;
  hint: string;
  on: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-[10px] border border-thread bg-folio px-3 py-2.5">
      <div className="min-w-0">
        <div className="text-[14px]">{label}</div>
        <div className="text-[12px] text-stone">{hint}</div>
      </div>
      <Switch on={on} onChange={onChange} />
    </div>
  );
}

function FieldInput({
  field,
  value,
  setValue,
  isSet,
}: {
  field: ChannelField;
  value: string;
  setValue: (v: string) => void;
  isSet?: boolean;
}) {
  const common = "h-9 w-full rounded border border-thread bg-folio px-3 outline-none focus:border-bindery";
  if (field.type === "toggle") {
    return (
      <div>
        <ToggleRow label={field.label} hint={field.description || "On or off."} on={value === "true"} onChange={(v) => setValue(v ? "true" : "false")} />
      </div>
    );
  }
  return (
    <div>
      <label className="mb-1 block text-[12px] font-medium text-stone">
        {field.label}
        {field.required ? " *" : ""}
      </label>
      {field.type === "select" ? (
        <select className={common} value={value} onChange={(e) => setValue(e.target.value)}>
          <option value="">—</option>
          {field.options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      ) : field.type === "textarea" ? (
        <textarea
          className="min-h-[72px] w-full rounded border border-thread bg-folio px-3 py-2 outline-none focus:border-bindery"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      ) : (
        <input
          className={common}
          type={field.secret ? "password" : field.type === "number" ? "number" : "text"}
          value={value}
          placeholder={field.secret && isSet ? "•••• set — type to replace" : ""}
          onChange={(e) => setValue(e.target.value)}
        />
      )}
      {field.description ? <p className="mt-1 text-[12px] text-stone">{field.description}</p> : null}
    </div>
  );
}

function ChannelForm({
  botId,
  adapter,
  channel,
  onBack,
}: {
  botId: string;
  adapter: ChannelAdapter;
  channel?: Channel;
  onBack: () => void;
}) {
  const [name, setName] = useState(channel?.name ?? "");
  const [enabled, setEnabled] = useState(channel?.enabled ?? true);
  const [inbound, setInbound] = useState(channel?.inbound ?? true);
  const [prompt, setPrompt] = useState(channel?.prompt ?? "");
  const [config, setConfig] = useState<Record<string, string>>(() => ({ ...(channel?.config ?? {}) }));
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const setCfg = (k: string, v: string) => setConfig((c) => ({ ...c, [k]: v }));

  async function save() {
    setBusy(true);
    setErr("");
    try {
      const cleanSecrets: Record<string, string> = {};
      for (const [k, v] of Object.entries(secrets)) if (v.trim()) cleanSecrets[k] = v;
      if (channel) {
        await ui.updateChannel({
          botId,
          id: channel.id,
          name,
          enabled,
          inbound,
          prompt,
          config,
          secrets: cleanSecrets,
          externalId: channel.externalId,
          targetTitle: channel.targetTitle,
        });
      } else {
        await ui.createChannel({ botId, adapter: adapter.slug, name, enabled, inbound, prompt, config, secrets: cleanSecrets });
      }
      onBack();
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="silo-page pb-12">
      <PageHead
        logo={adapter}
        title={channel ? channel.name : adapter.name}
        subtitle={channel ? adapter.name : `Add ${adapter.name}`}
        onBack={onBack}
      />
      {adapter.guide ? (
        <details open className="mb-5 max-w-[560px] rounded-[10px] border border-thread bg-cloth/30">
          <summary className="cursor-pointer select-none px-3 py-2 text-[12px] font-medium tracking-wide text-stone">
            Setup instructions
          </summary>
          <div className="silo-md border-t border-thread-2/50 px-3 py-3 text-[13px] leading-relaxed">
            <Markdown remarkPlugins={[remarkGfm]}>{adapter.guide}</Markdown>
          </div>
        </details>
      ) : null}
      <div className="grid max-w-[560px] gap-4">
        <div>
          <label className="mb-1 block text-[12px] font-medium text-stone">Name</label>
          <input
            className="h-9 w-full rounded border border-thread bg-folio px-3 outline-none focus:border-bindery"
            value={name}
            placeholder={adapter.name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        {adapter.fields.map((f) => (
          <FieldInput
            key={f.key}
            field={f}
            value={f.secret ? secrets[f.key] ?? "" : config[f.key] ?? ""}
            setValue={(v) => (f.secret ? setSecrets((s) => ({ ...s, [f.key]: v })) : setCfg(f.key, v))}
            isSet={channel?.secretsSet?.includes(f.key)}
          />
        ))}
        <ToggleRow label="Enabled" hint="Connect this channel when saved." on={enabled} onChange={setEnabled} />
        <ToggleRow
          label="Deliver messages to the Bot"
          hint="Off makes it send-only, used by tools."
          on={inbound}
          onChange={setInbound}
        />
        <div>
          <label className="mb-1 block text-[12px] font-medium text-stone">Prompt</label>
          <p className="mb-1 text-[12px] text-stone">
            Extra instructions for runs on this channel, e.g. “This is WhatsApp; keep replies short.”
          </p>
          <textarea
            className="h-[140px] w-full resize-y rounded border border-thread bg-folio px-3 py-2 font-mono text-[13px] leading-5 outline-none focus:border-bindery"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
        </div>

        {err ? <p className="text-carmine">{err}</p> : null}
        <div className="flex items-center gap-2">
          <Btn kind="primary" type="button" disabled={busy || (!channel && !name.trim())} onClick={() => void save()}>
            {busy ? "Saving…" : channel ? "Save" : "Add channel"}
          </Btn>
          <Btn kind="secondary" type="button" onClick={onBack}>
            Cancel
          </Btn>
        </div>
      </div>
    </div>
  );
}

function ChannelLog({ botId, channel, onClose }: { botId: string; channel: Channel; onClose: () => void }) {
  const [chats, setChats] = useState<Chat[]>([]);
  const [chatId, setChatId] = useState("");
  const [events, setEvents] = useState<Ev[]>([]);

  useEffect(() => {
    let dead = false;
    ui.listChats({ botId, channelId: channel.id })
      .then((r) => {
        if (dead) return;
        setChats(r.chats);
        setChatId(r.chats[0]?.id ?? "");
      })
      .catch(() => {});
    return () => {
      dead = true;
    };
  }, [botId, channel.id]);

  useEffect(() => {
    if (!chatId) {
      setEvents([]);
      return;
    }
    let dead = false;
    const ac = new AbortController();
    setEvents([]);
    (async () => {
      try {
        for await (const ev of ui.streamRun({ botId, chatId, afterEventId: "" }, { signal: ac.signal })) {
          if (dead) return;
          setEvents((xs) => [
            ...xs,
            {
              id: ev.id,
              kind: ev.kind,
              body: ev.body,
              tool: ev.tool,
              runId: ev.runId,
              attachments: ev.attachments.map((a) => ({ name: a.name, path: a.path, size: Number(a.size) })),
            },
          ]);
        }
      } catch {
        /* closed */
      }
    })();
    return () => {
      dead = true;
      ac.abort();
    };
  }, [botId, chatId]);

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-iron/30 p-4" onClick={onClose}>
      <div
        className="flex h-full max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-[10px] border border-thread bg-plaster"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center gap-2 border-b border-thread-2 px-4 py-3">
          <Broadcast size={16} className="text-bindery" />
          <span className="font-medium">{channel.name} log</span>
          <span className="text-[12px] text-stone">{chats.length} conversations</span>
          <button className="ml-auto text-stone hover:text-iron" onClick={onClose}>
            Close
          </button>
        </div>
        {chats.length > 1 ? (
          <div className="silo-scroll-x flex shrink-0 gap-1 border-b border-thread-2 px-3 py-2">
            {chats.map((c) => (
              <button
                key={c.id}
                className={`shrink-0 rounded px-2 py-1 text-[12px] ${
                  c.id === chatId ? "bg-bindery-pale text-iron" : "text-stone hover:bg-linen"
                }`}
                onClick={() => setChatId(c.id)}
              >
                {c.title || c.id}
              </button>
            ))}
          </div>
        ) : null}
        <div className="flex min-h-0 flex-1 flex-col">
          {chatId ? (
            <Thread botId={botId} events={events} sending={false} />
          ) : (
            <div className="flex h-full items-center justify-center text-stone">No conversations yet.</div>
          )}
        </div>
      </div>
    </div>
  );
}

function ChannelSetup({
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
    <div className="silo-page pb-12">
      <PageHead
        logo={adapter}
        title={channel.name}
        subtitle={adapter.name}
        onBack={onBack}
      />
      <div className="max-w-[560px] space-y-4">
        <div className="flex items-center gap-2 text-[12px] text-stone">
          <span className={`inline-block h-2 w-2 rounded-full ${statusDot(channel.status)}`} />
          <span>{channel.statusDetail || channel.status}</span>
        </div>

        {adapter.requiresTarget ? (
          <div className="rounded-[10px] border border-thread bg-folio p-3">
            <div className="mb-1 text-[12px] font-medium tracking-wide text-stone">Chat</div>
            <p className="text-[14px]">
              {target.id ? (
                <>
                  Bound to <span className="font-medium">{target.title || target.id}</span>
                </>
              ) : (
                <span className="text-carmine">No chat picked yet — this channel is inactive.</span>
              )}
            </p>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center gap-3">
          {adapter.actions.map((a) => (
            <Btn key={a.key} kind="secondary" type="button" disabled={busy} onClick={() => void run(a.key)}>
              {a.label}
            </Btn>
          ))}
        </div>

        {adapter.requiresTarget ? (
          <div>
            <div className="mb-2 text-[12px] font-medium tracking-wide text-stone">Set chat directly</div>
            <div className="flex items-center gap-2">
              <input
                className="h-9 min-w-0 flex-1 rounded border border-thread bg-folio px-3 text-[13px] outline-none focus:border-bindery"
                placeholder="@username, t.me/link, or chat id"
                value={manual}
                onChange={(e) => setManual(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && manual.trim()) void choose(manual.trim(), manual.trim());
                }}
              />
              <Btn kind="secondary" type="button" disabled={busy || !manual.trim()} onClick={() => void choose(manual.trim(), manual.trim())}>
                Use
              </Btn>
            </div>
          </div>
        ) : null}

        {state.message ? <p className="text-[12px] text-stone">{state.message}</p> : null}

        {state.kind === "select" && state.options.length > 0 ? (
          <div>
            <div className="mb-2 text-[12px] font-medium tracking-wide text-stone">Pick a chat</div>
            <div className="grid max-h-[320px] gap-1.5 overflow-auto">
              {state.options.map((o) => (
                <button
                  key={o.value}
                  type="button"
                  disabled={busy}
                  className={`flex items-center justify-between rounded-lg border px-3 py-2 text-left text-[13px] ${
                    o.value === target.id ? "border-bindery bg-bindery-pale text-iron" : "border-thread-2 bg-folio hover:border-bindery"
                  }`}
                  onClick={() => void choose(o.value, o.label)}
                >
                  <span className="truncate">{o.label}</span>
                  {o.value === target.id ? <CheckCircle size={14} className="shrink-0 text-bindery" /> : null}
                </button>
              ))}
            </div>
          </div>
        ) : null}

        {state.kind === "qr" && state.qr ? <img src={state.qr} alt="QR" className="w-48 rounded border border-thread bg-folio" /> : null}
        {state.kind === "error" ? <p className="text-carmine">{state.message}</p> : null}
        {err ? <p className="text-carmine">{err}</p> : null}
      </div>
    </div>
  );
}

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
    return (
      <div className="silo-page pb-12">
        <PageHead title="Add a channel" subtitle="Built-in adapters; one Bot can have several." onBack={back} />
        {!loaded ? (
          <p className="text-stone">Loading…</p>
        ) : adapters.length === 0 ? (
          <p className="text-stone">No adapters available.</p>
        ) : (
          <div className="grid max-w-[640px] gap-3">
            {adapters.map((a) => (
              <button
                key={a.slug}
                type="button"
                className="flex items-start gap-3 rounded-[10px] border border-thread bg-folio p-4 text-left hover:border-bindery"
                onClick={() => navigate(`/bots/${botId}/channels/new/${a.slug}`)}
              >
                <AdapterLogo adapter={a} size={36} />
                <div className="min-w-0">
                  <div className="font-medium">{a.name}</div>
                  {a.description ? <div className="text-[12px] text-stone">{a.description}</div> : null}
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    );
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
    <div className="silo-page pb-12">
      <div className="mb-1 flex items-center justify-between gap-3">
        <h2 className="text-[22px] font-medium tracking-tight">Channels</h2>
        <Btn kind="primary" type="button" icon={<Plus size={12} />} onClick={() => navigate(newPath)}>
          Add channel
        </Btn>
      </div>
      <p className="mb-6 text-stone">Ways to talk to this Bot. Built-in adapters; one Bot can have several.</p>
      {err ? <p className="mb-4 text-carmine">{err}</p> : null}

      {channels.length === 0 ? (
        <div className="rounded-[10px] border border-thread bg-folio p-6 text-center">
          <Broadcast size={24} className="mx-auto mb-2 text-bindery" />
          <p className="text-stone">No channels yet.</p>
        </div>
      ) : (
        <div className="grid gap-3">
          {channels.map((c) => {
            const a = adapters.find((x) => x.slug === c.adapter);
            const needsSetup = !!a?.requiresTarget && !c.externalId;
            return (
              <div key={c.id} className="flex items-center gap-3 rounded-[10px] border border-thread bg-folio p-4">
                <AdapterLogo adapter={a} size={28} />
                <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${statusDot(c.status)}`} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{c.name}</span>
                    <span className="rounded bg-cloth px-1.5 py-0.5 text-[11px] text-stone">{c.adapterName || c.adapter}</span>
                    {!c.enabled ? <span className="text-[12px] text-stone">disabled</span> : null}
                    {!c.inbound ? <span className="text-[12px] text-stone">send-only</span> : null}
                    {a?.requiresTarget ? (
                      c.externalId ? (
                        <span className="truncate text-[12px] text-stone">{c.targetTitle || c.externalId}</span>
                      ) : (
                        <span className="text-[12px] text-carmine">needs setup</span>
                      )
                    ) : null}
                  </div>
                  <div className="flex items-center gap-1.5 text-[12px] text-stone">
                    {c.status === "error" ? <WarningCircle size={12} className="text-carmine" /> : <CheckCircle size={12} />}
                    <span className="truncate">{c.statusDetail || c.status}</span>
                  </div>
                </div>
                {a?.actions?.length ? (
                  needsSetup ? (
                    <Btn kind="primary" type="button" onClick={() => navigate(`/bots/${botId}/channels/${c.id}/setup`)}>
                      <GearSix size={14} className="silo-blink" />
                      Set up
                    </Btn>
                  ) : (
                    <button
                      title="Set up"
                      className="rounded p-2 text-stone hover:bg-linen hover:text-iron"
                      onClick={() => navigate(`/bots/${botId}/channels/${c.id}/setup`)}
                    >
                      <GearSix size={16} />
                    </button>
                  )
                ) : null}
                <button
                  title="View log"
                  className="rounded p-2 text-stone hover:bg-linen hover:text-iron"
                  onClick={() => setLog(c)}
                >
                  <ClockCounterClockwise size={16} />
                </button>
                <button
                  title="Configure"
                  className="rounded p-2 text-stone hover:bg-linen hover:text-iron"
                  onClick={() => navigate(`/bots/${botId}/channels/${c.id}`)}
                >
                  <PencilSimple size={16} />
                </button>
                <button
                  title="Delete channel"
                  className="rounded p-2 text-stone hover:bg-linen hover:text-carmine"
                  onClick={async () => {
                    await ui.deleteChannel({ botId, id: c.id });
                    void refresh();
                  }}
                >
                  <Trash size={16} />
                </button>
              </div>
            );
          })}
        </div>
      )}

      {log ? <ChannelLog botId={botId} channel={log} onClose={() => setLog(null)} /> : null}
    </div>
  );
}

import { ArrowLeft, Check, CircleAlert, CircleCheck, History, Pencil, Plus, Radio, Settings, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import Markdown from "react-markdown";
import { useNavigate } from "react-router-dom";
import remarkGfm from "remark-gfm";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { Field, inputClass, textareaClass } from "./Field";
import { Select } from "./Select";
import { ToggleRow } from "./Switch";
import { Thread, type Ev } from "./Thread";
import type { Channel, ChannelAdapter, ChannelField, Chat } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function AdapterLogo({ adapter, size = 44 }: { adapter?: ChannelAdapter; size?: number }) {
  if (adapter?.logo) {
    return (
      <div
        className="flex shrink-0 items-center justify-center overflow-hidden rounded-lg border border-thread bg-cloth shadow-2xs"
        style={{ width: size, height: size }}
      >
        <img src={adapter.logo} alt="" className="h-full w-full object-cover" />
      </div>
    );
  }
  return (
    <div
      className="flex shrink-0 items-center justify-center rounded-lg border border-thread bg-bindery-pale text-bindery shadow-2xs"
      style={{ width: size, height: size }}
    >
      <Radio size={Math.round(size * 0.5)} />
    </div>
  );
}

function statusDot(status: string) {
  if (status === "connected") return "bg-pine";
  if (status === "starting") return "bg-pine animate-pulse";
  if (status === "error") return "bg-carmine";
  return "bg-stone";
}

function statusLabel(status: string) {
  if (status === "connected") return "Connected";
  if (status === "starting") return "Starting…";
  if (status === "error") return "Error";
  if (status === "stopped") return "Stopped";
  return status.replace(/_/g, " ");
}

function Opening({ onBack }: { onBack: () => void }) {
  return (
    <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
      <div className="mb-4 flex items-center gap-3">
        <button
          type="button"
          className="flex h-8 w-8 items-center justify-center rounded-lg border border-thread bg-folio text-stone hover:border-bindery hover:text-iron shadow-2xs transition-colors"
          onClick={onBack}
          title="Back"
        >
          <ArrowLeft size={16} />
        </button>
        <h2 className="text-[22px] font-medium tracking-tight text-iron">Channels</h2>
      </div>
      <p className="text-stone text-[13px]">Opening…</p>
    </div>
  );
}

function PageHead({
  logo,
  title,
  subtitle,
  onBack,
}: {
  logo?: ChannelAdapter;
  title: string;
  subtitle?: string;
  onBack: () => void;
}) {
  return (
    <div className="mb-6 flex items-center gap-3.5">
      <button
        type="button"
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-thread bg-folio text-stone hover:border-bindery hover:text-iron shadow-2xs transition-colors"
        onClick={onBack}
        title="Back"
      >
        <ArrowLeft size={16} />
      </button>
      <AdapterLogo adapter={logo} size={40} />
      <div className="min-w-0">
        <h2 className="truncate text-[20px] font-semibold tracking-tight text-iron">{title}</h2>
        {subtitle ? <div className="text-[12px] text-stone mt-0.5">{subtitle}</div> : null}
      </div>
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
  if (field.type === "toggle") {
    return (
      <div>
        <ToggleRow
          className="rounded-xl border border-thread bg-folio px-3.5 py-3 shadow-2xs"
          label={field.label}
          hint={field.description || "On or off."}
          on={value === "true"}
          onChange={(v) => setValue(v ? "true" : "false")}
        />
      </div>
    );
  }
  return (
    <Field label={field.label} required={field.required} hint={field.description}>
      {field.type === "select" ? (
        <Select
          value={value}
          onChange={setValue}
          placeholder="—"
          options={field.options.map((o) => ({ value: o.value, label: o.label }))}
        />
      ) : field.type === "textarea" ? (
        <textarea className={`${textareaClass} min-h-[80px]`} value={value} onChange={(e) => setValue(e.target.value)} />
      ) : (
        <input
          className={inputClass}
          type={field.secret ? "password" : field.type === "number" ? "number" : "text"}
          value={value}
          placeholder={field.secret && isSet ? "•••• set — type to replace" : ""}
          onChange={(e) => setValue(e.target.value)}
        />
      )}
    </Field>
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
    <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
      <PageHead
        logo={adapter}
        title={channel ? channel.name : adapter.name}
        subtitle={channel ? adapter.name : `Configure ${adapter.name}`}
        onBack={onBack}
      />

      <div className="max-w-2xl space-y-6">
        {adapter.guide ? (
          <details open className="rounded-xl border border-thread bg-cloth/40 p-4 shadow-2xs">
            <summary className="cursor-pointer select-none font-medium text-xs text-stone tracking-wide uppercase">
              Setup Instructions & Guide
            </summary>
            <div className="silo-md border-t border-thread/80 mt-3 pt-3 text-[13px] leading-relaxed">
              <Markdown remarkPlugins={[remarkGfm]}>{adapter.guide}</Markdown>
            </div>
          </details>
        ) : null}

        <div className="rounded-xl border border-thread bg-folio p-6 shadow-2xs space-y-5">
          <Field label="Name" required>
            <input className={inputClass} value={name} placeholder={adapter.name} onChange={(e) => setName(e.target.value)} />
          </Field>

          {adapter.fields.map((f) => (
            <FieldInput
              key={f.key}
              field={f}
              value={f.secret ? secrets[f.key] ?? "" : config[f.key] ?? ""}
              setValue={(v) => (f.secret ? setSecrets((s) => ({ ...s, [f.key]: v })) : setCfg(f.key, v))}
              isSet={channel?.secretsSet?.includes(f.key)}
            />
          ))}

          <ToggleRow
            className="rounded-xl border border-thread bg-folio px-3.5 py-3 shadow-2xs"
            label="Enabled"
            hint="Connect this channel when saved."
            on={enabled}
            onChange={setEnabled}
          />

          <ToggleRow
            className="rounded-xl border border-thread bg-folio px-3.5 py-3 shadow-2xs"
            label="Deliver messages to the Bot"
            hint="Off makes it send-only, used by tools."
            on={inbound}
            onChange={setInbound}
          />

          <Field label="Prompt" hint="Extra instructions for runs on this channel, e.g. “This is WhatsApp; keep replies short.”">
            <textarea
              className={`${textareaClass} h-[130px] font-mono text-[13px] leading-5`}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="Keep answers concise..."
            />
          </Field>

          {err && (
            <div className="rounded-lg border border-carmine/20 bg-carmine/10 p-3 text-[13px] text-carmine">
              {err}
            </div>
          )}

          <div className="flex items-center gap-2.5 pt-2">
            <Btn kind="primary" type="button" disabled={busy || (!channel && !name.trim())} onClick={() => void save()}>
              {busy ? "Saving…" : channel ? "Save changes" : "Add channel"}
            </Btn>
            <Btn kind="ghost" type="button" onClick={onBack}>
              Cancel
            </Btn>
          </div>
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
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-iron/30 backdrop-blur-xs p-4" onClick={onClose}>
      <div
        className="flex h-full max-h-[85vh] w-full max-w-4xl flex-col overflow-hidden rounded-xl border border-thread bg-folio shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center justify-between border-b border-thread px-5 py-3.5">
          <div className="flex items-center gap-2.5">
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-bindery-pale text-bindery">
              <Radio size={16} />
            </div>
            <div>
              <span className="font-semibold text-sm text-iron">{channel.name} Log</span>
              <span className="ml-2 rounded-full bg-cloth px-2 py-0.5 text-[11px] font-medium text-stone">
                {chats.length} {chats.length === 1 ? "conversation" : "conversations"}
              </span>
            </div>
          </div>
          <button
            type="button"
            className="flex h-8 w-8 items-center justify-center rounded-lg text-stone hover:bg-cloth hover:text-iron transition-colors"
            onClick={onClose}
          >
            <X size={16} />
          </button>
        </div>

        {chats.length > 1 ? (
          <div className="silo-scroll-x flex shrink-0 gap-1.5 border-b border-thread bg-cloth/40 px-4 py-2">
            {chats.map((c) => (
              <button
                key={c.id}
                type="button"
                className={`shrink-0 rounded-lg px-3 py-1.5 text-xs transition-colors ${
                  c.id === chatId
                    ? "bg-folio font-semibold text-iron shadow-2xs border border-thread"
                    : "text-stone hover:text-iron hover:bg-folio/60"
                }`}
                onClick={() => setChatId(c.id)}
              >
                {c.title || c.id}
              </button>
            ))}
          </div>
        ) : null}

        <div className="flex min-h-0 flex-1 flex-col bg-plaster">
          {chatId ? (
            <Thread botId={botId} chatId={chatId} events={events} sending={false} />
          ) : (
            <div className="flex h-full items-center justify-center text-stone text-[13px]">
              No conversations recorded yet.
            </div>
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
    <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
      <PageHead
        logo={adapter}
        title={channel.name}
        subtitle={`${adapter.name} Setup`}
        onBack={onBack}
      />

      <div className="max-w-2xl rounded-xl border border-thread bg-folio p-6 shadow-2xs space-y-5">
        {/* Status line */}
        <div className="flex items-center gap-2 rounded-lg border border-thread bg-cloth/50 px-3.5 py-2.5 text-[12px] font-medium text-stone">
          <span className={`inline-block h-2.5 w-2.5 rounded-full ${statusDot(channel.status)}`} />
          <span className="text-iron">{statusLabel(channel.status)}</span>
          {channel.statusDetail && <span className="text-stone">· {channel.statusDetail}</span>}
        </div>

        {adapter.requiresTarget ? (
          <div className="rounded-xl border border-thread bg-cloth/30 p-4">
            <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-stone">Target Chat</div>
            <p className="text-[14px]">
              {target.id ? (
                <span className="flex items-center gap-1.5 text-iron font-medium">
                  <CircleCheck size={16} className="text-pine shrink-0" />
                  Bound to <span className="font-semibold">{target.title || target.id}</span>
                </span>
              ) : (
                <span className="flex items-center gap-1.5 text-carmine">
                  <CircleAlert size={16} className="shrink-0" />
                  No chat picked yet — this channel is currently inactive.
                </span>
              )}
            </p>
          </div>
        ) : null}

        {adapter.actions?.length ? (
          <div className="space-y-2">
            <div className="text-[11px] font-semibold uppercase tracking-wider text-stone">Actions</div>
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
          <div className="space-y-2 pt-1 border-t border-thread/80">
            <div className="text-[11px] font-semibold uppercase tracking-wider text-stone">Set chat directly</div>
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
          <div className="rounded-lg border border-thread bg-cloth/50 p-3 text-[13px] text-stone">
            {state.message}
          </div>
        ) : null}

        {state?.kind === "select" && state.options.length > 0 ? (
          <div className="space-y-2 pt-2 border-t border-thread/80">
            <div className="text-[11px] font-semibold uppercase tracking-wider text-stone">Available chats</div>
            <div className="grid max-h-[320px] gap-2 overflow-auto">
              {state.options.map((o) => {
                const isSelected = o.value === target.id;
                return (
                  <button
                    key={o.value}
                    type="button"
                    disabled={busy}
                    className={`flex items-center justify-between rounded-lg border p-3 text-left text-[13px] transition-all shadow-2xs ${
                      isSelected
                        ? "border-bindery bg-bindery-pale text-iron font-medium"
                        : "border-thread bg-folio hover:border-bindery"
                    }`}
                    onClick={() => void choose(o.value, o.label)}
                  >
                    <span className="truncate">{o.label}</span>
                    {isSelected ? <CircleCheck size={16} className="shrink-0 text-bindery" /> : null}
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}

        {state?.kind === "qr" && state.qr ? (
          <div className="flex flex-col items-center gap-3 pt-3 border-t border-thread/80">
            <img src={state.qr} alt="Scan QR" className="w-52 rounded-xl border border-thread bg-folio p-2 shadow-sm" />
            <span className="text-xs text-stone">Scan with Telegram or your camera app</span>
          </div>
        ) : null}

        {state?.kind === "error" ? (
          <div className="rounded-lg border border-carmine/20 bg-carmine/10 p-3 text-[13px] text-carmine">
            {state.message}
          </div>
        ) : null}

        {err && (
          <div className="rounded-lg border border-carmine/20 bg-carmine/10 p-3 text-[13px] text-carmine">
            {err}
          </div>
        )}
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
  const [armDelete, setArmDelete] = useState<string>("");
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
      <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
        <PageHead title="Add a channel" subtitle="Choose a built-in adapter to connect with this Bot." onBack={back} />
        {!loaded ? (
          <p className="text-stone text-[13px]">Loading adapters…</p>
        ) : adapters.length === 0 ? (
          <div className="rounded-xl border border-dashed border-thread bg-folio p-12 text-center text-stone">
            No channel adapters available on this system.
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-3xl">
            {adapters.map((a) => (
              <button
                key={a.slug}
                type="button"
                className="group flex items-start gap-4 rounded-xl border border-thread bg-folio p-5 text-left transition-all hover:border-bindery hover:shadow-xs"
                onClick={() => navigate(`/bots/${botId}/channels/new/${a.slug}`)}
              >
                <AdapterLogo adapter={a} size={44} />
                <div className="min-w-0 flex-1">
                  <div className="font-semibold text-[15px] text-iron group-hover:text-bindery transition-colors">
                    {a.name}
                  </div>
                  {a.description ? (
                    <div className="mt-1 text-xs text-stone leading-relaxed line-clamp-2">{a.description}</div>
                  ) : null}
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
    <div className="w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6 pb-20">
      {/* Header */}
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1.5">
          <div className="flex items-center gap-2.5">
            <h1 className="text-[22px] font-medium tracking-tight text-iron">Channels</h1>
            <span className="rounded-full bg-cloth px-2 py-0.5 text-[11px] font-semibold text-stone">
              {channels.length}
            </span>
          </div>
          <p className="max-w-3xl text-[13px] leading-relaxed text-stone">
            Ways to talk to this Bot. Connect Telegram, Slack, or other platforms to talk with this Bot from your favorite chat apps.
          </p>
        </div>

        <button
          type="button"
          className={btnClass("primary")}
          onClick={() => navigate(newPath)}
        >
          <Plus size={15} />
          <span>Add channel</span>
        </button>
      </div>

      {err ? (
        <div className="mb-4 rounded-lg border border-carmine/20 bg-carmine/10 p-3.5 text-[13px] text-carmine">
          {err}
        </div>
      ) : null}

      {channels.length === 0 ? (
        <div className="rounded-xl border border-dashed border-thread bg-folio p-12 text-center shadow-2xs">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-cloth text-stone">
            <Radio size={28} />
          </div>
          <h3 className="text-base font-semibold text-iron">No channels connected yet</h3>
          <p className="mx-auto mt-1 max-w-md text-[13px] text-stone">
            Connect external chat adapters to talk with this Bot directly from mobile or desktop messengers.
          </p>
          <div className="mt-6">
            <button
              type="button"
              className={btnClass("primary")}
              onClick={() => navigate(newPath)}
            >
              <Plus size={15} />
              Add channel
            </button>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 lg:gap-5">
          {channels.map((c) => {
            const a = adapters.find((x) => x.slug === c.adapter);
            const needsSetup = !!a?.requiresTarget && !c.externalId;
            return (
              <div
                key={c.id}
                className="flex flex-col justify-between rounded-xl border border-thread bg-folio p-5 shadow-2xs transition-all hover:border-hover hover:shadow-xs min-h-[140px]"
              >
                <div>
                  <div className="flex items-start gap-4">
                    <AdapterLogo adapter={a} size={48} />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold text-[15px] text-iron tracking-tight">{c.name}</span>
                        <span className="rounded-[6px] border border-thread bg-cloth px-2 py-0.5 text-[11px] font-medium text-stone">
                          {c.adapterName || c.adapter}
                        </span>
                        {!c.enabled && (
                          <span className="rounded-[6px] bg-cloth px-2 py-0.5 text-[11px] font-medium text-stone">
                            Disabled
                          </span>
                        )}
                        {!c.inbound && (
                          <span className="rounded-[6px] bg-cloth px-2 py-0.5 text-[11px] font-medium text-stone">
                            Send-only
                          </span>
                        )}
                      </div>

                      {/* Target Info */}
                      {a?.requiresTarget && (
                        <div className="mt-1.5 text-xs">
                          {c.externalId ? (
                            <span className="text-stone">
                              Chat: <span className="font-medium text-iron">{c.targetTitle || c.externalId}</span>
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 font-semibold text-carmine">
                              <CircleAlert size={13} /> Needs setup
                            </span>
                          )}
                        </div>
                      )}

                      {/* Status */}
                      <div className="mt-2.5 flex items-center gap-1.5 text-[11px] font-medium text-stone">
                        <span className={`inline-block h-2 w-2 rounded-full ${statusDot(c.status)}`} />
                        <span className={c.status === "error" ? "text-carmine" : "text-stone"}>
                          {statusLabel(c.status)}
                        </span>
                        {c.statusDetail && (
                          <span className="truncate text-stone">· {c.statusDetail}</span>
                        )}
                      </div>
                    </div>
                  </div>
                </div>

                {/* Bottom Actions Toolbar */}
                <div className="mt-4 flex items-center justify-between border-t border-thread/80 pt-3.5">
                  <div>
                    {a?.actions?.length ? (
                      needsSetup ? (
                        <Btn
                          kind="primary"
                          type="button"
                          onClick={() => navigate(`/bots/${botId}/channels/${c.id}/setup`)}
                        >
                          <Settings size={14} className="silo-blink" />
                          <span>Set up</span>
                        </Btn>
                      ) : (
                        <Btn
                          kind="ghost"
                          type="button"
                          onClick={() => navigate(`/bots/${botId}/channels/${c.id}/setup`)}
                        >
                          <Settings size={14} />
                          <span>Set up</span>
                        </Btn>
                      )
                    ) : null}
                  </div>

                  <div className="flex items-center gap-1.5">
                    <Btn
                      kind="ghost"
                      type="button"
                      onClick={() => setLog(c)}
                      title="View conversation log"
                    >
                      <History size={14} />
                      <span>Log</span>
                    </Btn>

                    <Btn
                      kind="ghost"
                      type="button"
                      onClick={() => navigate(`/bots/${botId}/channels/${c.id}`)}
                      title="Configure channel"
                    >
                      <Pencil size={14} />
                      <span>Configure</span>
                    </Btn>

                    <Btn
                      kind="ghost"
                      type="button"
                      className="text-carmine hover:text-carmine"
                      onClick={async () => {
                        if (armDelete !== c.id) {
                          setArmDelete(c.id);
                          return;
                        }
                        setArmDelete("");
                        await ui.deleteChannel({ botId, id: c.id });
                        void refresh();
                      }}
                      title="Delete channel"
                    >
                      <Trash2 size={14} />
                      <span>{armDelete === c.id ? "Confirm?" : "Delete"}</span>
                    </Btn>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {log ? <ChannelLog botId={botId} channel={log} onClose={() => setLog(null)} /> : null}
    </div>
  );
}

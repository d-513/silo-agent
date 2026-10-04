import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { SaveButton, useSave } from "../Feedback";
import { ErrorWell, Field, inputClass, textareaClass } from "../Field";
import { FieldInput } from "../FieldInput";
import type { Channel, ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { widePage } from "../PageHead";
import { ToggleRow } from "../Switch";
import { AdapterHead } from "./AdapterHead";

export function ChannelForm({
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
  const saver = useSave();
  const [err, setErr] = useState("");

  const setCfg = (k: string, v: string) => setConfig((c) => ({ ...c, [k]: v }));

  async function save() {
    setBusy(true);
    setErr("");
    try {
      await saver.run(async () => {
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
      });
      onBack();
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={widePage}>
      <AdapterHead
        adapter={adapter}
        title={channel ? channel.name : adapter.name}
        subtitle={channel ? adapter.name : `Configure ${adapter.name}`}
        onBack={onBack}
      />

      <div className="max-w-2xl space-y-6">
        {adapter.guide ? (
          <details open className="rounded-card bg-well p-4">
            <summary className="cursor-pointer select-none font-medium text-xs text-ink-3 tracking-wide uppercase">
              Setup Instructions & Guide
            </summary>
            <div className="silo-md border-t border-line/80 mt-3 pt-3 text-[13px] leading-relaxed">
              <Markdown remarkPlugins={[remarkGfm]}>{adapter.guide}</Markdown>
            </div>
          </details>
        ) : null}

        <div className="rounded-card shadow-card bg-surface p-6 space-y-5">
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
            className="rounded-card shadow-card bg-surface px-3.5 py-3"
            label="Enabled"
            hint="Connect this channel when saved."
            on={enabled}
            onChange={setEnabled}
          />

          <ToggleRow
            className="rounded-card shadow-card bg-surface px-3.5 py-3"
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

          {err && <ErrorWell>{err}</ErrorWell>}

          <div className="flex items-center gap-2.5 pt-2">
            <SaveButton type="button" state={saver.state} disabled={busy || (!channel && !name.trim())} onClick={() => void save()}>
              {channel ? "Save changes" : "Add channel"}
            </SaveButton>
            <Btn kind="ghost" type="button" onClick={onBack}>
              Cancel
            </Btn>
          </div>
        </div>
      </div>
    </div>
  );
}

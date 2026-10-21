import { useState, type FormEvent } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { SaveButton, useSave } from "../Feedback";
import { ErrorWell, Field, inputClass, textareaClass } from "../Field";
import { FieldInput } from "../FieldInput";
import type { Channel, ChannelAdapter, ChannelField } from "../gen/silo/v1/ui_pb";
import { widePage } from "../PageHead";
import { Step } from "../Step";
import { ToggleRow } from "../Switch";
import { AdapterHead } from "./AdapterHead";
import { GuideCard } from "./GuideCard";
import { t } from "../i18n.ts";

// A field that fits beside another: not a secret (tokens are long) or a textarea.
const isShort = (f: ChannelField) => !f.secret && f.type !== "textarea";

export function ChannelForm({
  botId,
  adapter,
  channel,
  onBack,
  prefill,
  onSaved,
}: {
  botId: string;
  adapter: ChannelAdapter;
  channel?: Channel;
  onBack: () => void;
  // A Bot's setup offer: plain fields filled in for a new channel.
  prefill?: Record<string, string>;
  // A new channel was made (instead of going back).
  onSaved?: (ch: Channel) => void;
}) {
  const [name, setName] = useState(channel?.name ?? "");
  const [enabled, setEnabled] = useState(channel?.enabled ?? true);
  const [inbound, setInbound] = useState(channel?.inbound ?? true);
  const [prompt, setPrompt] = useState(channel?.prompt ?? "");
  const [config, setConfig] = useState<Record<string, string>>(() => ({ ...(prefill ?? {}), ...(channel?.config ?? {}) }));
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const saver = useSave();
  const [err, setErr] = useState("");

  const setCfg = (k: string, v: string) => setConfig((c) => ({ ...c, [k]: v }));

  // Credentials come first; everything else the adapter declares is behavior.
  const connect = adapter.fields.filter((f) => f.secret || f.required);
  const behavior = adapter.fields.filter((f) => !(f.secret || f.required));
  const filled = (f: ChannelField) =>
    f.type === "toggle" || (f.secret ? !!secrets[f.key]?.trim() || !!channel?.secretsSet?.includes(f.key) : !!config[f.key]?.trim());
  const connected = !!name.trim() && connect.filter((f) => f.required).every(filled);
  const nameAlone = !connect.some(isShort);

  const input = (f: ChannelField) => (
    <div key={f.key} className={isShort(f) ? "" : "sm:col-span-2"}>
      <FieldInput
        field={f}
        value={f.secret ? secrets[f.key] ?? "" : config[f.key] ?? ""}
        setValue={(v) => (f.secret ? setSecrets((s) => ({ ...s, [f.key]: v })) : setCfg(f.key, v))}
        isSet={channel?.secretsSet?.includes(f.key)}
      />
    </div>
  );

  async function save(e: FormEvent) {
    e.preventDefault();
    if (busy || (!channel && !name.trim())) return;
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
          const made = await ui.createChannel({ botId, adapter: adapter.slug, name, enabled, inbound, prompt, config, secrets: cleanSecrets });
          if (onSaved) {
            onSaved(made);
            return;
          }
        }
      });
      if (!channel && onSaved) return;
      onBack();
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={widePage}>
      <AdapterHead
        adapter={adapter}
        title={channel ? channel.name : adapter.name}
        subtitle={channel ? adapter.name : t("Add a {{name}} channel", { name: adapter.name })}
        onBack={onBack}
      />

      <div className={`grid items-start gap-6 ${adapter.guide ? "lg:grid-cols-[minmax(0,1fr)_360px]" : "max-w-3xl"}`}>
        {adapter.guide ? (
          <div className="min-w-0 lg:order-2">
            <GuideCard guide={adapter.guide} title={t("{{name}} setup guide", { name: adapter.name })} />
          </div>
        ) : null}

        <form className="min-w-0 space-y-4 lg:order-1" onSubmit={(e) => void save(e)}>
          <Step
            n={1}
            title={connect.length ? t("Connect") : t("Name it")}
            note={
              connect.length
                ? t("What {{name}} needs to sign in. Secrets are write-only: once saved they are never shown again.", { name: adapter.name })
                : t("This is how the channel appears in the list. {{name}} signs in on the next screen.", { name: adapter.name })
            }
            done={connected}
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t("Name")} required className={nameAlone ? "sm:col-span-2" : ""}>
                <input className={inputClass} value={name} placeholder={adapter.name} autoFocus={!channel} onChange={(e) => setName(e.target.value)} />
              </Field>
              {connect.map(input)}
            </div>
          </Step>

          <Step n={2} title={t("Behavior")} note={t("How the Bot answers on {{name}}.", { name: adapter.name })}>
            {behavior.length ? <div className="grid gap-4 sm:grid-cols-2">{behavior.map(input)}</div> : null}

            <div className="grid gap-3 sm:grid-cols-2">
              <ToggleRow className="rounded-control bg-well px-3.5 py-3" label={t("Enabled")} hint={t("Connect this channel when saved.")} on={enabled} onChange={setEnabled} />
              <ToggleRow
                className="rounded-control bg-well px-3.5 py-3"
                label={t("Deliver messages to the Bot")}
                hint={t("Off makes it send-only, used by tools.")}
                on={inbound}
                onChange={setInbound}
              />
            </div>

            <Field label={t("Prompt")} hint={t("Extra instructions for runs on this channel, e.g. “This is WhatsApp; keep replies short.”")}>
              <textarea
                className={`${textareaClass} h-[130px] font-mono text-[13px] leading-5`}
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder={t("Keep answers concise...")}
              />
            </Field>
          </Step>

          {err ? <ErrorWell>{err}</ErrorWell> : null}

          <div className="flex items-center gap-2.5">
            <SaveButton type="submit" state={saver.state} disabled={busy || (!channel && !name.trim())}>
              {channel ? t("Save changes") : t("Add channel")}
            </SaveButton>
            <Btn kind="ghost" type="button" onClick={onBack}>
              {t("Cancel")}
            </Btn>
          </div>
        </form>
      </div>
    </div>
  );
}

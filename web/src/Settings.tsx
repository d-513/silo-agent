import { ProactivitySettings } from "./Proactivity";
import { RotateCcw, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ui } from "./api";
import { ArmedButton, SaveButton, useSave } from "./Feedback";
import { Field, Panel, inputClass, textareaClass } from "./Field";
import { Select } from "./Select";
import { UI, type Bot, type ModelOption } from "./gen/silo/v1/ui_pb";
import { reload, setBot } from "./query";
import { modelOptions } from "./modelOptions";
import { fail } from "./errors";
import { MailSettings } from "./Mail";
import { TimeZoneSettings } from "./TimeZone";
import { locale, t } from "./i18n.ts";
import { botLanguageOptions, flagOf, mixed } from "./languages.ts";

export function PromptWell({
  label,
  hint,
  value,
  onChange,
}: {
  label: string;
  hint: string;
  value: string;
  onChange: (s: string) => void;
}) {
  const over = value.length > 8000;
  return (
    <Field
      label={label}
      hint={hint}
      className="min-w-0"
      headerRight={<span className={`font-mono text-[11px] ${over ? "text-vermilion" : "text-ink-3"}`}>{value.length}/8000</span>}
    >
      <textarea
        className={`${textareaClass} h-[220px] font-mono text-[13px] leading-5`}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  );
}

function DangerRow({ title, note, action }: { title: string; note: string; action: ReactNode }) {
  return (
    <div className="flex items-center gap-4 px-5 py-4 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none max-wide:flex-col max-wide:items-stretch">
      <div className="min-w-0 flex-1">
        <div className="font-medium">{title}</div>
        <p className="text-[12.5px] leading-[18px] text-ink-2">{note}</p>
      </div>
      <div className="shrink-0">{action}</div>
    </div>
  );
}

const noModels: ModelOption[] = [];

export function SettingsPane({
  bot,
  onError,
}: {
  bot: Bot;
  onError: (s: string) => void;
}) {
  const [name, setName] = useState(bot.name);
  const [description, setDescription] = useState(bot.description);
  const [soul, setSoul] = useState(bot.soul);
  const [model, setModel] = useState(bot.model);
  const [language, setLanguage] = useState(bot.language);
  const models = useQuery(UI.method.listModels, { botId: bot.id }).data?.models ?? noModels;
  const saver = useSave();
  const [dangerBusy, setDangerBusy] = useState(false);
  const nav = useNavigate();
  useEffect(() => {
    setName(bot.name);
    setDescription(bot.description);
    setSoul(bot.soul);
    setModel(bot.model);
    setLanguage(bot.language);
    setDangerBusy(false);
  }, [bot.id]);
  // Drop a stored model the operator has since removed from the allowlist, so
  // saving an unrelated field does not fail validation.
  const selectedModel = models.length === 0 || models.some((m) => m.id === model) ? model : "";
  async function save(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    onError("");
    try {
      const next = await saver.run(() => ui.updateBot({
        id: bot.id,
        name: name.trim(),
        description: description.trim(),
        soul,
        autoApprove: bot.autoApprove,
        model: selectedModel,
        language,
      }));
      setBot(next);
      setSoul(next.soul);
      setModel(next.model);
      setLanguage(next.language);
    } catch (ex) {
      onError(fail(ex));
    }
  }
  async function go(which: "reset" | "delete") {
    setDangerBusy(true);
    onError("");
    try {
      if (which === "reset") {
        setBot(await ui.resetContainer({ id: bot.id }));
      } else {
        await ui.deleteBot({ id: bot.id });
        void reload(UI.method.listBots);
        void nav({ to: "/" });
        return;
      }
    } catch (ex) {
      onError(fail(ex));
    } finally {
      setDangerBusy(false);
    }
  }
  return (
    <div className="silo-page pb-12">
      <h2 className="text-title">{t("Settings")}</h2>
      <p className="mb-6 text-ink-2">{t("This Bot only. SOUL is in the prompt and the Bot can edit it too. Memories are on the Memories page.")}</p>
      <form onSubmit={save} className="grid gap-4">
        <Panel title={t("Identity")} note={t("Shown on the folio and in the sidebar.")}>
          <div className="grid gap-4">
            <Field label={t("Name")} required>
              <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label={t("Description")} hint={t("One line. Use it to remember what this machine is for.")}>
              <textarea
                className={`${textareaClass} h-[72px]`}
                placeholder={t("What this machine is for")}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </Field>
          </div>
        </Panel>

        <Panel title={t("Model")} note={t("Default for this Bot's chats. From the operator's allowed list; a conversation can still override it from the composer.")}>
          <Field label={t("Default model")}>
            <Select
              value={selectedModel}
              onChange={setModel}
              emptyLabel={t("No models allowed")}
              options={[
                { value: "", label: t("Operator default") },
                ...modelOptions(models),
              ]}
            />
          </Field>
        </Panel>

        <Panel title={t("Language")} note={t("What this Bot writes to people in: its replies, chat titles, Feed posts and channel messages. It started as your display language.")}>
          <Field label={t("Bot language")} hint={language === mixed ? t("Mixed: the Bot answers in whatever language it is written to in.") : t("The Bot writes in this language unless you ask it for another. It applies from the next reply.")}>
            <Select className="max-w-[320px]" value={language} onChange={setLanguage} options={[{ value: mixed, label: `${flagOf(mixed)} ${t("Mixed", { context: "language" })}` }, ...botLanguageOptions(locale())]} />
          </Field>
        </Panel>

        <Panel title={t("Prompt")} note={t("Injected into the system prompt every run. The Bot can rewrite it.")}>
          <PromptWell label={t("SOUL")} hint={t("Identity, tone, hard rules.")} value={soul} onChange={setSoul} />
        </Panel>

        <div className="flex items-center gap-3">
          <SaveButton type="submit" state={saver.state} disabled={!name.trim()}>
            {t("Save changes")}
          </SaveButton>
        </div>
      </form>

      <ProactivitySettings bot={bot} onError={onError} />

      <TimeZoneSettings bot={bot} onError={onError} />

      {/* A Bot has a mailbox only while the operator has mail set up. */}
      {bot.mailAddress ? <MailSettings bot={bot} onError={onError} /> : null}

      <Panel
        title={t("Dangerous")}
        note={t("The first click arms, the second acts. These cannot be undone from here.")}
        tone="danger"
        padded={false}
        className="mt-8"
      >
        <DangerRow
          title={t("Reset container")}
          note={t("Stops and deletes the box. Workspace and Chrome profile stay. Start Bot makes a new one.")}
          action={
            <ArmedButton
              disabled={dangerBusy}
              armedLabel={t("Click again to reset")}
              icon={<RotateCcw size={14} />}
              onConfirm={() => void go("reset")}
            >
              {t("Reset")}
            </ArmedButton>
          }
        />
        <DangerRow
          title={t("Delete Bot")}
          note={t("Chats, secrets, connectors, the container, and files on disk.")}
          action={
            <ArmedButton
              disabled={dangerBusy}
              icon={<Trash2 size={14} />}
              onConfirm={() => void go("delete")}
            >
              {t("Delete")}
            </ArmedButton>
          }
        />
      </Panel>
    </div>
  );
}

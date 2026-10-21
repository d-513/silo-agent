import { ProactivitySettings } from "./Proactivity";
import { RotateCcw, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ui } from "./api";
import { ArmedButton, useSave } from "./Feedback";
import { Field, SettingGroup, SettingRow, inputClass, textareaClass } from "./Field";
import { SaveBar } from "./SaveBar";
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

const noModels: ModelOption[] = [];

// The fields the page's Save bar saves; Proactivity, Time zone and Mail apply
// on their own.
type Form = { name: string; description: string; soul: string; model: string; language: string };
const formKeys = ["name", "description", "soul", "model", "language"] as const;

export function SettingsPane({
  bot,
  onError,
}: {
  bot: Bot;
  onError: (s: string) => void;
}) {
  // Only what the owner changed sits on top of the cached Bot.
  const [edits, setEdits] = useState<Partial<Form>>({});
  const [saveErr, setSaveErr] = useState("");
  const models = useQuery(UI.method.listModels, { botId: bot.id }).data?.models ?? noModels;
  const saver = useSave();
  const [dangerBusy, setDangerBusy] = useState(false);
  const nav = useNavigate();
  useEffect(() => {
    setEdits({});
    setSaveErr("");
    setDangerBusy(false);
  }, [bot.id]);
  const form: Form = {
    name: edits.name ?? bot.name,
    description: edits.description ?? bot.description,
    soul: edits.soul ?? bot.soul,
    model: edits.model ?? bot.model,
    language: edits.language ?? bot.language,
  };
  const dirty = formKeys.filter((k) => form[k] !== bot[k]).length;
  const edit = (k: keyof Form) => (v: string) => {
    setEdits((e) => ({ ...e, [k]: v }));
    setSaveErr("");
  };
  // Drop a stored model the operator has since removed from the allowlist, so
  // saving an unrelated field does not fail validation.
  const selectedModel = models.length === 0 || models.some((m) => m.id === form.model) ? form.model : "";

  async function save() {
    if (!form.name.trim()) {
      setSaveErr(t("A Bot needs a name."));
      return;
    }
    if (dirty === 0) return;
    setSaveErr("");
    try {
      const next = await saver.run(() =>
        ui.updateBot({
          id: bot.id,
          name: form.name.trim(),
          description: form.description.trim(),
          soul: form.soul,
          autoApprove: bot.autoApprove,
          model: selectedModel,
          language: form.language,
        }),
      );
      setBot(next);
      setEdits({});
    } catch (ex) {
      setSaveErr(fail(ex));
    }
  }

  // ⌘/Ctrl+S saves; the ref keeps the listener on the latest form.
  const saveRef = useRef(save);
  saveRef.current = save;
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        void saveRef.current();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

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

  const over = form.soul.length > 8000;
  return (
    <div className="silo-page silo-page-wide">
      <h2 className="text-title">{t("General")}</h2>
      <p className="mb-6 text-ink-2">{t("This Bot only. SOUL is in the prompt and the Bot can edit it too. Memories are on the Memories page.")}</p>
      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          void save();
        }}
      >
        <SettingGroup title={t("Identity")} note={t("Shown on the folio and in the sidebar.")}>
          <SettingRow label={t("Name")} error={form.name.trim() ? undefined : t("A Bot needs a name.")}>
            <input className={inputClass} aria-label={t("Name")} value={form.name} onChange={(e) => edit("name")(e.target.value)} />
          </SettingRow>
          <SettingRow label={t("Description")} hint={t("One line. Use it to remember what this machine is for.")}>
            <textarea
              className={`${textareaClass} h-[60px]`}
              aria-label={t("Description")}
              placeholder={t("What this machine is for")}
              value={form.description}
              onChange={(e) => edit("description")(e.target.value)}
            />
          </SettingRow>
          <SettingRow
            label={t("Language")}
            hint={
              form.language === mixed
                ? t("Mixed: the Bot answers in whatever language it is written to in.")
                : t("What this Bot writes to people in: its replies, chat titles, Feed posts and channel messages. It started as your display language.")
            }
          >
            <Select
              value={form.language}
              onChange={edit("language")}
              options={[{ value: mixed, label: `${flagOf(mixed)} ${t("Mixed", { context: "language" })}` }, ...botLanguageOptions(locale())]}
            />
          </SettingRow>
        </SettingGroup>

        <SettingGroup title={t("Behaviour")} note={t("The model it thinks with, how much it does unasked, and the clock it keeps.")}>
          <SettingRow
            label={t("Default model")}
            hint={t("Default for this Bot's chats. From the operator's allowed list; a conversation can still override it from the composer.")}
          >
            <Select
              value={selectedModel}
              onChange={edit("model")}
              emptyLabel={t("No models allowed")}
              options={[{ value: "", label: t("Operator default") }, ...modelOptions(models)]}
            />
          </SettingRow>
          <ProactivitySettings bot={bot} onError={onError} />
          <TimeZoneSettings bot={bot} onError={onError} />
        </SettingGroup>

        <SettingGroup title={t("SOUL")} note={t("Injected into the system prompt every run. The Bot can rewrite it.")}>
          <SettingRow
            stack
            label={t("Identity, tone, hard rules.")}
            meta={<span className={`font-mono text-[11px] ${over ? "text-vermilion" : "text-ink-3"}`}>{form.soul.length}/8000</span>}
          >
            <textarea
              className={`${textareaClass} h-[360px] font-mono text-[13px] leading-5`}
              aria-label={t("SOUL")}
              value={form.soul}
              onChange={(e) => edit("soul")(e.target.value)}
            />
          </SettingRow>
        </SettingGroup>

        <SaveBar
          dirty={dirty}
          state={saver.state}
          error={saveErr}
          savedLabel={t("Saved")}
          onSave={() => void save()}
          onDiscard={() => {
            setEdits({});
            setSaveErr("");
          }}
        />
      </form>

      <div className="mt-6">
        {/* A Bot has a mailbox only while the operator has mail set up. */}
        {bot.mailAddress ? <MailSettings bot={bot} onError={onError} /> : null}

        <SettingGroup title={t("Dangerous")} note={t("The first click arms, the second acts. These cannot be undone from here.")} tone="danger">
          <SettingRow label={t("Reset container")} hint={t("Stops and deletes the box. Workspace and Chrome profile stay. Start Bot makes a new one.")}>
            <ArmedButton
              disabled={dangerBusy}
              armedLabel={t("Click again to reset")}
              icon={<RotateCcw size={14} />}
              onConfirm={() => void go("reset")}
            >
              {t("Reset")}
            </ArmedButton>
          </SettingRow>
          <SettingRow label={t("Delete Bot")} hint={t("Chats, secrets, connectors, the container, and files on disk.")}>
            <ArmedButton disabled={dangerBusy} icon={<Trash2 size={14} />} onConfirm={() => void go("delete")}>
              {t("Delete")}
            </ArmedButton>
          </SettingRow>
        </SettingGroup>
      </div>
    </div>
  );
}

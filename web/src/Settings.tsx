import { RotateCcw, Trash2 } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Field, Panel, inputClass, textareaClass } from "./Field";
import { Select } from "./Select";
import type { Bot, ModelOption } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function PromptWell({
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
      headerRight={<span className={`font-mono text-[11px] ${over ? "text-carmine" : "text-stone"}`}>{value.length}/8000</span>}
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
    <div className="flex items-center gap-4 border-b border-thread-2 px-4 py-3 last:border-b-0 max-wide:flex-col max-wide:items-stretch">
      <div className="min-w-0 flex-1">
        <div className="font-medium">{title}</div>
        <p className="text-[12px] text-stone">{note}</p>
      </div>
      <div className="shrink-0">{action}</div>
    </div>
  );
}

export function SettingsPane({
  bot,
  onSaved,
  onError,
  onRefresh,
}: {
  bot: Bot;
  onSaved: (b: Bot) => void;
  onError: (s: string) => void;
  onRefresh: () => void;
}) {
  const [name, setName] = useState(bot.name);
  const [description, setDescription] = useState(bot.description);
  const [soul, setSoul] = useState(bot.soul);
  const [memory, setMemory] = useState(bot.memory);
  const [model, setModel] = useState(bot.model);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [arm, setArm] = useState<"reset" | "delete" | "">("");
  const [dangerBusy, setDangerBusy] = useState(false);
  const nav = useNavigate();
  useEffect(() => {
    setName(bot.name);
    setDescription(bot.description);
    setSoul(bot.soul);
    setMemory(bot.memory);
    setModel(bot.model);
    setArm("");
    setDangerBusy(false);
  }, [bot.id]);
  useEffect(() => {
    let dead = false;
    ui.listModels({ botId: bot.id })
      .then((r) => {
        if (!dead) setModels(r.models);
      })
      .catch(() => {});
    return () => {
      dead = true;
    };
  }, [bot.id]);
  // Drop a stored model the operator has since removed from the allowlist, so
  // saving an unrelated field does not fail validation.
  const selectedModel = models.length === 0 || models.some((m) => m.id === model) ? model : "";
  async function save(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setSaved(false);
    onError("");
    try {
      const next = await ui.updateBot({
        id: bot.id,
        name: name.trim(),
        description: description.trim(),
        soul,
        memory,
        autoApprove: bot.autoApprove,
        model: selectedModel,
      });
      onSaved(next);
      setSoul(next.soul);
      setMemory(next.memory);
      setModel(next.model);
      setSaved(true);
    } catch (ex) {
      onError(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  async function go(which: "reset" | "delete") {
    if (arm !== which) {
      setArm(which);
      return;
    }
    setDangerBusy(true);
    onError("");
    try {
      if (which === "reset") {
        onSaved(await ui.resetContainer({ id: bot.id }));
        setArm("");
        onRefresh();
      } else {
        await ui.deleteBot({ id: bot.id });
        onRefresh();
        nav("/");
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
      <h2 className="text-[22px] font-medium tracking-tight">Settings</h2>
      <p className="mb-6 text-stone">This Bot only. SOUL and MEMORY are in the prompt; the Bot can edit them too.</p>
      <form onSubmit={save} className="grid gap-4">
        <Panel title="Identity" note="Shown on the folio and in the run header.">
          <div className="grid gap-4">
            <Field label="Name" required>
              <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="Description" hint="One line. Use it to remember what this machine is for.">
              <textarea
                className={`${textareaClass} h-[72px]`}
                placeholder="What this machine is for"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </Field>
          </div>
        </Panel>

        <Panel title="Model" note="Default for this Bot's chats. From the operator's allowed list; a conversation can still override it from the composer.">
          <Field label="Default model">
            <Select
              value={selectedModel}
              onChange={setModel}
              emptyLabel="No models allowed"
              options={[
                { value: "", label: "Operator default" },
                ...models.map((m) => ({
                  value: m.id,
                  label: m.label || m.id,
                  hint: m.label && m.label !== m.id ? m.id : undefined,
                })),
              ]}
            />
          </Field>
        </Panel>

        <Panel title="Prompt" note="Injected into the system prompt every run. The Bot can rewrite both.">
          <div className="grid grid-cols-1 gap-4 wide:grid-cols-2">
            <PromptWell label="SOUL" hint="Identity, tone, hard rules." value={soul} onChange={setSoul} />
            <PromptWell label="MEMORY" hint="Lasting facts. Compact past 8000." value={memory} onChange={setMemory} />
          </div>
        </Panel>

        <div className="flex items-center gap-3">
          <Btn kind="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? "Saving…" : "Save changes"}
          </Btn>
          {saved && <span className="text-stone">Saved</span>}
        </div>
      </form>

      <Panel
        title="Dangerous"
        note="Second click confirms. These cannot be undone from here."
        tone="danger"
        padded={false}
        className="mt-8"
      >
        <DangerRow
          title="Reset container"
          note="Stops and deletes the box. Workspace and Chrome profile stay. Start Bot makes a new one."
          action={
            <Btn
              kind="secondary"
              type="button"
              disabled={dangerBusy}
              icon={<RotateCcw size={12} />}
              onClick={() => void go("reset")}
            >
              {arm === "reset" ? "Reset?" : "Reset"}
            </Btn>
          }
        />
        <DangerRow
          title="Delete Bot"
          note="Chats, secrets, connectors, the container, and files on disk."
          action={
            <Btn
              kind="deny"
              type="button"
              disabled={dangerBusy}
              icon={<Trash2 size={12} />}
              onClick={() => void go("delete")}
            >
              {arm === "delete" ? "Delete?" : "Delete"}
            </Btn>
          }
        />
      </Panel>
    </div>
  );
}

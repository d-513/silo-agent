import { Check, Send } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Btn } from "../Btn";
import { defaultFormValues, normalizeOptions, parseBlocks, type DashboardBlock } from "../dashboardLayout";
import { Spinner } from "../Feedback";
import { Field, inputClass, textareaClass } from "../Field";
import type { DashboardCard } from "../gen/silo/v1/ui_pb";
import { feedStamp } from "../format";
import { chatLink } from "../links";
import { Lamp, StatusWord } from "../Lamp";
import { Md } from "../Md";
import { Select } from "../Select";
import { Switch } from "../Switch";
import { t } from "../i18n.ts";

// An ask is a decision the Bot needs from the owner: a slip with the vermilion
// ribbon, the question, its context and controls, and one submit that opens a
// chat with the chosen values. "Not now" puts it away; "Don't suggest this
// again" also tells the Bot never to raise the topic again.

export function sourceLabel(p: { sourceKind: string; sourceName: string }) {
  switch (p.sourceKind) {
    case "automation":
      return t("Automation “{{name}}”", { name: p.sourceName });
    case "channel":
      return t("Channel “{{name}}”", { name: p.sourceName });
    case "chat":
      return p.sourceName || t("Chat");
    default:
      return "";
  }
}

export function BlockRenderer({
  block,
  values,
  onChange,
  disabled,
}: {
  block: DashboardBlock;
  values: Record<string, unknown>;
  onChange: (name: string, val: unknown) => void;
  disabled?: boolean;
}) {
  switch (block.type) {
    case "markdown":
      if (!block.text) return null;
      return (
        <div className="silo-reply text-[13.5px]">
          <Md text={block.text} />
        </div>
      );

    case "kv": {
      const items = block.items ?? [];
      if (items.length === 0) return null;
      return (
        <dl className="overflow-hidden rounded-control bg-well">
          {items.map((item, idx) => (
            <div
              key={`${item.label}-${idx}`}
              className="flex items-baseline justify-between gap-4 px-3 py-2 shadow-[inset_0_-1px_0_var(--color-line-strong)] last:shadow-none"
            >
              <dt className="shrink-0 text-[12px] font-medium text-ink-3">{item.label}</dt>
              <dd className="min-w-0 truncate font-mono text-[12.5px] text-ink">{item.value}</dd>
            </div>
          ))}
        </dl>
      );
    }

    case "metrics": {
      const items = block.items ?? [];
      if (items.length === 0) return null;
      return (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {items.map((m, idx) => {
            const toneCls = m.tone === "good" ? "text-emerald" : m.tone === "bad" ? "text-vermilion" : m.tone === "warn" ? "text-ink-2" : "text-ink-3";
            return (
              <div key={`${m.label}-${idx}`} className="rounded-control bg-well px-3 py-2.5">
                <div className="truncate text-[11.5px] font-medium text-ink-3">{m.label}</div>
                <div className="mt-0.5 flex items-baseline gap-1.5">
                  <span className="font-mono text-[16px] font-semibold tracking-tight text-ink">{m.value}</span>
                  {m.delta ? <span className={`font-mono text-[11px] font-medium ${toneCls}`}>{m.delta}</span> : null}
                </div>
              </div>
            );
          })}
        </div>
      );
    }

    case "diff": {
      const content = block.patch ?? block.after ?? "";
      return (
        <div className="overflow-hidden rounded-control bg-well">
          {block.path ? <div className="border-b border-line px-3 py-1.5 font-mono text-[11.5px] font-medium text-ink-2">{block.path}</div> : null}
          <pre className="max-h-56 overflow-auto p-3 font-mono text-[12px] leading-5 text-ink">{content}</pre>
        </div>
      );
    }

    case "switch": {
      const checked = Boolean(values[block.name]);
      return (
        <div className="flex items-center justify-between gap-3 rounded-control bg-well/60 px-3 py-2">
          <div className="min-w-0">
            <div className="text-[13px] font-medium text-ink">{block.label}</div>
            {block.hint ? <div className="text-[12px] leading-4 text-ink-3">{block.hint}</div> : null}
          </div>
          <Switch on={checked} disabled={disabled} onChange={(v) => onChange(block.name, v)} />
        </div>
      );
    }

    case "select": {
      const opts = normalizeOptions(block.options);
      const val = typeof values[block.name] === "string" ? (values[block.name] as string) : (opts[0]?.value ?? "");
      return (
        <Field label={block.label} hint={block.hint}>
          <Select value={val} options={opts} disabled={disabled} onChange={(v) => onChange(block.name, v)} />
        </Field>
      );
    }

    case "segmented": {
      const opts = normalizeOptions(block.options);
      const val = typeof values[block.name] === "string" ? (values[block.name] as string) : (opts[0]?.value ?? "");
      return (
        <Field label={block.label} hint={block.hint}>
          <div className="inline-flex w-full flex-wrap gap-1 rounded-control bg-well p-1">
            {opts.map((o) => {
              const active = o.value === val;
              return (
                <button
                  key={o.value}
                  type="button"
                  disabled={disabled}
                  onClick={() => onChange(block.name, o.value)}
                  className={`flex-1 rounded-sm px-2.5 py-1 text-[12.5px] font-medium transition-colors duration-[140ms] ${
                    active ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:text-ink"
                  }`}
                >
                  {o.label}
                </button>
              );
            })}
          </div>
        </Field>
      );
    }

    case "checkboxes": {
      const opts = normalizeOptions(block.options);
      const selected = Array.isArray(values[block.name]) ? (values[block.name] as string[]) : [];
      const toggle = (v: string) => {
        if (selected.includes(v)) {
          onChange(
            block.name,
            selected.filter((x) => x !== v),
          );
        } else {
          onChange(block.name, [...selected, v]);
        }
      };
      return (
        <Field label={block.label} hint={block.hint}>
          <div className="space-y-1.5">
            {opts.map((o) => {
              const on = selected.includes(o.value);
              return (
                <button
                  key={o.value}
                  type="button"
                  disabled={disabled}
                  onClick={() => toggle(o.value)}
                  className="flex w-full items-center gap-2.5 rounded-control bg-well/60 px-3 py-2 text-left text-[13px] text-ink transition-colors hover:bg-well"
                >
                  <span
                    className={`grid h-4 w-4 shrink-0 place-items-center rounded-[4px] transition-colors ${
                      on ? "bg-ink text-on-ink" : "bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]"
                    }`}
                  >
                    {on ? <Check size={11} /> : null}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium leading-4">{o.label}</span>
                    {o.hint ? <span className="mt-0.5 block text-[11.5px] text-ink-3">{o.hint}</span> : null}
                  </span>
                </button>
              );
            })}
          </div>
        </Field>
      );
    }

    case "input": {
      const val = typeof values[block.name] === "string" ? (values[block.name] as string) : "";
      return (
        <Field label={block.label} hint={block.hint}>
          <input
            type="text"
            className={inputClass}
            value={val}
            disabled={disabled}
            placeholder={block.placeholder}
            onChange={(e) => onChange(block.name, e.target.value)}
          />
        </Field>
      );
    }

    case "textarea": {
      const val = typeof values[block.name] === "string" ? (values[block.name] as string) : "";
      return (
        <Field label={block.label} hint={block.hint}>
          <textarea
            rows={block.rows ?? 3}
            className={textareaClass}
            value={val}
            disabled={disabled}
            placeholder={block.placeholder}
            onChange={(e) => onChange(block.name, e.target.value)}
          />
        </Field>
      );
    }
  }
}

export function AskSlip({
  botId,
  card,
  onSubmit,
  onDismiss,
}: {
  botId: string;
  card: DashboardCard;
  // apart answers in a chat of its own instead of the main one.
  onSubmit: (card: DashboardCard, values: Record<string, unknown>, apart?: boolean) => Promise<void>;
  onDismiss: (card: DashboardCard, suppress: boolean) => Promise<void>;
}) {
  const blocks = parseBlocks(card.blocksJson);
  const [values, setValues] = useState<Record<string, unknown>>(() => defaultFormValues(blocks));
  const [busy, setBusy] = useState<"" | "submit" | "dismiss">("");
  useEffect(() => {
    setValues((prev) => ({
      ...defaultFormValues(parseBlocks(card.blocksJson)),
      ...prev,
    }));
  }, [card.blocksJson]);
  const src = sourceLabel(card);

  async function act(kind: "submit" | "dismiss", fn: () => Promise<void>) {
    setBusy(kind);
    try {
      await fn();
    } finally {
      setBusy("");
    }
  }

  return (
    <article className="rise relative flex flex-col overflow-hidden rounded-card bg-surface py-4 pr-4 pl-5 shadow-card">
      <span aria-hidden className="absolute inset-y-0 left-0 w-0.5 bg-vermilion" />
      <div className="flex min-w-0 items-center gap-2 text-[12px]">
        <Lamp status="needs_you" />
        <StatusWord status="needs_you" />
        {src ? (
          <span className="min-w-0 truncate text-ink-3">
            ·{" "}
            {card.sourceKind === "chat" && card.chatId ? (
              <Link {...chatLink(botId, card.chatId)} className="hover:text-ink hover:underline">
                {src}
              </Link>
            ) : (
              src
            )}
          </span>
        ) : null}
        <span className="ml-auto shrink-0 font-mono text-[11.5px] text-ink-3">{feedStamp(card.updatedAt || card.createdAt)}</span>
      </div>
      <h3 className="mt-1.5 text-[15px] leading-5 font-medium tracking-[-0.01em] text-ink">{card.title}</h3>
      {card.summary ? <p className="mt-0.5 text-[13px] leading-5 text-ink-2">{card.summary}</p> : null}
      {blocks.length > 0 ? (
        <div className="mt-3 space-y-3">
          {blocks.map((b, i) => (
            <BlockRenderer key={i} block={b} values={values} disabled={busy !== ""} onChange={(name, val) => setValues((prev) => ({ ...prev, [name]: val }))} />
          ))}
        </div>
      ) : null}
      <div className="mt-4 flex flex-wrap items-center gap-1.5">
        <Btn
          kind="primary"
          size="sm"
          disabled={busy !== ""}
          icon={busy === "submit" ? <Spinner size={12} tone="on-ink" /> : <Send size={13} />}
          title={t("Answers in the main chat. Ctrl/⌘-click for a new chat.")}
          onClick={(e) => {
            const apart = e.metaKey || e.ctrlKey;
            void act("submit", () => onSubmit(card, values, apart));
          }}
        >
          {card.submitLabel || t("Send")}
        </Btn>
        <Btn kind="ghost" size="sm" disabled={busy !== ""} onClick={() => void act("dismiss", () => onDismiss(card, false))}>
          {t("Not now")}
        </Btn>
        {card.key ? (
          <button
            type="button"
            disabled={busy !== ""}
            onClick={() => void act("dismiss", () => onDismiss(card, true))}
            className="ml-auto text-[12px] text-ink-3 transition-colors duration-[160ms] hover:text-ink disabled:opacity-50"
          >
            {t("Don't suggest this again")}
          </button>
        ) : null}
      </div>
    </article>
  );
}

import { Check, MessageCircleQuestionMark, Send } from "lucide-react";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Link } from "@tanstack/react-router";
import { Btn, pressClass } from "../Btn";
import { Spinner } from "../Feedback";
import { Field, inputClass, textareaClass } from "../Field";
import type { DashboardCard } from "../gen/silo/v1/ui_pb";
import { feedStamp } from "../format";
import { apart, chatLink } from "../links";
import { Md } from "../Md";
import { Select } from "../Select";
import { sourceLabel } from "../sources";
import { Switch } from "../Switch";
import { Tip } from "../Tip";
import { t } from "../i18n.ts";
import { defaultFormValues, isDisplay, normalizeOptions, parseBlocks, type DashboardBlock } from "./blocks";

// An ask is a decision the Bot needs from the owner. It is a question, not an
// alarm: no vermilion (that is for a paused run). The upper half is the Bot
// speaking (the question in its serif, then its context); the lower half is a
// well, the owner's reply tray, with the controls and one submit that answers
// with the chosen values. "Not now" puts it away; on the Dashboard, "Don't
// suggest this again" also tells the Bot never to raise the topic again. The
// same card is on the Dashboard and, when the Bot asked in a chat (or into the
// main chat from elsewhere), in the thread.

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
        <div className="flex items-center justify-between gap-3 rounded-control bg-surface px-3 py-2.5 shadow-card">
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
          <Choices label={block.label} options={opts} picked={[val]} disabled={disabled} onPick={(v) => onChange(block.name, v)} />
        </Field>
      );
    }

    case "checkboxes": {
      const opts = normalizeOptions(block.options);
      const selected = Array.isArray(values[block.name]) ? (values[block.name] as string[]) : [];
      const toggle = (v: string) => onChange(block.name, selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v]);
      return (
        <Field label={block.label} hint={block.hint}>
          <Choices multi label={block.label} options={opts} picked={selected} disabled={disabled} onPick={toggle} />
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

// Choices are a question's options, one or many. Short plain labels sit as
// chips that wrap; a hint or a long label makes them rows with room for a
// sentence. A picked option is cobalt-pale on a cobalt ring: it is yours now.
function Choices({
  options,
  picked,
  onPick,
  multi = false,
  label,
  disabled,
}: {
  options: { value: string; label: string; hint?: string }[];
  picked: string[];
  onPick: (value: string) => void;
  multi?: boolean;
  label?: string;
  disabled?: boolean;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const chips = options.length <= 12 && options.every((o) => !o.hint && o.label.length <= 28);
  const at = multi ? -1 : options.findIndex((o) => picked.includes(o.value));
  // One choice is a radio group: arrows move the pick, one tab stop.
  const keys = (e: KeyboardEvent) => {
    if (multi || disabled) return;
    const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const n = options.length;
    const j = ((((at < 0 ? 0 : at) + step) % n) + n) % n;
    refs.current[j]?.focus();
    onPick(options[j].value);
  };
  return (
    <div role={multi ? "group" : "radiogroup"} aria-label={label} onKeyDown={keys} className={chips ? "flex flex-wrap gap-1.5" : "space-y-1.5"}>
      {options.map((o, i) => {
        const on = picked.includes(o.value);
        return (
          <button
            key={o.value}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            role={multi ? "checkbox" : "radio"}
            aria-checked={on}
            tabIndex={multi || on || (at < 0 && i === 0) ? 0 : -1}
            disabled={disabled}
            onClick={() => onPick(o.value)}
            className={`${pressClass} font-medium disabled:cursor-not-allowed disabled:opacity-60 ${
              chips
                ? "inline-flex h-8 items-center gap-2 rounded-sm pr-3 pl-2.5 text-[13px]"
                : "flex w-full items-start gap-2.5 rounded-control px-3 py-2.5 text-left text-[13px]"
            } ${
              on
                ? "bg-cobalt-pale text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)]"
                : "bg-surface text-ink-2 shadow-[inset_0_0_0_1px_var(--color-line-strong)] hover:text-ink hover:shadow-[inset_0_0_0_1px_var(--color-ink-3)]"
            }`}
          >
            <Mark multi={multi} on={on} row={!chips} />
            {chips ? (
              o.label
            ) : (
              <span className="min-w-0 flex-1">
                <span className="block leading-4">{o.label}</span>
                {o.hint ? <span className="mt-1 block text-[12px] leading-4 font-normal text-ink-3">{o.hint}</span> : null}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

// Mark is a choice's box (many) or dot (one), filled cobalt once picked.
function Mark({ multi, on, row }: { multi: boolean; on: boolean; row: boolean }) {
  return (
    <span
      aria-hidden
      className={`grid h-3.5 w-3.5 shrink-0 place-items-center transition-[background-color,box-shadow] duration-[200ms] ease-quiet ${row ? "mt-px" : ""} ${
        multi ? "rounded-[4px]" : "rounded-full"
      } ${on ? "bg-cobalt text-on-accent" : "bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]"}`}
    >
      {on ? multi ? <Check size={10} strokeWidth={3} className="ask-tick" /> : <span className="ask-tick h-1.5 w-1.5 rounded-full bg-on-accent" /> : null}
    </span>
  );
}

// The server words a card's submit "Send" when the Bot gave it no label of
// its own; that one is ours to translate.
function submitWord(label: string) {
  return !label || label === "Send" ? t("Send") : label;
}

// A quiet button that still answers a hover on the well of the reply tray.
const quietBtn = `${pressClass} inline-flex h-8 items-center rounded-control px-2.5 text-[12.5px] font-medium text-ink-2 hover:bg-pressed hover:text-ink disabled:cursor-not-allowed disabled:opacity-50`;

export function AskSlip({
  botId,
  card,
  onSubmit,
  onDismiss,
  place = "board",
  asked = false,
}: {
  botId: string;
  card: DashboardCard;
  // apart answers in a chat of its own instead of the main one.
  onSubmit: (card: DashboardCard, values: Record<string, unknown>, apart?: boolean) => Promise<void>;
  onDismiss: (card: DashboardCard, suppress: boolean) => Promise<void>;
  // In a thread the card answers in that chat, and an answered one stays as
  // a record.
  place?: "board" | "thread";
  // In a thread: the Bot asked from elsewhere (an automation, a channel) and
  // the question was put into this chat. Only then is its source worth naming.
  asked?: boolean;
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
  const closed = card.status !== "open";
  const board = place === "board";
  const src = board || asked ? sourceLabel(card.sourceKind, card.sourceName) : "";
  // An answered card keeps what it showed; its controls would read as the
  // owner's answer, and the answer is in the thread below it.
  const shown = closed ? blocks.filter(isDisplay) : blocks;
  // The Bot's context reads above; from the first control down is the
  // owner's reply, set on the well like the composer's own ground.
  const cut = shown.findIndex((b) => !isDisplay(b));
  const context = cut < 0 ? shown : shown.slice(0, cut);
  const form = cut < 0 ? [] : shown.slice(cut);
  const question = (card.summary ?? "").trim();
  // ask_owner titles a card with its question's first line when the Bot gave
  // it no title; shown above the question, it would only say it twice.
  const titled = !!card.title && !question.startsWith(card.title.trim());

  async function act(kind: "submit" | "dismiss", fn: () => Promise<void>) {
    setBusy(kind);
    try {
      await fn();
    } finally {
      setBusy("");
    }
  }
  const send = (apartChat = false) => void act("submit", () => onSubmit(card, values, apartChat));

  const change = (name: string, val: unknown) => setValues((prev) => ({ ...prev, [name]: val }));
  const draw = (b: DashboardBlock, i: number) => <BlockRenderer key={i} block={b} values={values} disabled={busy !== "" || closed} onChange={change} />;

  const submit = (
    <Btn
      kind="primary"
      size="sm"
      disabled={busy !== ""}
      icon={busy === "submit" ? <Spinner size={12} tone="on-ink" /> : <Send size={13} />}
      onClick={(e) => send(board && apart(e))}
    >
      {submitWord(card.submitLabel)}
    </Btn>
  );

  return (
    <article className="rise flex flex-col overflow-hidden rounded-card bg-surface shadow-card">
      <div className="px-5 pt-4 pb-4">
        <div className="flex min-w-0 items-center gap-2 text-[12px] leading-4">
          {closed ? (
            <span className="flex shrink-0 items-center gap-1.5 font-medium text-ink-3">
              {card.status === "submitted" ? <Check size={13} className="text-emerald" /> : null}
              {card.status === "submitted" ? t("Answered") : t("Put away")}
            </span>
          ) : (
            <span className="flex shrink-0 items-center gap-1.5 font-medium text-cobalt">
              <MessageCircleQuestionMark size={14} />
              {t("Question for you")}
            </span>
          )}
          {asked ? <span className="shrink-0 text-ink-3">· {t("Asked while you were away")}</span> : null}
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
        {titled ? <h3 className="mt-3 text-[15px] leading-5 font-semibold tracking-[-0.01em] text-ink">{card.title}</h3> : null}
        {question ? (
          <div className={`silo-reply ask-question ${titled ? "mt-1" : "mt-3"} ${closed ? "is-closed" : ""}`}>
            <Md text={question} />
          </div>
        ) : null}
        {context.length ? <div className="mt-3 space-y-3">{context.map(draw)}</div> : null}
      </div>
      {closed ? null : (
        <div
          onKeyDown={(e) => {
            // ⌘/Ctrl+Enter answers from anywhere in the reply, as in the composer.
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && busy === "") {
              e.preventDefault();
              send();
            }
          }}
          className="bg-well px-5 pt-4 pb-4 shadow-[inset_0_1px_0_var(--color-line)]"
        >
          {form.length ? <div className="mb-4 space-y-4">{form.map(draw)}</div> : null}
          <div className="flex flex-wrap items-center gap-1.5">
            {board ? <Tip content={t("Answers in the main chat. Ctrl/⌘-click for a new chat.")}>{submit}</Tip> : submit}
            <button type="button" disabled={busy !== ""} onClick={() => void act("dismiss", () => onDismiss(card, false))} className={quietBtn}>
              {t("Not now")}
            </button>
            {/* A topic the Bot keeps raising on the board can be turned off; in
                a chat the owner just answers or puts it away. */}
            {card.key && board ? (
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
        </div>
      )}
    </article>
  );
}

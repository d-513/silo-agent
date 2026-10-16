import { Check, EyeOff, LayoutGrid, Play, Send, Sparkles, Timer, X } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApprovalCard } from "./Approval";
import { chatLink } from "./links";
import { ui } from "./api";
import { Btn } from "./Btn";
import {
  defaultFormValues,
  normalizeOptions,
  packDashboardCards,
  type DashboardBlock,
  type PackedDashboardCard,
} from "./dashboardLayout";
import { Spinner } from "./Feedback";
import { Field, inputClass, textareaClass } from "./Field";
import { UI, type Approval, type Bot, type Chat, type DashboardCard } from "./gen/silo/v1/ui_pb";
import { feedStamp } from "./format";
import { Md } from "./Md";
import { Select } from "./Select";
import { Switch } from "./Switch";
import { fail } from "./errors";
import { patch, recheck, reloadBot } from "./query";
import { t } from "./i18n.ts";

function sourceLabel(p: { sourceKind: string; sourceName: string }) {
  switch (p.sourceKind) {
    case "automation":
      return `${t("Automation")} · ${p.sourceName}`;
    case "channel":
      return `${t("Channel")} · ${p.sourceName}`;
    case "chat":
      return p.sourceName || t("Chat");
    default:
      return "";
  }
}

function useBentoColumns(ref: React.RefObject<HTMLDivElement | null>): 1 | 2 | 3 {
  const [cols, setCols] = useState<1 | 2 | 3>(2);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const update = (w: number) => {
      if (w >= 960) setCols(3);
      else if (w >= 580) setCols(2);
      else setCols(1);
    };
    update(el.clientWidth);
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) update(entry.contentRect.width);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [ref]);
  return cols;
}

function DashboardBlockRenderer({
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
            const toneCls =
              m.tone === "good"
                ? "text-emerald"
                : m.tone === "bad"
                  ? "text-vermilion"
                  : m.tone === "warn"
                    ? "text-amber"
                    : "text-ink-3";
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
          {block.path ? (
            <div className="border-b border-line px-3 py-1.5 font-mono text-[11.5px] font-medium text-ink-2">
              {block.path}
            </div>
          ) : null}
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

function DashboardCardItem({
  botId,
  item,
  fresh,
  onSubmit,
  onDismiss,
  onHydrate,
}: {
  botId: string;
  item: PackedDashboardCard;
  fresh: boolean;
  onSubmit: (card: DashboardCard, values: Record<string, unknown>) => Promise<void>;
  onDismiss: (card: DashboardCard, suppressKind: boolean) => Promise<void>;
  onHydrate: (card: DashboardCard) => Promise<void>;
}) {
  const { card, blocks, colSpan } = item;
  const [values, setValues] = useState<Record<string, unknown>>(() => defaultFormValues(blocks));
  const [busy, setBusy] = useState<"" | "submit" | "dismiss" | "hydrate">("");

  useEffect(() => {
    setValues((prev) => ({ ...defaultFormValues(blocks), ...prev }));
  }, [card.blocksJson]);

  const spanCls =
    colSpan === 3 ? "col-span-full" : colSpan === 2 ? "col-span-1 md:col-span-2" : "col-span-1";

  const urgent = card.priority === 1;
  const src = sourceLabel(card);

  async function handleSubmit() {
    setBusy("submit");
    try {
      await onSubmit(card, values);
    } finally {
      setBusy("");
    }
  }

  async function handleDismiss(suppress: boolean) {
    setBusy("dismiss");
    try {
      await onDismiss(card, suppress);
    } finally {
      setBusy("");
    }
  }

  async function handleHydrate() {
    setBusy("hydrate");
    try {
      await onHydrate(card);
    } finally {
      setBusy("");
    }
  }

  return (
    <article
      className={`flex flex-col justify-between rounded-card bg-surface px-5 py-4 shadow-card ${
        urgent ? "ring-1 ring-cobalt/30" : ""
      } ${spanCls}`}
    >
      <div>
        <header className="mb-2.5 flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              {fresh ? <span aria-label={t("New")} className="h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" /> : null}
              <h3 className="truncate text-card-title leading-5 text-ink">{card.title}</h3>
            </div>
            <p className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[12px] leading-4 text-ink-3">
              <span className="shrink-0 font-mono">{feedStamp(card.updatedAt || card.createdAt)}</span>
              {src ? (
                <>
                  <span aria-hidden>·</span>
                  {card.sourceKind === "chat" && card.chatId ? (
                    <Link {...chatLink(botId, card.chatId)} className="truncate hover:text-ink hover:underline">
                      {src}
                    </Link>
                  ) : (
                    <span className="truncate">{src}</span>
                  )}
                </>
              ) : null}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {card.key ? (
              <Btn
                kind="ghost"
                size="sm"
                iconOnly
                disabled={busy !== ""}
                title={t("Don't show this kind")}
                aria-label={t("Don't show this kind")}
                icon={<EyeOff size={13} />}
                onClick={() => void handleDismiss(true)}
              />
            ) : null}
            <Btn
              kind="ghost"
              size="sm"
              iconOnly
              disabled={busy !== ""}
              title={t("Dismiss")}
              aria-label={t("Dismiss")}
              icon={<X size={14} />}
              onClick={() => void handleDismiss(false)}
            />
          </div>
        </header>

        {card.summary ? <p className="mb-3 text-[13.5px] leading-5 text-ink-2">{card.summary}</p> : null}

        {blocks.length > 0 ? (
          <div className="space-y-3">
            {blocks.map((b, i) => (
              <DashboardBlockRenderer
                key={i}
                block={b}
                values={values}
                disabled={busy !== ""}
                onChange={(name, val) => setValues((prev) => ({ ...prev, [name]: val }))}
              />
            ))}
          </div>
        ) : null}
      </div>

      <footer className="mt-4 flex flex-wrap items-center justify-between gap-2 pt-3 shadow-[inset_0_1px_0_var(--color-line)]">
        <div className="flex items-center gap-1.5">
          {card.draftState === "lazy" || card.draftState === "hydrating" ? (
            <Btn
              kind="secondary"
              size="sm"
              disabled={busy !== "" || card.draftState === "hydrating"}
              icon={
                busy === "hydrate" || card.draftState === "hydrating" ? (
                  <Spinner size={12} />
                ) : (
                  <Sparkles size={13} />
                )
              }
              onClick={() => void handleHydrate()}
            >
              {card.draftState === "hydrating" ? t("Preparing…") : t("Prepare form")}
            </Btn>
          ) : null}
          <Btn kind="ghost" size="sm" disabled={busy !== ""} onClick={() => void handleDismiss(false)}>
            {t("Dismiss")}
          </Btn>
        </div>

        <Btn
          kind="primary"
          size="sm"
          disabled={busy !== ""}
          icon={busy === "submit" ? <Spinner size={12} tone="on-ink" /> : <Send size={13} />}
          onClick={() => void handleSubmit()}
        >
          {card.submitLabel || t("Send")}
        </Btn>
      </footer>
    </article>
  );
}

// DashboardPane renders the Bot's dedicated Dashboard:
// 1. Pending Approvals requiring immediate human sign-off
// 2. 2D Bento-packed modular Dashboard cards (maintained via bot/DASHBOARD.md)
export function DashboardPane({
  bot,
  onError,
  onQuoted,
}: {
  bot: Bot;
  onError: (s: string) => void;
  onQuoted: (c: Chat) => void;
}) {
  const dashQ = useQuery(UI.method.listDashboard, { botId: bot.id });
  const approvalsQ = useQuery(UI.method.listApprovals, { botId: bot.id });
  const autoQ = useQuery(UI.method.listAutomations, { botId: bot.id });

  const cards = dashQ.data?.cards ?? null;
  const approvals = approvalsQ.data?.approvals ?? [];
  const dashboardAuto = (autoQ.data?.automations ?? []).find((a) => a.kind === "dashboard");

  const [freshCards, setFreshCards] = useState<Set<string>>(new Set());
  const [runningRefresh, setRunningRefresh] = useState(false);

  const gridRef = useRef<HTMLDivElement>(null);
  const cols = useBentoColumns(gridRef);

  const packedCards = useMemo(() => packDashboardCards(cards ?? [], cols), [cards, cols]);

  useEffect(() => {
    void recheck(UI.method.listDashboard, { botId: bot.id });
    void recheck(UI.method.listApprovals, { botId: bot.id });
  }, [bot.id, bot.feedUnread, bot.status]);

  useEffect(() => {
    const unreadCards = (dashQ.data?.cards ?? []).filter((c) => !c.read).map((c) => c.id);
    if (!unreadCards.length) return;
    setFreshCards((cur) => new Set([...cur, ...unreadCards]));
    ui.markDashboardRead({ botId: bot.id })
      .then(() => {
        patch(UI.method.listDashboard, { botId: bot.id }, (r) => ({
          ...r,
          cards: r.cards.map((c) => ({ ...c, read: true })),
        }));
        reloadBot(bot.id);
      })
      .catch(() => {});
  }, [dashQ.data, bot.id]);

  useEffect(() => {
    if (dashQ.error) onError(fail(dashQ.error));
  }, [dashQ.error]);

  useEffect(() => {
    setFreshCards(new Set());
  }, [bot.id]);

  async function decideApproval(ap: Approval, decision: "allow_once" | "always" | "deny") {
    onError("");
    try {
      await ui.decideApproval({ id: ap.id, decision });
      void recheck(UI.method.listApprovals, { botId: bot.id });
      reloadBot(bot.id);
    } catch (e) {
      onError(fail(e));
    }
  }

  async function autoApprove(ap: Approval) {
    onError("");
    try {
      await ui.setRule({ botId: bot.id, connector: ap.connector, action: ap.action, permission: "auto" });
      await ui.decideApproval({ id: ap.id, decision: "allow_once" });
      void recheck(UI.method.listApprovals, { botId: bot.id });
      reloadBot(bot.id);
    } catch (e) {
      onError(fail(e));
    }
  }

  async function submitCard(card: DashboardCard, values: Record<string, unknown>) {
    onError("");
    try {
      const r = await ui.submitDashboardCard({
        botId: bot.id,
        id: card.id,
        valuesJson: JSON.stringify(values),
      });
      patch(UI.method.listDashboard, { botId: bot.id }, (cur) => ({
        ...cur,
        cards: cur.cards.filter((c) => c.id !== card.id),
      }));
      if (r.chat) onQuoted(r.chat);
    } catch (e) {
      onError(fail(e));
    }
  }

  async function dismissCard(card: DashboardCard, suppressKind: boolean) {
    onError("");
    try {
      await ui.dismissDashboardCard({ botId: bot.id, id: card.id, suppressKind });
      patch(UI.method.listDashboard, { botId: bot.id }, (cur) => ({
        ...cur,
        cards: cur.cards.filter((c) => c.id !== card.id),
      }));
    } catch (e) {
      onError(fail(e));
    }
  }

  async function hydrateCard(card: DashboardCard) {
    onError("");
    try {
      const r = await ui.hydrateDashboardCard({ botId: bot.id, id: card.id });
      if (r.card) {
        patch(UI.method.listDashboard, { botId: bot.id }, (cur) => ({
          ...cur,
          cards: cur.cards.map((c) => (c.id === r.card!.id ? r.card! : c)),
        }));
      }
      if (r.chat) onQuoted(r.chat);
    } catch (e) {
      onError(fail(e));
    }
  }

  async function runDashboardAutomation() {
    if (!dashboardAuto) return;
    onError("");
    setRunningRefresh(true);
    try {
      await ui.runAutomation({ botId: bot.id, id: dashboardAuto.id });
      void recheck(UI.method.listAutomations, { botId: bot.id });
    } catch (e) {
      onError(fail(e));
    } finally {
      setRunningRefresh(false);
    }
  }

  return (
    <div ref={gridRef} className="silo-page max-w-[1120px] pb-12">
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 className="text-title">{t("Dashboard")}</h2>
          <p className="text-ink-2">
            {t("Ask the Bot in chat what to track on your dashboard, or run the Dashboard automation.")}
          </p>
        </div>
        {dashboardAuto ? (
          <div className="flex items-center gap-2">
            <Btn
              kind="secondary"
              size="sm"
              disabled={runningRefresh || dashboardAuto.running}
              icon={
                runningRefresh || dashboardAuto.running ? <Spinner size={12} /> : <Play size={13} />
              }
              onClick={() => void runDashboardAutomation()}
            >
              {dashboardAuto.running ? t("Running now") : t("Refresh dashboard")}
            </Btn>
            <Link
              to="/bots/$botId/automations/$automationId"
              params={{ botId: bot.id, automationId: dashboardAuto.id }}
            >
              <Btn kind="ghost" size="sm" icon={<Timer size={13} />}>
                {t("Schedule")}
              </Btn>
            </Link>
          </div>
        ) : null}
      </div>

      {approvals.length > 0 ? (
        <section className="mb-8 space-y-3">
          <div className="flex items-center gap-2">
            <h3 className="text-[12px] font-semibold tracking-wider text-ink-3 uppercase">
              {t("Needs your approval")}
            </h3>
            <span className="rounded-full bg-vermilion-pale px-2 py-0.5 font-mono text-[11px] font-medium text-vermilion">
              {approvals.length}
            </span>
          </div>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {approvals.map((ap) => (
              <ApprovalCard
                key={ap.id}
                bot={bot}
                approval={ap}
                onDecide={(decision) => void decideApproval(ap, decision)}
                onAutoApprove={() => void autoApprove(ap)}
              />
            ))}
          </div>
        </section>
      ) : null}

      <section>
        {cards === null ? (
          <p className="text-ink-3">{t("Loading…")}</p>
        ) : packedCards.length === 0 ? (
          <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
            <LayoutGrid size={20} className="text-ink-3" />
            <p className="text-ink-2">{t("No open dashboard cards.")}</p>
            <p className="max-w-md text-[12.5px] text-ink-3">
              {t("Ask the Bot in chat what to track on your dashboard, or run the Dashboard automation.")}
            </p>
          </div>
        ) : (
          <div
            className={`grid gap-3 ${
              cols === 3 ? "grid-cols-3" : cols === 2 ? "grid-cols-2" : "grid-cols-1"
            }`}
          >
            {packedCards.map((item) => (
              <DashboardCardItem
                key={item.card.id}
                botId={bot.id}
                item={item}
                fresh={freshCards.has(item.card.id)}
                onSubmit={submitCard}
                onDismiss={dismissCard}
                onHydrate={hydrateCard}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

import { BrainCircuit, Check, Minimize2, UnfoldVertical } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { setAutoExpand, useAutoExpand } from "../autoExpand";
import type { CollectMemoriesResponse } from "../gen/silo/v1/ui_pb";
import { Tip, TipTitle } from "../Tip";
import { useDismiss } from "../useMenu";
import type { Usage } from "../useRunStream";
import { collectSummary, useMemoryCollect } from "./memoryCollect";
import { contextFill, fmtTokens } from "./model";
import { t } from "../i18n.ts";

const WIDTH = 284;

// ConversationMenu is the composer's one control for the conversation itself:
// a ring for how full the model's context window was on the last turn, which
// opens a small panel with the numbers, Compact now, Save memories and the
// Follow live steps preference.
export function ConversationMenu({
  usage,
  onCompact,
  compactBlocked,
  chatId,
  onCollect,
}: {
  usage?: Usage | null;
  onCompact?: () => void;
  // compactBlocked: a reply is running, or there is no chat yet.
  compactBlocked: boolean;
  chatId?: string;
  onCollect?: () => Promise<CollectMemoriesResponse>;
}) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<{ left: number; top: number; up: boolean }>({ left: 0, top: 0, up: true });
  const collect = useMemoryCollect(chatId, onCollect);
  const autoExpand = useAutoExpand();

  const measured = !!usage && usage.window > 0;
  const fill = measured ? contextFill(usage) : null;
  const pct = fill?.pct ?? 0;
  const r = 6;
  const circ = 2 * Math.PI * r;

  useEffect(() => {
    if (!open) return;
    const rect = triggerRef.current?.getBoundingClientRect();
    if (rect) {
      // The composer sits at the bottom, so the panel opens upward when it fits,
      // its right edge on the trigger's.
      const up = rect.top > 320 || rect.top > window.innerHeight - rect.bottom;
      const left = Math.min(Math.max(8, rect.right - WIDTH), window.innerWidth - WIDTH - 8);
      setBox({ left, top: up ? rect.top - 8 : rect.bottom + 8, up });
    }
    requestAnimationFrame(() => items()[0]?.focus());
  }, [open]);
  useDismiss(open, triggerRef, menuRef, () => setOpen(false));

  function items(): HTMLElement[] {
    return Array.from(menuRef.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]:not([aria-disabled="true"])') ?? []);
  }

  function close() {
    setOpen(false);
    triggerRef.current?.focus();
  }

  function onMenuKey(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    }
    if (e.key === "Tab") {
      setOpen(false);
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Home" && e.key !== "End") return;
    e.preventDefault();
    const list = items();
    if (list.length === 0) return;
    const at = list.indexOf(document.activeElement as HTMLElement);
    const next =
      e.key === "Home" ? 0 : e.key === "End" ? list.length - 1 : e.key === "ArrowDown" ? (at + 1) % list.length : (at - 1 + list.length) % list.length;
    list[next].focus();
  }

  function onTriggerKey(e: KeyboardEvent<HTMLButtonElement>) {
    if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
      e.preventDefault();
      setOpen(true);
    }
  }

  const running = collect.state.kind === "running";
  const fresh = collect.state.kind === "done" && collect.state.fresh;
  const changed = collect.state.kind === "done" ? collect.state.res.saved + collect.state.res.updated : 0;
  const collectAside =
    collect.state.kind === "running"
      ? t("Reading…")
      : collect.state.kind === "done"
        ? collectSummary(collect.state.res)
        : collect.state.kind === "error"
          ? t("Failed")
          : undefined;
  const canCompact = measured && !!onCompact && !compactBlocked;

  const style: CSSProperties = { left: box.left, top: box.top, width: WIDTH, transform: box.up ? "translateY(-100%)" : undefined };

  return (
    <>
      <Tip content={<TipTitle aside={measured ? `${pct}%` : undefined}>{t("This conversation")}</TipTitle>}>
        <button
          ref={triggerRef}
          type="button"
          aria-haspopup="menu"
          aria-expanded={open}
          aria-label={t("This conversation, context {{percent}}% full", { percent: pct })}
          onClick={() => setOpen((v) => !v)}
          onKeyDown={onTriggerKey}
          className={`relative flex h-[34px] min-w-[34px] shrink-0 items-center justify-center gap-1.5 rounded-control px-2 font-mono text-[11px] transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.96] active:duration-[70ms] aria-expanded:bg-well ${
            fill ? fill.tone : "text-ink-3"
          }`}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden className="shrink-0 -rotate-90">
            <circle cx="8" cy="8" r={r} fill="none" stroke="currentColor" strokeOpacity="0.22" strokeWidth="2" />
            {fill ? (
              <circle
                cx="8"
                cy="8"
                r={r}
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeDasharray={circ}
                strokeDashoffset={circ * (1 - fill.frac)}
                className="transition-[stroke-dashoffset] duration-[400ms] ease-quiet"
              />
            ) : null}
          </svg>
          {measured ? <span className="hidden @min-[400px]:inline">{pct}%</span> : null}
          {running ? <span aria-hidden className="breathe absolute top-1.5 right-1.5 h-1.5 w-1.5 rounded-full bg-cobalt" /> : null}
          {fresh && !open ? (
            <span className="pop absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-cobalt px-1 font-sans text-[10px] font-semibold text-on-accent">
              {changed > 0 ? `+${changed}` : <Check size={10} strokeWidth={3} />}
            </span>
          ) : null}
        </button>
      </Tip>
      {open
        ? createPortal(
            <div
              ref={menuRef}
              role="menu"
              aria-label={t("This conversation")}
              onKeyDown={onMenuKey}
              className="fixed z-50 rounded-card bg-surface p-1.5 shadow-slip"
              style={style}
            >
              <div className="rise">
                <div className="px-2 pt-1.5 pb-2.5">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="text-[12.5px] font-medium text-ink">{t("Context window")}</span>
                    {measured ? <span className={`font-mono text-[11px] ${fill!.tone}`}>{pct}%</span> : null}
                  </div>
                  <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-well">
                    {fill ? <div className={`h-full rounded-full ${fill.barTone}`} style={{ width: `${Math.max(2, pct)}%` }} /> : null}
                  </div>
                  {measured ? (
                    <dl className="mt-2 grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5 text-[12px] text-ink-3">
                      <dt>{t("Used")}</dt>
                      <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(fill!.used)}</dd>
                      <dt>{t("Model limit")}</dt>
                      <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(usage!.window)}</dd>
                      {usage!.cacheRead > 0 ? (
                        <>
                          <dt>{t("From cache")}</dt>
                          <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(usage!.cacheRead)}</dd>
                        </>
                      ) : null}
                    </dl>
                  ) : null}
                  <p className="mt-2 text-[12px] leading-[17px] text-ink-3">
                    {t("Near the limit the conversation is summarized automatically; the thread keeps everything.")}
                  </p>
                </div>

                <div className="mx-1 h-px bg-line" />

                <div className="grid gap-0.5 pt-1">
                  {onCompact && measured ? (
                    <Item
                      icon={<Minimize2 size={15} />}
                      label={t("Compact now")}
                      hint={compactBlocked ? t("You can compact once the reply finishes") : undefined}
                      disabled={!canCompact}
                      onPick={() => {
                        setOpen(false);
                        onCompact();
                      }}
                    />
                  ) : null}
                  {collect.available ? (
                    <Item
                      icon={<BrainCircuit size={15} className={running ? "breathe text-cobalt" : undefined} />}
                      label={t("Save memories")}
                      aside={collectAside}
                      hint={
                        collect.state.kind === "error" ? (
                          <span className="text-vermilion">{collect.state.message}</span>
                        ) : running ? (
                          t("Reading this chat for facts worth keeping.")
                        ) : (
                          t("Keep what the Bot missed in this chat")
                        )
                      }
                      disabled={running}
                      busy={running}
                      onPick={() => void collect.run()}
                    />
                  ) : null}
                  {collect.state.kind === "done" && collect.state.res.memories.length > 0 ? (
                    <ul className="mx-2 mb-1 ml-[34px] grid gap-1 text-[12px] leading-[17px] text-ink-2">
                      {collect.state.res.memories.slice(0, 4).map((m) => (
                        <li key={m.id} className="line-clamp-2">
                          {m.content}
                        </li>
                      ))}
                    </ul>
                  ) : null}
                  <Item
                    icon={<UnfoldVertical size={15} />}
                    label={t("Follow live steps")}
                    hint={
                      autoExpand
                        ? t("Thinking and tool rows open while they run and fold shut when they finish.")
                        : t("Thinking and tool rows stay folded. Click a row to open it.")
                    }
                    checked={autoExpand}
                    onPick={() => setAutoExpand(!autoExpand)}
                  />
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

// Item is one row of the panel: a glyph, a label with an optional hint under it,
// and on the right either a short result or, for a preference, a small switch.
function Item({
  icon,
  label,
  hint,
  aside,
  checked,
  disabled,
  busy,
  onPick,
}: {
  icon: ReactNode;
  label: string;
  hint?: ReactNode;
  aside?: string;
  checked?: boolean;
  disabled?: boolean;
  busy?: boolean;
  onPick: () => void;
}) {
  const toggle = checked !== undefined;
  return (
    <button
      type="button"
      role={toggle ? "menuitemcheckbox" : "menuitem"}
      aria-checked={toggle ? checked : undefined}
      aria-disabled={disabled || undefined}
      aria-busy={busy || undefined}
      onClick={disabled ? undefined : onPick}
      className="grid w-full grid-cols-[18px_1fr_auto] items-start gap-x-2.5 rounded-sm px-2 py-1.5 text-left outline-none transition-colors duration-[160ms] ease-quiet hover:bg-well focus-visible:bg-well aria-disabled:cursor-default aria-disabled:hover:bg-transparent"
    >
      <span className={`mt-px flex h-[18px] items-center ${disabled && !busy ? "text-ink-3" : "text-ink-2"}`}>{icon}</span>
      <span className="min-w-0">
        <span className={`block text-[13px] leading-[18px] font-medium ${disabled && !busy ? "text-ink-3" : "text-ink"}`}>{label}</span>
        {hint ? <span className="mt-0.5 block text-[12px] leading-[17px] text-ink-3">{hint}</span> : null}
      </span>
      {toggle ? (
        <span
          aria-hidden
          className={`relative mt-px inline-block h-[18px] w-[30px] rounded-full transition-colors duration-[260ms] ease-quiet ${
            checked ? "bg-ink" : "bg-pressed shadow-[inset_0_0_0_1px_var(--color-line)]"
          }`}
        >
          <span
            className={`absolute top-[2px] h-[14px] w-[14px] rounded-full shadow-[0_1px_2px_rgb(20_18_14/0.18)] transition-[left] duration-[260ms] ease-quiet ${
              checked ? "left-[14px] bg-on-ink" : "left-[2px] bg-knob"
            }`}
          />
        </span>
      ) : aside ? (
        <span className="mt-px max-w-[110px] truncate text-right text-[12px] leading-[18px] text-ink-3">{aside}</span>
      ) : null}
    </button>
  );
}

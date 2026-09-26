import { createContext, useContext, useRef, useState, type ReactNode } from "react";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
import { Lamp, StatusWord } from "./Lamp";
import type { Approval, Bot } from "./gen/silo/v1/ui_pb";

function phrase(s: string) {
  return s
    .replaceAll(/[._-]+/g, " ")
    .trim()
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

export function describeApproval(ap: Approval) {
  if (ap.title) {
    return { title: ap.title, summary: ap.summary, fields: ap.fields };
  }
  const title = `${phrase(ap.action)} · ${phrase(ap.connector)}`;
  let args: Record<string, string> = {};
  try {
    const v = JSON.parse(ap.argsJson || "{}") as Record<string, unknown>;
    for (const [k, val] of Object.entries(v)) {
      if (val == null || val === "") continue;
      args[k] = typeof val === "string" ? val : JSON.stringify(val);
    }
  } catch {
    /* ignore */
  }
  const fields = Object.entries(args).map(([k, value]) => ({ label: phrase(k), value }));
  return { title, summary: `This Bot wants to ${phrase(ap.action).toLowerCase()} (${phrase(ap.connector)}).`, fields };
}

// Keeps a slip mounted through its exit: when `show` drops, the last children
// play slip-out (and the wash fades) before unmounting.
export function SlipPresence({ show, children }: { show: boolean; children: ReactNode }) {
  const last = useRef<ReactNode>(null);
  const [leaving, setLeaving] = useState(false);
  const [mounted, setMounted] = useState(show);
  if (show) last.current = children;
  if (show && (!mounted || leaving)) {
    setMounted(true);
    setLeaving(false);
  }
  if (!show && mounted && !leaving) setLeaving(true);
  if (!mounted) return null;
  return (
    <LeavingCtx.Provider value={leaving ? () => setMounted(false) : null}>{show ? children : last.current}</LeavingCtx.Provider>
  );
}

const LeavingCtx = createContext<(() => void) | null>(null);

export function NeedYouSlip({
  bot,
  title,
  summary,
  fields,
  note,
  children,
}: {
  bot: Bot;
  title: string;
  summary: string;
  fields?: { label: string; value: string }[];
  note?: string;
  children: ReactNode;
}) {
  const done = useContext(LeavingCtx);
  const leaving = !!done;
  return (
    <>
      {/* A canvas wash over the thread, never a dark dim. */}
      <div
        aria-hidden
        className={`pointer-events-none absolute inset-0 z-[9] bg-canvas/55 max-wide:fixed wide:left-[var(--wash-left,0px)] ${leaving ? "wash-out" : "wash-in"}`}
      />
      <aside
        role="dialog"
        aria-label={title}
        inert={leaving}
        onAnimationEnd={(e) => {
          if (leaving && e.target === e.currentTarget) done?.();
        }}
        className={`${leaving ? "slip-out" : "slip-in"} z-20 flex flex-col overflow-hidden bg-surface shadow-slip max-wide:fixed max-wide:inset-x-0 max-wide:bottom-0 max-wide:max-h-[85dvh] max-wide:rounded-t-panel max-wide:pb-[env(safe-area-inset-bottom)] wide:absolute wide:top-3 wide:right-3 wide:bottom-3 wide:w-[400px] wide:rounded-panel`}
      >
        <div className="min-h-0 flex-1 overflow-auto px-6 pt-5 pb-4">
          <div className="mx-auto mb-4 h-1 w-10 rounded-full bg-pressed wide:hidden" />
          <div className="mb-5 flex items-center gap-2.5">
            <Crest index={bot.crest} size={28} />
            <span className="min-w-0 truncate text-[15px] leading-5 font-semibold tracking-[-0.01em]">{bot.name}</span>
            <span className="ml-auto flex shrink-0 items-center gap-2">
              <Lamp status="needs_you" />
              <StatusWord status="needs_you" />
            </span>
          </div>
          <h2 className="mb-2 text-[22px] leading-7 font-medium tracking-[-0.015em]">{title}</h2>
          <p className="mb-5 text-[14px] leading-[22px] text-ink-2">{summary}</p>
          {fields && fields.length > 0 && (
            <dl className="overflow-hidden rounded-control bg-well">
              {fields.map((f) => (
                <div key={f.label} className="flex gap-4 px-3.5 py-2.5 shadow-[inset_0_-1px_0_var(--color-line-strong)] last:shadow-none">
                  <dt className="w-24 shrink-0 text-[12px] leading-5 font-medium text-ink-3">{f.label}</dt>
                  <dd className="min-w-0 flex-1 whitespace-pre-wrap break-words font-mono text-[12.5px] leading-5 text-ink">{f.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
        <div className="shrink-0 px-6 pt-2 pb-5">
          <div className="flex flex-col gap-2">{children}</div>
          {note ? (
            <p className="mt-4 flex items-center gap-2 text-[12.5px] leading-[18px] text-ink-3">
              <span className="breathe h-[7px] w-[7px] shrink-0 rounded-full bg-vermilion" />
              {note}
            </p>
          ) : null}
        </div>
      </aside>
    </>
  );
}

export function ApprovalSlip({
  bot,
  approval,
  onDecide,
  onAutoApprove,
}: {
  bot: Bot;
  approval: Approval;
  onDecide: (decision: "allow_once" | "always" | "deny") => void;
  onAutoApprove?: () => void;
}) {
  const d = describeApproval(approval);
  const waiting = approval.runId ? "This run is paused until you choose." : "Waiting for your choice.";
  return (
    <NeedYouSlip bot={bot} title={d.title} summary={d.summary} fields={d.fields} note={waiting}>
      <Btn kind="primary" className="!h-10 w-full justify-center" onClick={() => onDecide("allow_once")}>
        Allow once
      </Btn>
      <Btn kind="secondary" className="!h-10 w-full justify-center" onClick={() => onDecide("always")}>
        Always allow this action
      </Btn>
      {onAutoApprove && (
        <Btn
          kind="secondary"
          className="!h-10 w-full justify-center"
          title="Set this action to Auto and let the approval model decide from now on. This run is allowed once."
          onClick={onAutoApprove}
        >
          Auto-approve this action
        </Btn>
      )}
      <Btn kind="deny" className="!h-10 w-full justify-center" onClick={() => onDecide("deny")}>
        Deny
      </Btn>
    </NeedYouSlip>
  );
}

export function ConnectorAuthSlip({
  bot,
  name,
  onAuthorize,
  onLater,
}: {
  bot: Bot;
  name: string;
  onAuthorize: () => void;
  onLater: () => void;
}) {
  return (
    <NeedYouSlip
      bot={bot}
      title={`Authorize ${name}`}
      summary="This connector uses OAuth. A browser window will ask you to allow Silo to call it."
    >
      <Btn kind="primary" className="!h-10 w-full justify-center" onClick={onAuthorize}>
        Authorize
      </Btn>
      <Btn kind="secondary" className="!h-10 w-full justify-center" onClick={onLater}>
        Later
      </Btn>
    </NeedYouSlip>
  );
}

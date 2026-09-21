import type { ReactNode } from "react";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
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
  return (
    <aside className="silo-slip z-20 flex flex-col overflow-auto bg-folio p-5 max-wide:fixed max-wide:inset-x-0 max-wide:bottom-0 max-wide:max-h-[80dvh] max-wide:rounded-t-[10px] max-wide:border-t max-wide:border-thread max-wide:pb-[max(1.25rem,env(safe-area-inset-bottom))] wide:absolute wide:top-0 wide:right-0 wide:h-full wide:w-[400px] wide:border-l wide:border-thread">
      <div className="mx-auto mb-3 h-1 w-10 rounded-full bg-thread wide:hidden" />
      <div className="mb-5 flex items-center gap-2.5">
        <Crest index={bot.crest} size={28} />
        <span className="font-medium">{bot.name}</span>
        <span className="ml-0.5 flex items-center gap-1.5 text-[12px] font-medium text-carmine">
          <span className="h-[7px] w-[7px] rounded-full bg-carmine" />
          Needs you
        </span>
      </div>
      <h2 className="mb-2 text-[22px] font-medium">{title}</h2>
      <p className="mb-4 text-[13px] leading-6 text-stone">{summary}</p>
      {fields && fields.length > 0 && (
        <dl className="mb-4 space-y-3 rounded-[10px] border border-thread-2 bg-cloth p-3">
          {fields.map((f) => (
            <div key={f.label}>
              <dt className="text-[10px] font-medium tracking-[0.12em] text-stone uppercase">{f.label}</dt>
              <dd className="mt-0.5 whitespace-pre-wrap break-words font-mono text-[13px] text-iron">{f.value}</dd>
            </div>
          ))}
        </dl>
      )}
      {note && <p className="mb-4 text-[13px] text-stone">{note}</p>}
      <div className="mt-auto flex flex-col pt-2">{children}</div>
    </aside>
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
      <Btn kind="primary" className="mb-2 w-full justify-center" onClick={() => onDecide("allow_once")}>
        Allow once
      </Btn>
      <Btn kind="secondary" className="mb-2 w-full justify-center" onClick={() => onDecide("always")}>
        Always allow this action
      </Btn>
      {onAutoApprove && (
        <Btn
          kind="secondary"
          className="mb-2 w-full justify-center"
          title="Set this action to Auto and let the approval model decide from now on. This run is allowed once."
          onClick={onAutoApprove}
        >
          Auto-approve this action
        </Btn>
      )}
      <Btn kind="deny" className="w-full justify-center" onClick={() => onDecide("deny")}>
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
      <Btn kind="primary" className="mb-2 w-full justify-center" onClick={onAuthorize}>
        Authorize
      </Btn>
      <Btn kind="secondary" className="w-full justify-center" onClick={onLater}>
        Later
      </Btn>
    </NeedYouSlip>
  );
}

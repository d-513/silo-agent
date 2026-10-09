import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { ErrorWell, Panel, SkeletonRows } from "../Field";
import { ago } from "../format";
import { UI, type ListSessionsResponse, type Session } from "../gen/silo/v1/ui_pb";
import { put } from "../query";
import { deviceName, methodName } from "./device";

// SessionsPanel lists where the account is signed in, and signs any of them
// out. The session in use is first and is ended with Sign out, not here.
export function SessionsPanel() {
  const q = useQuery(UI.method.listSessions, {});
  const [actErr, setErr] = useState("");
  const rows = q.data?.sessions;
  const err = actErr || (q.error ? fail(q.error) : "");
  const others = rows?.filter((s) => !s.current).length ?? 0;

  // Both calls answer with the list as it now is.
  async function act(call: () => Promise<ListSessionsResponse>) {
    setErr("");
    try {
      put(UI.method.listSessions, {}, await call());
    } catch (e) {
      setErr(fail(e));
    }
  }
  return (
    <Panel
      title="Sessions"
      note="Everywhere this account is signed in."
      padded={false}
      action={
        others > 0 ? (
          <ArmedButton size="sm" armedLabel="Click again to sign them out" onConfirm={() => void act(() => ui.revokeOtherSessions({}))}>
            Sign out everywhere else
          </ArmedButton>
        ) : null
      }
    >
      {err ? <ErrorWell className="mx-5 mt-4">{err}</ErrorWell> : null}
      {!rows ? (
        q.error ? null : (
          <SkeletonRows rows={2} height={48} className="p-5" />
        )
      ) : (
        <ul className="divide-y divide-line-strong">
          {rows.map((s) => (
            <li key={s.id} className="flex items-center gap-4 px-5 py-3.5">
              <div className="min-w-0 flex-1">
                <div className="text-[14px] font-medium text-ink">{deviceName(s.userAgent)}</div>
                <p className="mt-0.5 flex flex-wrap gap-x-1.5 text-[12.5px] text-ink-3">{details(s)}</p>
              </div>
              {s.current ? (
                <span className="shrink-0 text-[12.5px] text-ink-2">This device</span>
              ) : (
                <ArmedButton kind="ghost" size="sm" armedLabel="Click again to sign out" onConfirm={() => void act(() => ui.revokeSession({ id: s.id }))}>
                  Sign out
                </ArmedButton>
              )}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function details(s: Session) {
  const parts = [methodName(s.method)];
  if (s.ip) parts.push(s.ip);
  parts.push(s.current ? "Active now" : s.lastSeenAt ? `Last seen ${ago(s.lastSeenAt)}` : "Not seen yet");
  return parts.map((p, i) => (
    <span key={i} className="flex gap-x-1.5">
      {i > 0 ? <span aria-hidden>·</span> : null}
      <span>{p}</span>
    </span>
  ));
}

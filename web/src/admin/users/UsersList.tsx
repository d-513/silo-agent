import { ChevronRight, Plus } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { ui } from "../../api";
import { useAuth } from "../../auth";
import { btnClass } from "../../Btn";
import { fail } from "../../errors";
import { ArmedButton } from "../../Feedback";
import { ErrorWell, Panel, SkeletonRows } from "../../Field";
import { ago, day } from "../../format";
import { type Invite, type User } from "../../gen/silo/v1/ui_pb";
import { dropInvite, signInWays, useUsers } from "./useUsers";

// UsersList is everyone with an account, and the invite links not yet used.
export function UsersList() {
  const { loaded, users, invites, err } = useUsers();
  const { email } = useAuth();
  const [actErr, setErr] = useState("");

  async function revoke(i: Invite) {
    setErr("");
    try {
      await ui.deleteInvite({ id: i.id });
      dropInvite(i.id);
    } catch (e) {
      setErr(fail(e));
    }
  }
  return (
    <div className="grid gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-xl text-[13.5px] leading-[21px] text-ink-2">People get an account from an invite link, which they open to choose their own password.</p>
        <Link to="/admin/users/invite" className={btnClass("primary", "pr-[7px]")}>
          Invite someone
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-xs bg-on-ink/20 text-on-ink">
            <Plus size={13} />
          </span>
        </Link>
      </div>
      {err || actErr ? <ErrorWell>{actErr || err}</ErrorWell> : null}
      {!loaded ? (
        err ? null : (
          <SkeletonRows rows={3} height={64} />
        )
      ) : (
        <>
          <Panel padded={false}>
            <ul className="divide-y divide-line-strong">
              {users.map((u) => (
                <li key={u.id}>
                  <Link
                    to="/admin/users/$userId"
                    params={{ userId: u.id }}
                    className="flex items-center gap-4 px-5 py-3.5 transition-colors duration-[160ms] ease-quiet hover:bg-well"
                  >
                    <div className="min-w-0 flex-1">
                      <div className={`truncate text-[14px] font-medium ${u.disabled ? "text-ink-3" : "text-ink"}`}>
                        {u.email}
                        {u.email === email ? <span className="ml-2 font-normal text-ink-3">you</span> : null}
                      </div>
                      <p className="mt-0.5 truncate text-[12.5px] text-ink-3">{summary(u)}</p>
                    </div>
                    {u.disabled ? <span className="shrink-0 text-[12.5px] text-ink-2">Disabled</span> : null}
                    <ChevronRight size={16} className="shrink-0 text-ink-3" />
                  </Link>
                </li>
              ))}
            </ul>
          </Panel>
          {invites.length > 0 ? (
            <Panel title="Links not used yet" note="A link works once. Its address cannot be shown again: make a new one to replace it." padded={false}>
              <ul className="divide-y divide-line-strong">
                {invites.map((i) => (
                  <li key={i.id} className="flex items-center gap-4 px-5 py-3.5">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[14px] font-medium text-ink">{i.email}</div>
                      <p className="mt-0.5 text-[12.5px] text-ink-3">
                        {i.userId ? "Password reset" : i.admin ? "Invite as admin" : "Invite"} · expires {day(i.expiresAt)}
                      </p>
                    </div>
                    <ArmedButton kind="ghost" size="sm" armedLabel="Click again to revoke" onConfirm={() => void revoke(i)}>
                      Revoke
                    </ArmedButton>
                  </li>
                ))}
              </ul>
            </Panel>
          ) : null}
        </>
      )}
    </div>
  );
}

function summary(u: User): string {
  const parts = [u.admin ? "Admin" : "User", signInWays(u), u.bots === 1 ? "1 Bot" : `${u.bots} Bots`];
  parts.push(u.lastSignInAt ? `signed in ${ago(u.lastSignInAt)}` : "never signed in");
  return parts.join(" · ");
}

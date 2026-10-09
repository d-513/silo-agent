import { useQuery } from "@connectrpc/connect-query";
import { ErrorWell, Panel, SkeletonRows } from "../Field";
import { fail } from "../errors";
import { UI, type User } from "../gen/silo/v1/ui_pb";
import { patch } from "../query";
import { PasswordPanel } from "./PasswordPanel";
import { SessionsPanel } from "./SessionsPanel";
import { TwoFactorPanel } from "./TwoFactorPanel";

// AccountPage is the signed-in person's own sign-in: how they get in, their
// password, their second factor, and where they are signed in.
export function AccountPage() {
  const me = useQuery(UI.method.me, {});
  const user = me.data?.user;
  // Every panel answers with the account as it now is.
  const setUser = (u: User) => patch(UI.method.me, {}, (prev) => ({ ...prev, user: u }));
  return (
    <div className="silo-page pb-12">
      <h1 className="mb-2 text-title">Account</h1>
      <p className="mb-6 text-ink-2">How you sign in to Silo.</p>
      {me.error ? <ErrorWell className="mb-5">{fail(me.error)}</ErrorWell> : null}
      {!user ? (
        me.error ? null : (
          <SkeletonRows rows={3} height={120} />
        )
      ) : (
        <div className="grid gap-5">
          <Panel title="Sign-in">
            <div className="mb-1 text-[12px] font-medium text-ink-3">Email</div>
            <div className="rounded-sm bg-well px-3 py-2 text-ink">{user.email}</div>
            <p className="mt-3 text-[12.5px] leading-[18px] text-ink-2">{methods(user)}</p>
          </Panel>
          <PasswordPanel user={user} onChange={setUser} />
          {user.hasPassword ? <TwoFactorPanel user={user} onChange={setUser} /> : null}
          <SessionsPanel />
        </div>
      )}
    </div>
  );
}

function methods(u: User): string {
  if (u.hasPassword && u.oidc) return "You can sign in with your password or with single sign-on.";
  if (u.oidc) return "You sign in with single sign-on. Set a password below to be able to sign in without it.";
  return "You sign in with your password.";
}

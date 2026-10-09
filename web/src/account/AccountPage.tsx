import { useQuery } from "@connectrpc/connect-query";
import { ErrorWell, SkeletonRows } from "../Field";
import { fail } from "../errors";
import { UI, type User } from "../gen/silo/v1/ui_pb";
import { patch } from "../query";
import { EmailPanel } from "./EmailPanel";
import { PasswordPanel } from "./PasswordPanel";
import { SessionsPanel } from "./SessionsPanel";
import { TwoFactorPanel } from "./TwoFactorPanel";

// AccountPage is the signed-in person's own sign-in. Two columns on a wide
// window: who they are and their password on the left, the second factor and
// where they are signed in on the right. One column below 960.
export function AccountPage() {
  const me = useQuery(UI.method.me, {});
  const user = me.data?.user;
  // Every panel answers with the account as it now is.
  const setUser = (u: User) => patch(UI.method.me, {}, (prev) => ({ ...prev, user: u }));
  return (
    <div className="silo-page silo-page-lg pb-12">
      <h1 className="mb-2 text-title">Account</h1>
      <p className="mb-6 text-ink-2">How you sign in to Silo.</p>
      {me.error ? <ErrorWell className="mb-5">{fail(me.error)}</ErrorWell> : null}
      {!user ? (
        me.error ? null : (
          <div className="grid gap-5 wide:grid-cols-2">
            <SkeletonRows rows={2} height={180} />
            <SkeletonRows rows={2} height={180} />
          </div>
        )
      ) : (
        <div className="grid items-start gap-5 wide:grid-cols-2">
          <div className="grid min-w-0 gap-5">
            <EmailPanel user={user} onChange={setUser} />
            <PasswordPanel user={user} onChange={setUser} />
          </div>
          <div className="grid min-w-0 gap-5">
            {user.hasPassword ? <TwoFactorPanel user={user} onChange={setUser} /> : null}
            <SessionsPanel />
          </div>
        </div>
      )}
    </div>
  );
}

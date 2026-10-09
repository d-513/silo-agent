import { useQuery } from "@connectrpc/connect-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { ui } from "./api";
import { setSession } from "./auth";
import { Btn, btnClass } from "./Btn";
import { fail } from "./errors";
import { inputClass } from "./Field";
import { UI } from "./gen/silo/v1/ui_pb";
import { queryClient } from "./query";
import { passwordProblem } from "./signinFlow";
import { AuthCard, AuthError, authLabel } from "./SignIn";

// InvitePage is where an admin's link lands: a new account picks its password
// and is signed in; an existing one picks a new password and signs in with it.
export function InvitePage() {
  const { token } = useParams({ from: "/invite/$token" });
  const navigate = useNavigate();
  const invite = useQuery(UI.method.getInvite, { token }, { retry: false, refetchOnWindowFocus: false });
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  if (done) {
    return (
      <AuthCard title="Password changed">
        <p className="mb-6 text-[13.5px] leading-[21px] text-ink-2">Sign in with your new password. Anywhere you were signed in before has been signed out.</p>
        <Link to="/signin" className={btnClass("primary", "w-full justify-center")}>
          Sign in
        </Link>
      </AuthCard>
    );
  }
  if (invite.error) {
    return (
      <AuthCard title="This link has expired">
        <p className="mb-6 text-[13.5px] leading-[21px] text-ink-2">{fail(invite.error)}</p>
        <Link to="/signin" className={btnClass("secondary", "w-full justify-center")}>
          Go to sign in
        </Link>
      </AuthCard>
    );
  }
  if (!invite.data) return <AuthCard title="One moment">{null}</AuthCard>;

  const reset = invite.data.passwordReset;
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const problem = passwordProblem(password, again);
    if (problem) {
      setErr(problem);
      return;
    }
    setErr("");
    setBusy(true);
    try {
      const r = await ui.acceptInvite({ token, password });
      if (!r.signedIn) {
        setDone(true);
        return;
      }
      // Whoever was signed in on this browser before is not any more.
      queryClient.clear();
      setSession({ email: r.user?.email ?? "", admin: r.user?.admin ?? false });
      void navigate({ to: "/" });
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  return (
    <AuthCard title={reset ? "Choose a new password" : "Welcome to Silo"}>
      <form onSubmit={onSubmit}>
        <p className="mb-4 text-[13.5px] leading-[21px] text-ink-2">
          {reset ? "For " : "You were invited as "}
          <span className="font-medium text-ink">{invite.data.email}</span>
          {reset ? "." : ". Choose a password to finish your account."}
        </p>
        {/* Lets a password manager file the new password under the right name. */}
        <input className="hidden" type="email" autoComplete="username" value={invite.data.email} readOnly />
        <label htmlFor="silo-new-pass" className={authLabel}>
          Password
        </label>
        <input
          id="silo-new-pass"
          type="password"
          className={`${inputClass} mb-4`}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          autoFocus
          required
        />
        <label htmlFor="silo-new-pass-2" className={authLabel}>
          Password again
        </label>
        <input id="silo-new-pass-2" type="password" className={`${inputClass} mb-6`} value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" required />
        {err && <AuthError>{err}</AuthError>}
        <Btn kind="primary" className="w-full justify-center" type="submit" disabled={busy}>
          {busy ? "Saving…" : reset ? "Set password" : "Create account"}
        </Btn>
      </form>
    </AuthCard>
  );
}

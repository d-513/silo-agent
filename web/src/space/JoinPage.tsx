import { useQuery } from "@connectrpc/connect-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { ui } from "../api";
import { setSession, useSession } from "../auth";
import { Btn, btnClass } from "../Btn";
import { fail } from "../errors";
import { inputClass } from "../Field";
import { UI } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { queryClient, reload } from "../query";
import { passwordProblem } from "../signinFlow";
import { AuthCard, AuthError, authLabel } from "../SignIn";
import { Trans } from "../Trans.tsx";
import { roleWord, SpaceMark } from "./SpaceTile";

// JoinPage is where a Workspace invite link lands. Signed in, it joins; signed
// out, it makes an account when the operator allows that, else asks to sign
// in first and comes back.
export function JoinPage() {
  const { token } = useParams({ from: "/join/$token" });
  const nav = useNavigate();
  const session = useSession();
  const info = useQuery(UI.method.getSpaceInvite, { token }, { retry: false, refetchOnWindowFocus: false });
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  if (info.error) {
    return (
      <AuthCard title={t("Something went wrong")}>
        <p className="mb-6 text-[13.5px] leading-[21px] text-ink-2">{fail(info.error)}</p>
      </AuthCard>
    );
  }
  const inv = info.data;
  if (!inv) return <AuthCard title={t("One moment")}>{null}</AuthCard>;
  if (inv.state !== "ok") {
    return (
      <AuthCard title={inv.state === "used" ? t("This link has been used") : t("This link has expired")}>
        <p className="mb-6 text-[13.5px] leading-[21px] text-ink-2">{t("Invite links work once, for 7 days. Ask whoever sent it for a new one.")}</p>
        <Link to="/" className={btnClass("secondary", "w-full justify-center")}>
          {session ? t("Go to Silo") : t("Go to sign in")}
        </Link>
      </AuthCard>
    );
  }

  async function join(e?: FormEvent) {
    e?.preventDefault();
    if (!session) {
      const problem = passwordProblem(password, again);
      if (problem) {
        setErr(problem);
        return;
      }
    }
    setErr("");
    setBusy(true);
    try {
      const r = await ui.joinSpace(session ? { token } : { token, email, password });
      if (!session) {
        queryClient.clear();
        setSession({ email: r.email, admin: r.admin });
      } else {
        await reload(UI.method.listSpaces);
      }
      void nav({ to: "/w/$spaceId", params: { spaceId: r.space?.id ?? "" } });
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }

  const head = (
    <div className="mb-5 flex items-center gap-3">
      <SpaceMark space={{ name: inv.spaceName, mark: inv.spaceMark }} size={40} />
      <p className="text-[13.5px] leading-[21px] text-ink-2">
        <Trans
          i18nKey="<b>{{who}}</b> invited you to <b>{{space}}</b> as {{role}}."
          values={{ who: inv.invitedBy, space: inv.spaceName, role: roleWord(inv.role) }}
          components={{ b: <span className="font-medium text-ink" /> }}
        />
      </p>
    </div>
  );

  if (session) {
    return (
      <AuthCard title={t("Join {{space}}", { space: inv.spaceName })}>
        {head}
        {err && <AuthError>{err}</AuthError>}
        <div className="flex gap-2">
          <Btn kind="primary" className="flex-1 justify-center" onClick={() => void join()} disabled={busy}>
            {busy ? t("Joining…") : t("Join")}
          </Btn>
          <Link to="/" className={btnClass("secondary", "flex-1 justify-center")}>
            {t("Not now")}
          </Link>
        </div>
      </AuthCard>
    );
  }
  if (!inv.signup) {
    return (
      <AuthCard title={t("Join {{space}}", { space: inv.spaceName })}>
        {head}
        <p className="mb-6 text-[13.5px] leading-[21px] text-ink-2">{t("Sign in to join. No account yet? Ask the instance admin for one, then open this link again.")}</p>
        <Link to="/signin" search={{ from: `/join/${token}` }} className={btnClass("primary", "w-full justify-center")}>
          {t("Sign in")}
        </Link>
      </AuthCard>
    );
  }
  return (
    <AuthCard title={t("Join {{space}}", { space: inv.spaceName })}>
      <form onSubmit={(e) => void join(e)}>
        {head}
        <label htmlFor="join-email" className={authLabel}>
          {t("Email")}
        </label>
        <input id="join-email" type="email" className={`${inputClass} mb-4`} value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="username" autoFocus required />
        <label htmlFor="join-pass" className={authLabel}>
          {t("Password")}
        </label>
        <input id="join-pass" type="password" className={`${inputClass} mb-4`} value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required />
        <label htmlFor="join-pass-2" className={authLabel}>
          {t("Password again")}
        </label>
        <input id="join-pass-2" type="password" className={`${inputClass} mb-6`} value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" required />
        {err && <AuthError>{err}</AuthError>}
        <Btn kind="primary" className="w-full justify-center" type="submit" disabled={busy}>
          {busy ? t("Creating…") : t("Create account and join")}
        </Btn>
        <p className="mt-4 text-center text-[12.5px] text-ink-3">
          <Link to="/signin" search={{ from: `/join/${token}` }} className="text-cobalt hover:text-cobalt-deep">
            {t("I have an account")}
          </Link>
        </p>
      </form>
    </AuthCard>
  );
}

import { useQuery } from "@connectrpc/connect-query";
import { useSearch } from "@tanstack/react-router";
import { useState, type FormEvent, type ReactNode } from "react";
import { ui } from "./api";
import { useAuth } from "./auth";
import { Btn } from "./Btn";
import { fail } from "./errors";
import { inputClass } from "./Field";
import { UI } from "./gen/silo/v1/ui_pb";
import { oidcStart, signInError } from "./signinFlow";
import { tunnelNext } from "./signinNext";
import { SiloGlyph } from "./SiloMark";

// The card every signed-out page sits in: Sign in, and an invite link's page.
export function AuthCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex min-h-dvh items-start justify-center bg-canvas px-4 pt-[18vh]">
      <div className="rise w-full max-w-[400px] rounded-card bg-surface p-8 shadow-card">
        <div className="mb-7 flex items-center gap-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-[8px] bg-well text-ink">
            <SiloGlyph className="h-5 w-5" />
          </span>
          <span className="text-[14px] font-medium">Silo Agent</span>
        </div>
        <h1 className="mb-6 text-title">{title}</h1>
        {children}
      </div>
    </div>
  );
}

export function AuthError({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="mb-3 flex items-start gap-2 text-[13px] text-vermilion">
      <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
      <span>{children}</span>
    </p>
  );
}

export const authLabel = "mb-1.5 block text-[12px] font-medium text-ink-3";

export function SignIn() {
  const { setSession } = useAuth();
  const search = useSearch({ from: "/signin" });
  // What the server offers. Until it answers, the password form shows: it is
  // what nearly every server has, and the form must not jump in a beat later.
  const options = useQuery(UI.method.authOptions, {}).data;
  const withPassword = options?.password ?? true;
  const [email, setEm] = useState("");
  const [password, setPassword] = useState("");
  // Set once the password was right and the account wants its second factor.
  const [needsCode, setNeedsCode] = useState(false);
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      const r = await ui.signIn({ email, password, code: needsCode ? code : "" });
      if (r.needsCode) {
        setNeedsCode(true);
        setBusy(false);
        return;
      }
      // A private tunnel sent us here: carry on to its handoff, which is a
      // server route, so leave the SPA (the session cookie is already set).
      const handoff = tunnelNext(window.location.search, window.location.origin);
      if (handoff) {
        window.location.assign(handoff);
        return;
      }
      // The /signin route sees the session and goes back to where it ended
      // (router.tsx), not the Bots list.
      setSession({ email: r.user?.email ?? email, admin: r.user?.admin ?? false });
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  const shown = err || signInError(search.error);
  return (
    <AuthCard title={needsCode ? "Two-factor code" : "Sign in"}>
      {needsCode ? (
        <form onSubmit={onSubmit}>
          <p className="mb-4 text-[13.5px] leading-[21px] text-ink-2">Enter the 6-digit code from your authenticator app, or one of your recovery codes.</p>
          <label htmlFor="silo-code" className={authLabel}>
            Code
          </label>
          <input
            id="silo-code"
            className={`${inputClass} mb-6 font-mono tracking-[0.12em]`}
            value={code}
            onChange={(e) => setCode(e.target.value)}
            autoComplete="one-time-code"
            autoCapitalize="none"
            spellCheck={false}
            autoFocus
            required
          />
          {shown && <AuthError>{shown}</AuthError>}
          <Btn kind="primary" className="w-full justify-center" type="submit" disabled={busy}>
            {busy ? "Signing in…" : "Sign in"}
          </Btn>
          <Btn
            kind="ghost"
            className="mt-2 w-full justify-center"
            type="button"
            onClick={() => {
              setNeedsCode(false);
              setCode("");
              setPassword("");
              setErr("");
            }}
          >
            Use another account
          </Btn>
        </form>
      ) : (
        <>
          {withPassword ? (
            <form onSubmit={onSubmit}>
              <label htmlFor="silo-email" className={authLabel}>
                Email
              </label>
              <input id="silo-email" className={`${inputClass} mb-4`} value={email} onChange={(e) => setEm(e.target.value)} autoComplete="username" autoFocus required />
              <label htmlFor="silo-pass" className={authLabel}>
                Password
              </label>
              <input
                id="silo-pass"
                type="password"
                className={`${inputClass} mb-6`}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
              {shown && <AuthError>{shown}</AuthError>}
              <Btn kind="primary" className="w-full justify-center" type="submit" disabled={busy}>
                {busy ? "Signing in…" : "Sign in"}
              </Btn>
            </form>
          ) : (
            shown && <AuthError>{shown}</AuthError>
          )}
          {options?.oidc ? (
            <>
              {withPassword ? (
                <div className="my-5 flex items-center gap-3 text-[12px] text-ink-3" aria-hidden>
                  <span className="h-px flex-1 bg-line-strong" />
                  or
                  <span className="h-px flex-1 bg-line-strong" />
                </div>
              ) : null}
              {/* A server route: the browser leaves the app for the provider. */}
              <Btn
                kind={withPassword ? "secondary" : "primary"}
                className="w-full justify-center"
                type="button"
                onClick={() => window.location.assign(oidcStart(window.location.search, window.location.origin))}
              >
                {options.oidcLabel || "Single sign-on"}
              </Btn>
            </>
          ) : null}
        </>
      )}
    </AuthCard>
  );
}

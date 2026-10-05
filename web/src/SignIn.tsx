import { useState, type FormEvent } from "react";
import { ui } from "./api";
import { useAuth } from "./auth";
import { Btn } from "./Btn";
import { fail } from "./errors";
import { inputClass } from "./Field";
import { tunnelNext } from "./signinNext";
import { SiloGlyph } from "./SiloMark";

export function SignIn() {
  const { setSession } = useAuth();
  const [email, setEm] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      const r = await ui.signIn({ email, password });
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
  return (
    <div className="flex min-h-dvh items-start justify-center bg-canvas px-4 pt-[18vh]">
      <form onSubmit={onSubmit} className="rise w-full max-w-[400px] rounded-card shadow-card bg-surface p-8">
        <div className="mb-7 flex items-center gap-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-[8px] bg-well text-ink">
            <SiloGlyph className="h-5 w-5" />
          </span>
          <span className="text-[14px] font-medium">Silo Agent</span>
        </div>
        <h1 className="mb-6 text-title">Sign in</h1>
        <label htmlFor="silo-email" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          Email
        </label>
        <input
          id="silo-email"
          className={`${inputClass} mb-4`}
          value={email}
          onChange={(e) => setEm(e.target.value)}
          autoComplete="username"
          autoFocus
          required
        />
        <label htmlFor="silo-pass" className="mb-1.5 block text-[12px] font-medium text-ink-3">
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
        {err && (
          <p role="alert" className="mb-3 flex items-start gap-2 text-[13px] text-vermilion">
            <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
            <span>{err}</span>
          </p>
        )}
        <Btn kind="primary" className="w-full justify-center" type="submit" disabled={busy}>
          {busy ? "Signing in…" : "Sign in"}
        </Btn>
      </form>
    </div>
  );
}

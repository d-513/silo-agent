import { useState, type FormEvent } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { SaveButton, useSave } from "../Feedback";
import { ErrorWell, Field, inputClass, Panel } from "../Field";
import { UI, type User } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { passwordProblem } from "../signinFlow";

// PasswordPanel changes the password, or sets a first one for an account that
// so far only signs in with single sign-on.
export function PasswordPanel({ user, onChange }: { user: User; onChange: (u: User) => void }) {
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [err, setErr] = useState("");
  const saver = useSave();
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const problem = passwordProblem(password, again);
    if (problem) {
      setErr(problem);
      return;
    }
    setErr("");
    try {
      onChange(await saver.run(() => ui.changePassword({ current, password })));
      setCurrent("");
      setPassword("");
      setAgain("");
      // The other sessions were signed out with the old password.
      void reload(UI.method.listSessions);
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  return (
    <Panel
      title="Password"
      note={user.hasPassword ? "Changing it signs you out everywhere else." : "This account has no password yet."}
    >
      <form onSubmit={onSubmit} className="grid gap-4">
        <input className="hidden" type="email" autoComplete="username" value={user.email} readOnly />
        {user.hasPassword ? (
          <Field label="Current password">
            <input className={inputClass} type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
          </Field>
        ) : null}
        <Field label="New password" hint="At least 8 characters.">
          <input className={inputClass} type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </Field>
        <Field label="New password again">
          <input className={inputClass} type="password" autoComplete="new-password" value={again} onChange={(e) => setAgain(e.target.value)} required />
        </Field>
        {err ? <ErrorWell>{err}</ErrorWell> : null}
        <div>
          <SaveButton state={saver.state} type="submit" savedLabel="Changed">
            {user.hasPassword ? "Change password" : "Set password"}
          </SaveButton>
        </div>
      </form>
    </Panel>
  );
}

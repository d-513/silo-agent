import { useState, type FormEvent } from "react";
import { ui } from "../api";
import { setSession } from "../auth";
import { fail } from "../errors";
import { SaveButton, useSave } from "../Feedback";
import { ErrorWell, Field, inputClass, Panel } from "../Field";
import { type User } from "../gen/silo/v1/ui_pb";

// EmailPanel changes the address the account signs in with. Silo sends no
// mail, so the change is immediate and nothing is confirmed.
export function EmailPanel({ user, onChange }: { user: User; onChange: (u: User) => void }) {
  // Only what was typed is kept; until then the field shows the account's email.
  const [draft, setDraft] = useState<string | null>(null);
  const [err, setErr] = useState("");
  const saver = useSave();
  const email = draft ?? user.email;
  const changed = email.trim().toLowerCase() !== user.email;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      const u = await saver.run(() => ui.changeEmail({ email }));
      onChange(u);
      // The rail and the admin pages know the signed-in user by this.
      setSession({ email: u.email, admin: u.admin });
      setDraft(null);
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  return (
    <Panel title="Email" note={methods(user)}>
      <form onSubmit={onSubmit} className="grid gap-4">
        <Field label="Email" hint="What you sign in with. It changes at once; Silo sends no mail to confirm it.">
          <input className={inputClass} type="email" autoComplete="username" spellCheck={false} value={email} onChange={(e) => setDraft(e.target.value)} required />
        </Field>
        {err ? <ErrorWell>{err}</ErrorWell> : null}
        <div>
          <SaveButton state={saver.state} type="submit" savedLabel="Changed" disabled={!changed}>
            Change email
          </SaveButton>
        </div>
      </form>
    </Panel>
  );
}

function methods(u: User): string {
  if (u.hasPassword && u.oidc) return "You sign in with your password or with single sign-on.";
  if (u.oidc) return "You sign in with single sign-on. Set a password to be able to sign in without it.";
  return "You sign in with your password.";
}

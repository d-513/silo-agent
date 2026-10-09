import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { ui } from "../../api";
import { Btn } from "../../Btn";
import { fail } from "../../errors";
import { CopyButton } from "../../Feedback";
import { ErrorWell, Field, inputClass, Panel } from "../../Field";
import { day } from "../../format";
import { type Invite } from "../../gen/silo/v1/ui_pb";
import { PageHead } from "../../PageHead";
import { ToggleRow } from "../../Switch";
import { reloadUsers } from "./useUsers";

// InviteUser makes the link that becomes someone's account.
export function InviteUser() {
  const navigate = useNavigate();
  const back = () => void navigate({ to: "/admin/users" });
  const [email, setEmail] = useState("");
  const [admin, setAdmin] = useState(false);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [made, setMade] = useState<Invite | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      setMade(await ui.createInvite({ email, admin }));
      void reloadUsers();
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="max-w-xl">
      <PageHead title="Invite someone" onBack={back} />
      {made ? (
        <Panel title={`Link for ${made.email}`} note={`Send it to them yourself. It works once, until ${day(made.expiresAt)}, and is not shown again.`}>
          <LinkWell url={made.url} />
          <div className="mt-4">
            <Btn kind="primary" type="button" onClick={back}>
              Done
            </Btn>
          </div>
        </Panel>
      ) : (
        <Panel>
          <form onSubmit={onSubmit} className="grid gap-5">
            <Field label="Email" hint="What they sign in with. Silo does not send mail: you pass the link on.">
              <input className={inputClass} type="email" autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="name@example.com" autoFocus required />
            </Field>
            <ToggleRow label="Admin" hint="Admins change settings and manage everyone's accounts. They do not see other people's Bots." on={admin} onChange={setAdmin} />
            {err ? <ErrorWell>{err}</ErrorWell> : null}
            <div>
              <Btn kind="primary" type="submit" disabled={busy}>
                {busy ? "Making the link…" : "Make invite link"}
              </Btn>
            </div>
          </form>
        </Panel>
      )}
    </div>
  );
}

// A link that is shown once, with the one thing to do with it.
export function LinkWell({ url }: { url: string }) {
  return (
    <div className="flex items-center gap-2 rounded-sm bg-well py-1.5 pl-3 pr-1.5">
      <code className="min-w-0 flex-1 break-all font-mono text-[12.5px] leading-5 text-ink">{url}</code>
      <CopyButton label text={url} title="Copy the link" className="shrink-0" />
    </div>
  );
}

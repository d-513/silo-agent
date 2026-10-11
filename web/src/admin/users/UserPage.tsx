import { useNavigate, useParams } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { ui } from "../../api";
import { setSession, useAuth } from "../../auth";
import { Btn } from "../../Btn";
import { fail } from "../../errors";
import { ArmedButton, SaveButton, useSave } from "../../Feedback";
import { ErrorWell, Field, inputClass, Panel, SkeletonRows } from "../../Field";
import { ago, day } from "../../format";
import { type Invite, type User } from "../../gen/silo/v1/ui_pb";
import { PageHead } from "../../PageHead";
import { ToggleRow } from "../../Switch";
import { LinkWell } from "./InviteUser";
import { dropUser, reloadUsers, setUser, signInWays, useUsers } from "./useUsers";
import { t } from "../../i18n.ts";

// UserPage is one account, as an admin sees it: its role, whether it may sign
// in, a way back in for someone locked out, and removing it.
export function UserPage() {
  const { userId } = useParams({ from: "/_authed/admin/users/$userId" });
  const navigate = useNavigate();
  const back = () => void navigate({ to: "/admin/users" });
  const { loaded, users, err } = useUsers();
  const { email } = useAuth();
  const user = users.find((u) => u.id === userId);
  const self = user?.email === email;
  const [actErr, setErr] = useState("");

  // Every change answers with the user as they now are.
  async function change(req: { admin?: boolean; disabled?: boolean; password?: string; resetTotp?: boolean; email?: string }) {
    setErr("");
    try {
      const u = await ui.updateUser({ id: userId, ...req });
      setUser(u);
      // An admin's own row: the session is known by its email.
      if (self) setSession({ email: u.email, admin: u.admin });
      return true;
    } catch (e) {
      setErr(fail(e));
      return false;
    }
  }
  async function remove() {
    setErr("");
    try {
      await ui.deleteUser({ id: userId });
      dropUser(userId);
      back();
    } catch (e) {
      setErr(fail(e));
    }
  }

  if (!user) {
    return (
      <div className="max-w-xl">
        <PageHead title={t("User")} onBack={back} />
        {err ? <ErrorWell>{err}</ErrorWell> : loaded ? <p className="text-ink-2">{t("There is no such user. They may have been deleted.")}</p> : <SkeletonRows rows={3} height={96} />}
      </div>
    );
  }
  return (
    <div className="max-w-xl">
      <PageHead title={user.email} subtitle={`${t("Joined {{date}}", { date: day(user.createdAt) })} · ${user.lastSignInAt ? t("signed in {{when}}", { when: ago(user.lastSignInAt) }) : t("never signed in")}`} onBack={back} />
      <div className="grid gap-5">
        {actErr ? <ErrorWell>{actErr}</ErrorWell> : null}
        <EmailForm user={user} onEmail={(email) => change({ email })} />
        <Panel title={t("Access")} note={self ? t("This is your account. Another admin has to change your role or access.") : undefined} padded={false}>
          <div className="divide-y divide-line-strong">
            <ToggleRow
              className="px-5 py-3.5"
              label={t("Admin")}
              hint={t("Changes settings and manages everyone's accounts. Does not see other people's Bots.")}
              on={user.admin}
              disabled={self}
              onChange={(v) => void change({ admin: v })}
            />
            <ToggleRow
              className="px-5 py-3.5"
              label={t("Disabled")}
              hint={t("Cannot sign in, and their Bots stop: no runs, automations, channels or tunnels. Nothing is deleted.")}
              on={user.disabled}
              disabled={self}
              onChange={(v) => void change({ disabled: v })}
            />
          </div>
        </Panel>
        <BackIn user={user} onPassword={(password) => change({ password })} onError={setErr} />
        {user.totp ? (
          <Panel title={t("Two-factor")} note={t("On. If they lost their phone and their recovery codes, turn it off so their password alone signs them in again.")}>
            <ArmedButton armedLabel={t("Click again to turn it off")} onConfirm={() => void change({ resetTotp: true })}>
              {t("Turn off two-factor")}
            </ArmedButton>
          </Panel>
        ) : null}
        {self ? null : (
          <Panel
            tone="danger"
            title={t("Delete user")}
            note={
              user.bots === 0
                ? t("Removes the account with everything it owns and its personal skills. This cannot be undone.")
                : t("Removes the account with its {{count}} Bots, their files and its personal skills. This cannot be undone.", { count: user.bots })
            }
          >
            <ArmedButton kind="deny" armedLabel={t("Click again to delete")} onConfirm={() => void remove()}>
              {t("Delete user")}
            </ArmedButton>
          </Panel>
        )}
      </div>
    </div>
  );
}

// BackIn is the two ways an admin gets someone back into their account: a
// link for them to choose a new password, or a password set here and now.
function BackIn({ user, onPassword, onError }: { user: User; onPassword: (password: string) => Promise<boolean>; onError: (e: string) => void }) {
  const [link, setLink] = useState<Invite | null>(null);
  const [busy, setBusy] = useState(false);
  const [password, setPassword] = useState("");
  const saver = useSave();

  async function makeLink() {
    onError("");
    setBusy(true);
    try {
      setLink(await ui.createInvite({ userId: user.id }));
      void reloadUsers();
    } catch (e) {
      onError(fail(e));
    } finally {
      setBusy(false);
    }
  }
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    try {
      await saver.run(async () => {
        if (!(await onPassword(password))) throw new Error("not saved");
      });
      setPassword("");
      setLink(null);
    } catch {
      /* the page shows why */
    }
  }
  return (
    <Panel title={t("Password")} note={t("{{user}}. A new password signs them out everywhere.", { user: signInWays(user) })}>
      <div className="grid gap-5">
        <div>
          <div className="mb-1.5 text-[12px] font-medium text-ink-3">{t("Reset link")}</div>
          {link ? (
            <>
              <LinkWell url={link.url} />
              <p className="mt-1.5 text-[12.5px] leading-[18px] text-ink-3">{t("Send it to them. It works once, until {{date}}, and is not shown again.", { date: day(link.expiresAt) })}</p>
            </>
          ) : (
            <>
              <Btn type="button" disabled={busy} onClick={() => void makeLink()}>
                {busy ? t("Making the link…") : t("Make reset link")}
              </Btn>
              <p className="mt-1.5 text-[12.5px] leading-[18px] text-ink-3">{t("They open it and choose a new password themselves.")}</p>
            </>
          )}
        </div>
        <form onSubmit={onSubmit}>
          <Field label={t("Or set a password")} hint={t("At least 8 characters. You will have to tell them what it is.")}>
            <div className="flex items-center gap-2">
              <input className={`${inputClass} font-mono`} type="text" autoComplete="off" spellCheck={false} value={password} onChange={(e) => setPassword(e.target.value)} />
              <SaveButton kind="secondary" state={saver.state} type="submit" savedLabel={t("Set")} disabled={!password}>
                {t("Set")}
              </SaveButton>
            </div>
          </Field>
        </form>
      </div>
    </Panel>
  );
}

// EmailForm is the address a user signs in with, as an admin sets it. Unlike
// one the user picks for themselves, single sign-on may match an account by it.
function EmailForm({ user, onEmail }: { user: User; onEmail: (email: string) => Promise<boolean> }) {
  const [draft, setDraft] = useState<string | null>(null);
  const saver = useSave();
  const email = draft ?? user.email;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    try {
      await saver.run(async () => {
        if (!(await onEmail(email))) throw new Error("not saved");
      });
      setDraft(null);
    } catch {
      /* the page shows why */
    }
  }
  return (
    <Panel title={t("Email")}>
      <form onSubmit={onSubmit}>
        <Field hint={t("What they sign in with. They can change it themselves; single sign-on only matches an account by an address set here or by an invite.")}>
          <div className="flex items-center gap-2">
            <input className={inputClass} type="email" autoComplete="off" spellCheck={false} value={email} onChange={(e) => setDraft(e.target.value)} required />
            <SaveButton kind="secondary" state={saver.state} type="submit" savedLabel={t("Set")}>
              {t("Set")}
            </SaveButton>
          </div>
        </Field>
      </form>
    </Panel>
  );
}

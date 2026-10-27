import { useState } from "react";
import { useAuth } from "../auth";
import { Btn } from "../Btn";
import { ArmedButton } from "../Feedback";
import { t } from "../i18n.ts";
import { Tip } from "../Tip";

// A connection or drive someone signed in to acts as them. Personal (the
// default), it may only be in Bots they alone use; shared, any Bot of the
// Workspace may get it. Only the lender switches it.

/** LendChip says whose identity a resource carries and, to its lender, offers the switch. */
export function LendChip({ lentBy, shared, solo, onShare }: { lentBy: string; shared: boolean; solo: boolean; onShare: (shared: boolean) => Promise<void> }) {
  const { email } = useAuth();
  const [busy, setBusy] = useState(false);
  if (!lentBy || solo) return null;
  const mine = lentBy === email;
  const set = async (v: boolean) => {
    setBusy(true);
    try {
      await onShare(v);
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="flex items-center gap-1.5">
      <Tip
        content={
          <p className="max-w-[280px] text-[12.5px]">
            {shared ? t("Any Bot in the workspace may get it, and acts as {{who}}.", { who: lentBy }) : t("Only Bots that {{who}} alone uses may get it.", { who: lentBy })}
          </p>
        }
      >
        <span tabIndex={0} className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-2 outline-none">
          {shared ? t("Shared") : t("Personal")}
        </span>
      </Tip>
      {mine ? (
        shared ? (
          <Btn kind="ghost" size="sm" disabled={busy} onClick={() => void set(false)}>
            {t("Make personal")}
          </Btn>
        ) : (
          <ArmedButton kind="ghost" size="sm" disabled={busy} armedLabel={t("Click again: Bots act as you")} onConfirm={() => void set(true)}>
            {t("Share with workspace")}
          </ArmedButton>
        )
      ) : null}
    </span>
  );
}

/**
 * SignInChoice is Sign in with "Who can use this sign-in" when the Workspace
 * has other people: Only my Bots (personal, the default) or Anyone in it
 * (shared, a second click).
 */
export function SignInChoice({ solo, space, onSignIn }: { solo: boolean; space: string; onSignIn: (shared: boolean) => void }) {
  const [open, setOpen] = useState(false);
  if (solo) {
    return (
      <Btn kind="primary" onClick={() => onSignIn(false)}>
        {t("Sign in")}
      </Btn>
    );
  }
  if (!open) {
    return (
      <Btn kind="primary" onClick={() => setOpen(true)}>
        {t("Sign in")}
      </Btn>
    );
  }
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <Tip content={<p className="max-w-[260px] text-[12.5px]">{t("Bots that only you use. Nobody else can give it to a Bot.")}</p>}>
        <Btn kind="primary" onClick={() => onSignIn(false)}>
          {t("Only my Bots")}
        </Btn>
      </Tip>
      <ArmedButton kind="secondary" armedLabel={t("Click again: Bots act as you")} onConfirm={() => onSignIn(true)}>
        {t("Anyone in {{space}}", { space })}
      </ArmedButton>
    </span>
  );
}

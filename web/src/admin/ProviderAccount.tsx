import { useQuery } from "@connectrpc/connect-query";
import { ExternalLink, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { ui } from "../api";
import { Btn, btnClass } from "../Btn";
import { fail } from "../errors";
import { ArmedButton, CopyButton, Spinner } from "../Feedback";
import { ErrorWell, inputClass } from "../Field";
import { UI, type Provider, type ProviderAccount as Account } from "../gen/silo/v1/ui_pb";
import { locale, t } from "../i18n.ts";
import { put, reload } from "../query";
import { Tip } from "../Tip";

// ProviderNotice is what the operator must know before using a provider,
// above its settings.
export function ProviderNotice({ text }: { text: string }) {
  return (
    <p className="flex items-start gap-2.5 rounded-control bg-well px-3 py-2.5 text-small leading-5 text-ink-2">
      <TriangleAlert className="mt-0.5 shrink-0 text-vermilion" size={15} />
      <span>{text}</span>
    </p>
  );
}

/** Who is signed in, as one line. */
export function accountLine(a: Account): string {
  if (!a.signedIn) return t("Not signed in");
  const parts = [a.email ? t("Signed in as {{email}}", { email: a.email }) : a.method === "oauth_token" ? t("Signed in with a long-lived token") : t("Signed in")];
  if (a.plan) parts.push(t("{{plan}} plan", { plan: a.plan.charAt(0).toUpperCase() + a.plan.slice(1) }));
  if (a.expiresAt > 0n) parts.push(t("until {{date}}", { date: new Date(Number(a.expiresAt) * 1000).toLocaleDateString(locale()) }));
  return parts.join(" · ");
}

// ProviderAccount is the sign-in of a provider that takes one instead of a
// key: who is signed in, and the steps to sign in (open the provider's page,
// paste the code it shows). It is the same for every such provider; the
// provider only names its ways to sign in.
export function ProviderAccount({ provider }: { provider: Provider }) {
  const input = { provider: provider.id };
  const account = useQuery(UI.method.getProviderAccount, input, { retry: false });
  // step is a sign-in under way: its method and the page to open.
  const [step, setStep] = useState<{ method: string; url: string } | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  // Whatever changed who is signed in also changed what the settings say
  // (the provider's lamp, a token saved) and which models it lists.
  const settle = (next: Account) => {
    put(UI.method.getProviderAccount, input, next);
    void reload(UI.method.getSettings);
    void reload(UI.method.listProviderModels, input);
  };
  const act = async (name: string, fn: () => Promise<void>) => {
    setBusy(name);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(fail(e));
    } finally {
      setBusy("");
    }
  };
  const begin = (method: string) =>
    act(method, async () => {
      const res = await ui.beginProviderSignIn({ provider: provider.id, method });
      setCode("");
      setStep({ method, url: res.url });
    });
  const finish = () =>
    act("finish", async () => {
      settle(await ui.finishProviderSignIn({ provider: provider.id, code }));
      setStep(null);
      setCode("");
    });
  const signOut = () => act("out", async () => settle(await ui.signOutProvider(input)));

  if (account.isPending) {
    return (
      <div className="flex h-9 items-center gap-2 text-small text-ink-3">
        <Spinner size={13} /> {t("Checking the sign-in…")}
      </div>
    );
  }
  if (account.error) return <ErrorWell>{fail(account.error)}</ErrorWell>;
  const a = account.data;
  const method = step ? a.methods.find((m) => m.id === step.method) : undefined;

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <span className="flex min-w-0 flex-1 items-center gap-2 text-row text-ink">
          <span aria-hidden className={`h-[7px] w-[7px] shrink-0 rounded-full ${a.signedIn ? "bg-lamp" : "bg-canvas shadow-[inset_0_0_0_1.5px_var(--color-line-strong)]"}`} />
          <span className="min-w-0 truncate">{accountLine(a)}</span>
        </span>
        {step ? null : (
          <span className="flex flex-wrap items-center gap-2">
            {a.methods.map((m, i) => (
              <Tip key={m.id} content={m.description}>
                <Btn size="sm" kind={i === 0 && !a.signedIn ? "primary" : "secondary"} disabled={busy !== ""} onClick={() => void begin(m.id)}>
                  {busy === m.id ? <Spinner size={13} tone={i === 0 && !a.signedIn ? "on-ink" : "ink"} /> : null}
                  {i === 0 && a.signedIn ? t("Sign in again") : m.label}
                </Btn>
              </Tip>
            ))}
            {a.signedIn ? (
              <ArmedButton size="sm" armedLabel={t("Click again to sign out")} disabled={busy !== ""} onConfirm={() => void signOut()}>
                {t("Sign out")}
              </ArmedButton>
            ) : null}
          </span>
        )}
      </div>
      {a.problem ? <ErrorWell>{a.problem}</ErrorWell> : null}
      {step ? (
        <div className="rounded-control bg-well px-4 py-3.5">
          {method ? <p className="mb-3 text-small leading-5 text-ink-2">{method.description}</p> : null}
          <ol className="grid gap-3 text-small text-ink">
            <li className="flex flex-wrap items-center gap-2">
              <StepNo n={1} />
              <span className="min-w-0 flex-1">{t("Open {{name}}'s sign-in page and sign in there.", { name: provider.name })}</span>
              <a href={step.url} target="_blank" rel="noreferrer" className={btnClass("primary", "", "sm")}>
                {t("Open the page")} <ExternalLink size={13} />
              </a>
              <CopyButton text={step.url} title={t("Copy the address")} />
            </li>
            <li className="flex items-center gap-2">
              <StepNo n={2} />
              <span>{t("The page ends on a code. Copy it.")}</span>
            </li>
            <li className="flex flex-wrap items-center gap-2">
              <StepNo n={3} />
              <form
                className="flex min-w-0 flex-1 flex-wrap items-center gap-2"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (code.trim() && busy === "") void finish();
                }}
              >
                <input
                  autoFocus
                  className={`${inputClass} min-w-[12rem] flex-1 font-mono`}
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  placeholder={t("Paste the code")}
                  aria-label={t("Sign-in code")}
                  autoComplete="off"
                  spellCheck={false}
                />
                <Btn size="sm" kind="primary" type="submit" disabled={!code.trim() || busy !== ""}>
                  {busy === "finish" ? <Spinner size={13} tone="on-ink" /> : null}
                  {t("Finish")}
                </Btn>
                <Btn
                  size="sm"
                  kind="ghost"
                  type="button"
                  disabled={busy !== ""}
                  onClick={() => {
                    setStep(null);
                    setError("");
                  }}
                >
                  {t("Cancel")}
                </Btn>
              </form>
            </li>
          </ol>
        </div>
      ) : null}
      {error ? <ErrorWell>{error}</ErrorWell> : null}
    </div>
  );
}

function StepNo({ n }: { n: number }) {
  return <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-surface font-mono text-label text-ink-2 shadow-card tabular-nums">{n}</span>;
}

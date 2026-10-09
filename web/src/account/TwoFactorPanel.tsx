import { useState, type FormEvent, type ReactNode } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { CopyButton } from "../Feedback";
import { ErrorWell, Field, inputClass, Panel } from "../Field";
import { type StartTOTPResponse, type User } from "../gen/silo/v1/ui_pb";

// What the panel is in the middle of. Nothing here outlives a reload: an
// enrolment that was not finished simply starts again.
type Step =
  | { at: "idle" }
  | { at: "password" } // off: asking for the password before showing a secret
  | { at: "scan"; setup: StartTOTPResponse } // off: QR shown, waiting for the first code
  | { at: "codes"; codes: string[] } // recovery codes, shown once
  | { at: "off" } // on: asking for a code to turn it off
  | { at: "renew" }; // on: asking for a code to replace the recovery codes

// TwoFactorPanel enrols an authenticator app for password sign-in, shows the
// recovery codes once, and turns it all off again.
export function TwoFactorPanel({ user, onChange }: { user: User; onChange: (u: User) => void }) {
  const [step, setStep] = useState<Step>({ at: "idle" });
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  function go(next: Step) {
    setStep(next);
    setText("");
    setErr("");
  }
  // run does one step's call; a failure stays on the step with its reason.
  async function run(e: FormEvent, call: () => Promise<void>) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      await call();
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }

  let body: ReactNode;
  if (step.at === "codes") {
    body = (
      <div>
        <p className="mb-3 text-[13.5px] leading-[21px] text-ink-2">
          Keep these recovery codes somewhere safe. Each one signs you in once if you lose your phone. They are not shown again.
        </p>
        <ul className="grid grid-cols-2 gap-x-6 gap-y-1.5 rounded-sm bg-well px-4 py-3 font-mono text-[13px] text-ink">
          {step.codes.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
        <div className="mt-4 flex items-center gap-2">
          <Btn kind="primary" type="button" onClick={() => go({ at: "idle" })}>
            I saved them
          </Btn>
          <CopyButton label text={step.codes.join("\n")} title="Copy the codes" />
        </div>
      </div>
    );
  } else if (step.at === "password") {
    body = (
      <form
        className="grid gap-4"
        onSubmit={(e) =>
          run(e, async () => {
            const setup = await ui.startTOTP({ password: text });
            go({ at: "scan", setup });
          })
        }
      >
        <Field label="Your password" hint="To make sure it is you setting this up.">
          <input className={inputClass} type="password" autoComplete="current-password" value={text} onChange={(e) => setText(e.target.value)} autoFocus required />
        </Field>
        {err ? <ErrorWell>{err}</ErrorWell> : null}
        <Actions busy={busy} label="Continue" onCancel={() => go({ at: "idle" })} />
      </form>
    );
  } else if (step.at === "scan") {
    const { setup } = step;
    body = (
      <form
        className="grid gap-4"
        onSubmit={(e) =>
          run(e, async () => {
            const r = await ui.confirmTOTP({ code: text });
            if (r.user) onChange(r.user);
            go({ at: "codes", codes: r.codes });
          })
        }
      >
        <p className="text-[13.5px] leading-[21px] text-ink-2">Scan this with your authenticator app, then enter the code it shows.</p>
        <div className="flex flex-wrap items-start gap-5">
          {/* A QR code is black on white whatever the theme, or it does not scan. */}
          <img src={setup.qr} alt="QR code for your authenticator app" width={168} height={168} className="rounded-sm shadow-[0_0_0_1px_var(--color-line-strong)]" />
          <div className="min-w-0 flex-1 basis-48">
            <div className="mb-1 text-[12px] font-medium text-ink-3">Or type this key</div>
            <div className="flex items-center gap-1">
              <code className="min-w-0 break-all font-mono text-[12.5px] text-ink">{setup.secret}</code>
              <CopyButton text={setup.secret} title="Copy the key" />
            </div>
          </div>
        </div>
        <CodeField value={text} onChange={setText} />
        {err ? <ErrorWell>{err}</ErrorWell> : null}
        <Actions busy={busy} label="Turn on" onCancel={() => go({ at: "idle" })} />
      </form>
    );
  } else if (step.at === "off" || step.at === "renew") {
    const off = step.at === "off";
    body = (
      <form
        className="grid gap-4"
        onSubmit={(e) =>
          run(e, async () => {
            if (off) {
              onChange(await ui.disableTOTP({ code: text }));
              go({ at: "idle" });
              return;
            }
            const r = await ui.newRecoveryCodes({ code: text });
            go({ at: "codes", codes: r.codes });
          })
        }
      >
        <CodeField
          value={text}
          onChange={setText}
          hint={off ? "A code from your authenticator app, or a recovery code." : "A code from your authenticator app. The recovery codes you have now stop working."}
        />
        {err ? <ErrorWell>{err}</ErrorWell> : null}
        <Actions busy={busy} label={off ? "Turn off" : "Make new codes"} onCancel={() => go({ at: "idle" })} />
      </form>
    );
  } else if (user.totp) {
    body = (
      <div className="flex flex-wrap items-center gap-2">
        <Btn type="button" onClick={() => go({ at: "renew" })}>
          New recovery codes
        </Btn>
        <Btn kind="ghost" type="button" onClick={() => go({ at: "off" })}>
          Turn off
        </Btn>
      </div>
    );
  } else {
    body = (
      <Btn type="button" onClick={() => go({ at: "password" })}>
        Set up
      </Btn>
    );
  }
  return (
    <Panel
      title="Two-factor"
      note={
        user.totp
          ? "On. Signing in with your password also asks for a code from your authenticator app."
          : "Off. Turn it on to be asked for a code from an authenticator app when you sign in with your password."
      }
    >
      {body}
    </Panel>
  );
}

function CodeField({ value, onChange, hint }: { value: string; onChange: (v: string) => void; hint?: string }) {
  return (
    <Field label="Code" hint={hint} className="max-w-[240px]">
      <input
        className={`${inputClass} font-mono tracking-[0.12em]`}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="one-time-code"
        autoCapitalize="none"
        spellCheck={false}
        autoFocus
        required
      />
    </Field>
  );
}

function Actions({ busy, label, onCancel }: { busy: boolean; label: string; onCancel: () => void }) {
  return (
    <div className="flex items-center gap-2">
      <Btn kind="primary" type="submit" disabled={busy}>
        {label}
      </Btn>
      <Btn kind="ghost" type="button" onClick={onCancel}>
        Cancel
      </Btn>
    </div>
  );
}

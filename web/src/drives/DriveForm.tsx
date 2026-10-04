import { ChevronRight, CircleCheck, RefreshCw, Trash2 } from "lucide-react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ui } from "../api";
import { Btn } from "../Btn";
import { ArmedButton, SaveButton, Spinner } from "../Feedback";
import { ErrorWell, Field, inputClass } from "../Field";
import { PageHead } from "../PageHead";
import { ToggleRow } from "../Switch";
import type { Drive, DriveTemplate } from "../gen/silo/v1/ui_pb";
import { DriveMark } from "./DriveMark";
import { FolderPicker } from "./FolderPicker";
import { PickInput } from "./PickInput";
import { Step } from "./Step";
import { VarInput } from "./VarInput";
import { useDriveForm } from "./useDriveForm";

// The add/edit drive page: connect, choose what to mount, name and access.
export function DriveForm({
  botId,
  t,
  drive,
  taken,
  onDone,
}: {
  botId: string;
  t: DriveTemplate;
  drive?: Drive;
  taken: Set<string>;
  onDone: () => void;
}) {
  const {
    editing, draft, values, setValues, secrets, name, setName, readOnly, setReadOnly, err, authBusy, authErr, test, saver,
    oauth, connectVars, mountVars, advancedVars, connected, requiredMissing, set, setSecret, signIn, runTest, browse, pickLoader,
    nameOk, canSave, save, pathPreview,
  } = useDriveForm({ botId, t, drive, taken, onDone });
  const varInputs = (vars: typeof connectVars) =>
    vars.map((v) => (
      <VarInput
        key={v.key}
        v={v}
        value={v.secret ? secrets[v.key] ?? "" : values[v.key] ?? ""}
        onChange={(s) => (v.secret ? setSecret(v.key, s) : set(v.key, s))}
        isSet={draft?.secretsSet.includes(v.key)}
      />
    ));


  return (
    <div className="silo-page">
      <PageHead title={editing ? drive!.name : t.title} subtitle={editing ? t.title : t.blurb} mark={<DriveMark svg={t.iconSvg} size={44} />} onBack={onDone} />

      {!editing && t.guide ? (
        <div className="mb-5 rounded-card border-l-4 border-cobalt bg-well px-4 py-3">
          <p className="mb-1.5 text-label-caps uppercase text-ink-3">Before you add</p>
          <div className="silo-md text-[14px] leading-[22px] text-ink">
            <Markdown remarkPlugins={[remarkGfm]}>{t.guide}</Markdown>
          </div>
        </div>
      ) : null}

      <div className="space-y-4">
        <Step
          n={1}
          title={oauth ? `Connect ${t.authLabel || t.title}` : "Connect"}
          note={oauth ? "Sign in with the account whose files the Bot should see." : "Where the files are, and how to sign in."}
          done={connected}
        >
          {oauth && !t.available ? (
            <ErrorWell>An admin has to set up {t.title} before anyone can connect it (Admin → Drives).</ErrorWell>
          ) : null}
          {varInputs(connectVars)}
          {oauth ? (
            draft?.connected ? (
              <div className="flex flex-wrap items-center gap-3 rounded-control bg-well px-3.5 py-3">
                <CircleCheck size={17} className="shrink-0 text-emerald" />
                <div className="min-w-0 flex-1 text-[13.5px] text-ink">
                  Connected{draft.account ? <> as <span className="font-medium">{draft.account}</span></> : null}
                </div>
                <Btn kind="ghost" size="sm" type="button" onClick={() => void signIn()} disabled={authBusy}>
                  {authBusy ? <Spinner size={12} /> : <RefreshCw size={13} />}
                  Change account
                </Btn>
              </div>
            ) : (
              <div className="flex flex-wrap items-center gap-3">
                <Btn kind="primary" type="button" disabled={!t.available || authBusy} onClick={() => void signIn()}>
                  {authBusy ? <Spinner size={13} tone="white" /> : null}
                  {authBusy ? "Waiting for sign-in…" : `Sign in with ${t.authLabel || t.title}`}
                </Btn>
                {authBusy ? <span className="text-[12.5px] text-ink-3">Finish in the window that opened.</span> : null}
              </div>
            )
          ) : editing ? null : (
            <div className="flex flex-wrap items-center gap-3">
              <Btn kind="secondary" type="button" disabled={requiredMissing.length > 0 || test.state === "busy"} onClick={() => void runTest()}>
                {test.state === "busy" ? <Spinner size={13} /> : null}
                {test.state === "busy" ? "Testing…" : "Test connection"}
              </Btn>
              {test.state === "ok" ? (
                <span className="flex items-center gap-1.5 text-[13px] text-emerald">
                  <CircleCheck size={15} /> {test.msg}
                </span>
              ) : requiredMissing.length > 0 ? (
                <span className="text-[12.5px] text-ink-3">Fill in {requiredMissing.map((v) => v.label).join(", ")} first.</span>
              ) : null}
            </div>
          )}
          {test.state === "fail" ? <ErrorWell>{test.msg}</ErrorWell> : null}
          {authErr ? <ErrorWell>{authErr}</ErrorWell> : null}
        </Step>

        {mountVars.length > 0 ? (
          <Step n={2} title="Choose what to mount" note="Only what you pick appears in the Bot." locked={!connected} done={connected && editing}>
            {mountVars.map((v) =>
              v.type === "pick" ? (
                <PickInput
                  key={`${v.key}:${draft?.account ?? ""}`}
                  v={v}
                  value={values[v.key] ?? ""}
                  ready={connected}
                  load={pickLoader(v.key)}
                  onPick={(o) => {
                    setValues((m) => {
                      const next = { ...m, [v.key]: o?.value ?? "" };
                      for (const [k, val] of Object.entries(o?.extra ?? {})) next[k] = val;
                      return next;
                    });
                  }}
                />
              ) : (
                <FolderPicker key={v.key} v={v} value={values[v.key] ?? ""} onChange={(s) => set(v.key, s)} browse={browse} ready={connected} />
              ),
            )}
          </Step>
        ) : null}

        <Step n={mountVars.length > 0 ? 3 : 2} title="Name and access" locked={!connected}>
          <Field
            label="Name"
            required
            error={name && !nameOk ? (taken.has(name) && name !== drive?.name ? "This Bot already has a drive with that name." : "Lowercase letters, digits, and dashes.") : undefined}
          >
            <input
              className={`${inputClass} font-mono`}
              value={name}
              spellCheck={false}
              onChange={(e) => setName(e.target.value.toLowerCase())}
            />
          </Field>
          <div className="-mt-2 flex items-center gap-2 text-[12.5px] text-ink-3">
            <span>The Bot sees it at</span>
            <code className="rounded-xs bg-well px-1.5 py-0.5 font-mono text-[12px] text-ink">{pathPreview}</code>
          </div>
          <ToggleRow
            className="rounded-control bg-well px-3.5 py-3"
            label="Read-only"
            hint="The Bot can open files but not change, add, or delete them."
            on={readOnly}
            onChange={setReadOnly}
          />
          {advancedVars.length > 0 ? (
            <details className="group">
              <summary className="flex cursor-pointer items-center gap-2 rounded-sm bg-well px-3 py-2 text-[12px] font-medium tracking-wide text-ink-3">
                <ChevronRight size={12} className="shrink-0 transition-transform group-open:rotate-90" />
                Advanced settings
              </summary>
              <div className="mt-4 space-y-4">
                {varInputs(advancedVars)}
              </div>
            </details>
          ) : null}
        </Step>

        {err ? <ErrorWell>{err}</ErrorWell> : null}

        <div className="flex flex-wrap items-center gap-2.5 pt-1">
          <SaveButton type="button" state={saver.state} disabled={!canSave} onClick={() => void save()}>
            {editing ? "Save changes" : "Add drive"}
          </SaveButton>
          <Btn kind="ghost" type="button" onClick={onDone}>
            Cancel
          </Btn>
          {!connected && !editing ? (
            <span className="text-[12.5px] text-ink-3">{oauth ? "Sign in first." : "Test the connection first."}</span>
          ) : null}
          {editing ? (
            <ArmedButton
              kind="ghost"
              className="ml-auto"
              icon={<Trash2 size={14} />}
              armedLabel="Click again to remove"
              onConfirm={async () => {
                await ui.deleteDrive({ id: drive!.id });
                onDone();
              }}
            >
              Remove drive
            </ArmedButton>
          ) : null}
        </div>
      </div>
    </div>
  );
}

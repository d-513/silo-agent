import { ChevronRight, Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ui } from "./api";
import { CATEGORY_LABEL, DriveMark } from "./BotDrives";
import { Btn } from "./Btn";
import { ArmedButton, CopyButton, SaveButton, useSave } from "./Feedback";
import { ErrorWell, Field, inputClass, Panel, SkeletonRows } from "./Field";
import type { DriveProviderSettings, DriveSettings, DriveSystemField, DriveTemplate } from "./gen/silo/v1/ui_pb";
import { fail } from "./errors";

function SystemField({
  f,
  value,
  onChange,
  onClear,
}: {
  f: DriveSystemField;
  value: string | undefined;
  onChange: (v: string) => void;
  onClear: () => void;
}) {
  const fromEnv = f.source === "env";
  const [replacing, setReplacing] = useState(false);
  const hint = fromEnv ? `Set by ${f.envName} in the environment, which wins over this page.` : f.help;
  if (f.secret && f.set && !replacing && value === undefined) {
    return (
      <Field label={f.label} required={f.required} hint={hint}>
        <div className="flex items-center gap-2">
          <div className="flex h-9 min-w-0 flex-1 items-center rounded-control bg-well px-3 font-mono text-[13px] text-ink-2">••••••••</div>
          {fromEnv ? null : (
            <>
              <Btn kind="secondary" type="button" onClick={() => setReplacing(true)}>
                Replace
              </Btn>
              <ArmedButton kind="ghost" armedLabel="Click again to clear" onConfirm={onClear}>
                Clear
              </ArmedButton>
            </>
          )}
        </div>
      </Field>
    );
  }
  return (
    <Field label={f.label} required={f.required} hint={hint}>
      <input
        className={`${inputClass} ${f.secret ? "font-mono" : ""}`}
        type={f.secret ? "password" : "text"}
        autoComplete={f.secret ? "new-password" : "off"}
        spellCheck={false}
        disabled={fromEnv}
        autoFocus={replacing}
        value={value ?? (f.secret ? "" : f.value)}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  );
}

function ProviderFolio({ t, p, redirect, onSaved }: { t: DriveTemplate; p: DriveProviderSettings; redirect: string; onSaved: (s: DriveSettings) => void }) {
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [err, setErr] = useState("");
  const saver = useSave();
  const dirty = Object.keys(edits).length > 0;

  async function put(values: Record<string, string>) {
    setErr("");
    try {
      const s = await saver.run(() => ui.putDriveSettings({ template: t.key, values }));
      setEdits({});
      onSaved(s);
    } catch (e) {
      setErr(fail(e));
    }
  }

  return (
    <details className="group overflow-hidden rounded-card bg-surface shadow-card" open={undefined}>
      <summary className="flex cursor-pointer items-center gap-3 px-4 py-3 transition-colors duration-[160ms] ease-quiet hover:bg-well">
        <ChevronRight size={14} className="shrink-0 text-ink-3 transition-transform duration-[200ms] ease-quiet group-open:rotate-90" />
        <DriveMark svg={t.iconSvg} size={30} />
        <span className="min-w-0 flex-1 truncate text-[14px] font-medium text-ink">{t.title}</span>
        {p.ready ? (
          <span className="text-[12px] font-medium text-emerald">Ready</span>
        ) : (
          <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">Needs setup</span>
        )}
      </summary>
      <div className="space-y-5 px-5 pb-5 pt-2 shadow-[inset_0_1px_0_var(--color-line)]">
        {t.setup ? (
          <div className="silo-md pt-3 text-[13.5px] leading-[22px] text-ink-2">
            <Markdown remarkPlugins={[remarkGfm]}>{t.setup}</Markdown>
          </div>
        ) : null}
        <div>
          <div className="mb-1.5 text-[12px] font-medium text-ink-3">Redirect URI to register</div>
          <div className="flex items-center gap-2 rounded-control bg-well px-3 py-2">
            <code className="min-w-0 flex-1 truncate font-mono text-[12.5px] text-ink">{redirect}</code>
            <CopyButton text={redirect} title="Copy redirect URI" />
          </div>
        </div>
        {p.fields.map((f) => (
          <SystemField
            key={f.key}
            f={f}
            value={edits[f.key]}
            onChange={(v) => setEdits((m) => ({ ...m, [f.key]: v }))}
            onClear={() => void put({ [f.key]: "" })}
          />
        ))}
        {err ? <ErrorWell>{err}</ErrorWell> : null}
        <div className="flex items-center gap-2.5">
          <SaveButton type="button" state={saver.state} disabled={!dirty} onClick={() => void put(edits)} />
          {dirty ? (
            <Btn kind="ghost" type="button" onClick={() => setEdits({})}>
              Discard
            </Btn>
          ) : null}
        </div>
      </div>
    </details>
  );
}

export function AdminDrives() {
  const [templates, setTemplates] = useState<DriveTemplate[] | null>(null);
  const [settings, setSettings] = useState<DriveSettings | null>(null);
  const [err, setErr] = useState("");
  const [q, setQ] = useState("");

  useEffect(() => {
    Promise.all([ui.listDriveTemplates({}), ui.getDriveSettings({})])
      .then(([t, s]) => {
        setTemplates(t.templates);
        setSettings(s);
      })
      .catch((e) => setErr(fail(e)));
  }, []);

  const byKey = useMemo(() => new Map((settings?.providers ?? []).map((p) => [p.template, p])), [settings]);
  const s = q.trim().toLowerCase();
  const match = (t: DriveTemplate) => !s || `${t.title} ${t.blurb}`.toLowerCase().includes(s);
  const needing = (templates ?? []).filter((t) => byKey.has(t.key) && match(t));
  const free = (templates ?? []).filter((t) => !byKey.has(t.key) && match(t));
  const ready = needing.filter((t) => byKey.get(t.key)?.ready).length;

  return (
    <div>
      <p className="mb-5 max-w-[600px] text-[13.5px] leading-[21px] text-ink-2">
        Drives let each Bot mount cloud storage as a folder. Providers that sign in with OAuth need a client registered once, here, before owners can connect them. The rest need nothing.
      </p>
      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}
      {!templates || !settings ? (
        <SkeletonRows rows={5} height={54} />
      ) : (
        <>
          <div className="mb-4 flex items-center gap-3">
            <div className="relative min-w-0 flex-1">
              <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-3" />
              <input className={`${inputClass} pl-9`} placeholder="Search providers" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search providers" />
            </div>
            <span className="shrink-0 text-[12.5px] tabular-nums text-ink-3">
              {ready} of {settings.providers.length} ready
            </span>
          </div>
          <div className="space-y-6">
            {Object.keys(CATEGORY_LABEL).map((c) => {
              const items = needing.filter((t) => t.category === c);
              if (items.length === 0) return null;
              return (
                <section key={c}>
                  <h3 className="mb-2.5 text-[11px] font-medium uppercase tracking-[0.08em] text-ink-3">{CATEGORY_LABEL[c]}</h3>
                  <div className="space-y-2">
                    {items.map((t) => (
                      <ProviderFolio key={t.key} t={t} p={byKey.get(t.key)!} redirect={settings.redirectUrl} onSaved={setSettings} />
                    ))}
                  </div>
                </section>
              );
            })}
            {free.length > 0 ? (
              <Panel title="No setup needed" note="Owners fill these in themselves when they add a drive.">
                <div className="flex flex-wrap gap-2">
                  {free.map((t) => (
                    <span key={t.key} className="flex items-center gap-2 rounded-control bg-well py-1.5 pl-1.5 pr-3 text-[13px] text-ink-2">
                      <DriveMark svg={t.iconSvg} size={22} className="bg-surface" />
                      {t.title}
                    </span>
                  ))}
                </div>
              </Panel>
            ) : null}
          </div>
        </>
      )}
    </div>
  );
}

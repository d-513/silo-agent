import { ArrowLeft, Check, ChevronRight, CircleAlert, CircleCheck, FolderOpen, HardDrive, Folder, Pencil, Plus, RefreshCw, Search, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import Markdown from "react-markdown";
import { useNavigate } from "react-router-dom";
import remarkGfm from "remark-gfm";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { ArmedButton, CopyButton, SaveButton, Spinner, useSave } from "./Feedback";
import { Field, inputClass, SkeletonRows, textareaClass } from "./Field";
import { Lamp } from "./Lamp";
import { Select } from "./Select";
import { ToggleRow } from "./Switch";
import { Tip, TipAction, TipTitle } from "./Tip";
import type { BrowseDriveDir, Drive, DriveOption, DriveTemplate, DriveVar } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export const CATEGORY_LABEL: Record<string, string> = {
  consumer: "Personal cloud",
  "self-hosted": "Self-hosted",
  "object-storage": "Object storage",
  protocol: "File servers",
};

// Provider marks are the brands' own SVGs (thesvg.org), drawn as an <img> from
// a data URI: several inline SVGs would share ids (gradients, masks) and break
// each other, and an <img> never runs markup from the file.
export function DriveMark({ svg, size = 40, muted = false, className = "" }: { svg?: string; size?: number; muted?: boolean; className?: string }) {
  const src = useMemo(() => (svg ? `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}` : ""), [svg]);
  return (
    <div
      aria-hidden
      className={`flex shrink-0 items-center justify-center rounded-sm bg-well ${className}`}
      style={{ width: size, height: size, padding: Math.round(size * 0.16) }}
    >
      {src ? (
        <img src={src} alt="" draggable={false} className={`h-full w-full object-contain ${muted ? "opacity-45 grayscale" : ""}`} />
      ) : (
        <HardDrive size={Math.round(size * 0.5)} className="text-ink-2" />
      )}
    </div>
  );
}

// state → the lamp and the word beside it. A lamp is never alone.
function driveLook(d: Drive): { lamp: string; word: string; tone: string } {
  switch (d.state) {
    case "mounted":
      return { lamp: "online", word: "Mounted", tone: "text-emerald" };
    case "mounting":
      return { lamp: "starting", word: "Connecting…", tone: "text-ink-2" };
    case "needs_auth":
      return { lamp: "needs_you", word: "Reconnect needed", tone: "text-vermilion" };
    case "needs_setup":
      return { lamp: "needs_you", word: "Waiting for an admin", tone: "text-vermilion" };
    case "needs_input":
      return { lamp: "needs_you", word: "Needs details", tone: "text-vermilion" };
    case "error":
      return { lamp: "needs_you", word: "Error", tone: "text-vermilion" };
    default:
      return { lamp: "stopped", word: "Stopped", tone: "text-ink-3" };
  }
}

function slug(s: string) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

function uniqueName(base: string, taken: Set<string>) {
  const b = slug(base) || "drive";
  if (!taken.has(b)) return b;
  for (let i = 2; ; i++) {
    const n = `${b.slice(0, 36)}-${i}`;
    if (!taken.has(n)) return n;
  }
}

const NAME_RE = /^[a-z0-9][a-z0-9-]{0,39}$/;

function visible(v: DriveVar, values: Record<string, string>, t: DriveTemplate) {
  for (const [k, want] of Object.entries(v.visibleIf)) {
    const def = t.vars.find((x) => x.kind === "user" && x.key === k)?.defaultValue ?? "";
    if ((values[k] || def) !== want) return false;
  }
  return true;
}

function PageHead({ title, subtitle, mark, onBack }: { title: string; subtitle?: string; mark?: ReactNode; onBack: () => void }) {
  return (
    <div className="mb-6 flex items-center gap-3.5">
      <button
        type="button"
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-sm bg-surface text-ink-2 shadow-card transition-colors hover:text-ink hover:shadow-float"
        onClick={onBack}
        aria-label="Back"
      >
        <ArrowLeft size={16} />
      </button>
      {mark}
      <div className="min-w-0">
        <h2 className="truncate text-[22px] font-medium leading-7 tracking-[-0.015em] text-ink">{title}</h2>
        {subtitle ? <div className="mt-0.5 text-[12.5px] text-ink-3">{subtitle}</div> : null}
      </div>
    </div>
  );
}

function ErrorWell({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="flex items-start gap-2 rounded-sm bg-vermilion-pale px-3 py-2.5 text-[13px] leading-5 text-vermilion">
      <CircleAlert size={15} className="mt-0.5 shrink-0" />
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

// A numbered step of the add form. Later steps stay visible but quiet until the
// earlier one is done, so the whole path is readable at a glance.
function Step({ n, title, note, done, locked, children }: { n: number; title: string; note?: string; done?: boolean; locked?: boolean; children: ReactNode }) {
  return (
    <section className={`rounded-card bg-surface p-5 shadow-card transition-opacity duration-[200ms] ease-quiet ${locked ? "opacity-55" : ""}`} aria-disabled={locked || undefined}>
      <header className="mb-4 flex items-start gap-3">
        <span
          className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[12px] font-semibold tabular-nums transition-colors duration-[240ms] ease-quiet ${
            done ? "bg-ink text-white" : "bg-well text-ink-2"
          }`}
        >
          {done ? <Check size={13} strokeWidth={2.5} /> : n}
        </span>
        <div className="min-w-0">
          <h3 className="text-[15px] font-semibold leading-6 tracking-[-0.01em] text-ink">{title}</h3>
          {note ? <p className="text-[12.5px] leading-[18px] text-ink-2">{note}</p> : null}
        </div>
      </header>
      <fieldset disabled={locked} className="space-y-4">
        {children}
      </fieldset>
    </section>
  );
}

function VarInput({
  v,
  value,
  onChange,
  isSet,
}: {
  v: DriveVar;
  value: string;
  onChange: (s: string) => void;
  isSet?: boolean;
}) {
  if (v.type === "bool") {
    return <ToggleRow className="rounded-control bg-well px-3.5 py-3" label={v.label} hint={v.help} on={value === "true"} onChange={(x) => onChange(x ? "true" : "false")} />;
  }
  const secretHint = v.secret ? (isSet ? "Saved. Type to replace it." : "Stays on the Silo server; the Bot never sees it.") : "";
  return (
    <Field label={v.label} required={v.required} hint={[v.help, secretHint].filter(Boolean).join(" ")}>
      {v.type === "select" ? (
        <Select value={value || v.defaultValue} onChange={onChange} options={v.options.map((o) => ({ value: o.value, label: o.label, hint: o.detail }))} />
      ) : v.type === "textarea" ? (
        <textarea
          className={`${textareaClass} min-h-[96px] font-mono text-[12.5px] leading-5`}
          value={value}
          spellCheck={false}
          placeholder={v.secret && isSet ? "•••••••• saved — paste to replace" : v.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <input
          className={`${inputClass} ${v.secret ? "font-mono" : ""}`}
          type={v.secret ? "password" : v.type === "number" ? "number" : v.type === "url" ? "url" : "text"}
          inputMode={v.type === "url" ? "url" : undefined}
          autoComplete={v.secret ? "new-password" : "off"}
          spellCheck={false}
          value={value}
          placeholder={v.secret && isSet ? "•••••••• saved" : v.placeholder || v.defaultValue}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
    </Field>
  );
}

// FolderPicker browses the remote live through the sidecar. It walks one level
// at a time (a crumb trail plus the folders here) instead of a tree: a remote
// drive can be huge, and every level is a network round trip.
function FolderPicker({
  v,
  value,
  onChange,
  browse,
  ready,
}: {
  v: DriveVar;
  value: string;
  onChange: (s: string) => void;
  browse: (path: string) => Promise<BrowseDriveDir[]>;
  ready: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [at, setAt] = useState(value);
  const [dirs, setDirs] = useState<BrowseDriveDir[] | null>(null);
  const [err, setErr] = useState("");
  const seq = useRef(0);

  const go = useCallback(
    async (path: string) => {
      const mine = ++seq.current;
      setAt(path);
      setDirs(null);
      setErr("");
      try {
        const got = await browse(path);
        if (mine === seq.current) setDirs(got);
      } catch (e) {
        if (mine === seq.current) {
          setErr(fail(e));
          setDirs([]);
        }
      }
    },
    [browse],
  );

  const crumbs = at ? at.split("/") : [];
  const rootLabel = v.placeholder || "Whole drive";

  return (
    <Field label={v.label} hint={v.help}>
      <div className="flex items-center gap-2">
        <div className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-control bg-well px-3 text-[13px]">
          <FolderOpen size={15} className="shrink-0 text-ink-3" />
          <span className={`truncate font-mono text-[12.5px] ${value ? "text-ink" : "text-ink-3"}`}>{value ? `/${value}` : rootLabel}</span>
        </div>
        {value ? (
          <Btn kind="ghost" type="button" onClick={() => onChange("")}>
            Clear
          </Btn>
        ) : null}
        <Btn
          kind="secondary"
          type="button"
          disabled={!ready}
          aria-expanded={open}
          onClick={() => {
            const next = !open;
            setOpen(next);
            if (next) void go(value);
          }}
        >
          {open ? "Close" : "Browse"}
        </Btn>
      </div>
      {open ? (
        <div className="mt-2 overflow-hidden rounded-control bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]">
          <nav className="silo-scroll-x flex items-center gap-1 px-2 py-2 text-[12.5px] shadow-[inset_0_-1px_0_var(--color-line)]" aria-label="Folder path">
            <button type="button" className={`shrink-0 rounded-sm px-2 py-1 ${at ? "text-ink-2 hover:bg-well hover:text-ink" : "font-medium text-ink"}`} onClick={() => void go("")}>
              {rootLabel}
            </button>
            {crumbs.map((c, i) => {
              const p = crumbs.slice(0, i + 1).join("/");
              const last = i === crumbs.length - 1;
              return (
                <span key={p} className="flex shrink-0 items-center gap-1">
                  <ChevronRight size={12} className="text-ink-3" />
                  <button type="button" className={`rounded-sm px-2 py-1 ${last ? "font-medium text-ink" : "text-ink-2 hover:bg-well hover:text-ink"}`} onClick={() => void go(p)}>
                    {c}
                  </button>
                </span>
              );
            })}
          </nav>
          <div className="max-h-[260px] overflow-auto py-1">
            {dirs === null ? (
              <SkeletonRows rows={3} height={32} className="p-2" />
            ) : err ? (
              <div className="p-2">
                <ErrorWell>{err}</ErrorWell>
              </div>
            ) : dirs.length === 0 ? (
              <p className="px-4 py-3 text-[12.5px] text-ink-3">No folders here.</p>
            ) : (
              dirs.map((d) => (
                <button
                  key={d.path}
                  type="button"
                  className="flex h-9 w-full items-center gap-2.5 px-3 text-left text-[13.5px] text-ink transition-colors duration-[160ms] ease-quiet hover:bg-well"
                  onClick={() => void go(d.path)}
                >
                  <Folder size={15} className="shrink-0 text-ink-3" />
                  <span className="min-w-0 flex-1 truncate">{d.name}</span>
                  <ChevronRight size={14} className="shrink-0 text-ink-3" />
                </button>
              ))
            )}
          </div>
          <div className="flex items-center justify-between gap-2 px-3 py-2 shadow-[inset_0_1px_0_var(--color-line)]">
            <span className="min-w-0 truncate font-mono text-[12px] text-ink-3">{at ? `/${at}` : rootLabel}</span>
            <Btn
              kind="primary"
              size="sm"
              type="button"
              onClick={() => {
                onChange(at);
                setOpen(false);
              }}
            >
              {at ? "Mount this folder" : "Mount everything"}
            </Btn>
          </div>
        </div>
      ) : null}
    </Field>
  );
}

function PickInput({
  v,
  value,
  onPick,
  load,
  ready,
}: {
  v: DriveVar;
  value: string;
  onPick: (o: DriveOption | null) => void;
  load: () => Promise<DriveOption[]>;
  ready: boolean;
}) {
  const [opts, setOpts] = useState<DriveOption[] | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!ready) return;
    let dead = false;
    setErr("");
    load()
      .then((o) => {
        if (!dead) setOpts(o);
      })
      .catch((e) => {
        if (!dead) {
          setErr(fail(e));
          setOpts([]);
        }
      });
    return () => {
      dead = true;
    };
  }, [ready, load]);
  const empty = v.required ? [] : [{ value: "", label: v.placeholder || "Default" }];
  const options = [...empty, ...(opts ?? []).map((o) => ({ value: o.value, label: o.label, hint: o.detail }))];
  if (value && !options.some((o) => o.value === value)) options.push({ value, label: value });
  return (
    <Field label={v.label} required={v.required} hint={err ? undefined : v.help} error={err || undefined}>
      <Select
        value={value}
        disabled={!ready || opts === null}
        placeholder={opts === null && ready ? "Loading…" : v.placeholder || "Choose…"}
        onChange={(val) => onPick((opts ?? []).find((o) => o.value === val) ?? (val ? null : null))}
        options={options}
      />
    </Field>
  );
}

// DriveForm is add and edit in one: Connect → Choose what to mount → Name &
// access. Adding keeps a server-side draft from the first step that needs the
// server (sign-in, test, browse), so those work before the drive exists.
function DriveForm({
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
  const editing = !!drive;
  const [draft, setDraft] = useState<Drive | undefined>(drive);
  const [values, setValues] = useState<Record<string, string>>(() => ({ ...(drive?.options ?? {}) }));
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [name, setName] = useState(drive?.name ?? uniqueName(t.title, taken));
  const [readOnly, setReadOnly] = useState(drive?.readOnly ?? false);
  const [err, setErr] = useState("");
  const [authBusy, setAuthBusy] = useState(false);
  const [authErr, setAuthErr] = useState("");
  const [test, setTest] = useState<{ state: "idle" | "busy" | "ok" | "fail"; msg: string }>({ state: "idle", msg: "" });
  const saver = useSave();
  const draftRef = useRef<Drive | undefined>(drive);
  draftRef.current = draft;

  const oauth = t.authKind === "oauth2";
  const userVars = t.vars.filter((v) => v.kind === "user" && v.type !== "hidden" && visible(v, values, t));
  const connectVars = userVars.filter((v) => !v.advanced && v.type !== "pick" && v.type !== "folder");
  const mountVars = userVars.filter((v) => !v.advanced && (v.type === "pick" || v.type === "folder"));
  const advancedVars = userVars.filter((v) => v.advanced);
  const connected = oauth ? !!draft?.connected : editing || test.state === "ok";
  const requiredMissing = connectVars.filter((v) => v.required && !(values[v.key] || secrets[v.key] || draft?.secretsSet.includes(v.key) || v.defaultValue));

  // Only connection fields change what a test proves. Choosing a folder or a
  // pick never does, so it must not send the owner back to Test connection.
  const connectionField = (k: string) => {
    const v = t.vars.find((x) => x.kind === "user" && x.key === k);
    return !!v && v.type !== "folder" && v.type !== "pick";
  };
  const set = (k: string, val: string) => setValues((m) => ({ ...m, [k]: val }));
  const setSecret = (k: string, val: string) => setSecrets((m) => ({ ...m, [k]: val }));

  function options() {
    const out: Record<string, string> = {};
    for (const v of t.vars) {
      if (v.kind !== "user") continue;
      if (v.secret) {
        if (secrets[v.key]) out[v.key] = secrets[v.key];
      } else if (values[v.key] !== undefined && values[v.key] !== "") out[v.key] = values[v.key];
    }
    return out;
  }

  // Sync the draft with what is on screen. Editing a saved drive never writes
  // here: its changes apply only on Save.
  const sync = useCallback(async (): Promise<Drive> => {
    const cur = draftRef.current;
    if (cur && !cur.draft) return cur;
    const d = await ui.saveDrive({ botId, id: cur?.id ?? "", template: t.key, draft: true, readOnly, options: options() });
    setDraft(d);
    draftRef.current = d;
    return d;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [botId, t.key, readOnly, values, secrets]);

  const reloadDraft = useCallback(async () => {
    const cur = draftRef.current;
    if (!cur) return;
    const r = await ui.listDrives({ botId, draftId: cur.draft ? cur.id : "" });
    const fresh = r.drives.find((x) => x.id === cur.id);
    if (fresh) {
      setDraft(fresh);
      setValues((m) => ({ ...fresh.options, ...m, ...Object.fromEntries(Object.entries(fresh.options).filter(([k]) => !m[k])) }));
    }
  }, [botId]);

  // The sign-in popup posts back when it is done; polling covers a lost message.
  useEffect(() => {
    if (!authBusy) return;
    const onMsg = (e: MessageEvent) => {
      const m = e.data as { silo?: string; ok?: boolean; message?: string };
      if (m?.silo !== "drive-auth") return;
      setAuthBusy(false);
      if (!m.ok) setAuthErr(m.message || "Sign-in did not finish.");
      void reloadDraft();
    };
    window.addEventListener("message", onMsg);
    const poll = setInterval(() => {
      void (async () => {
        const cur = draftRef.current;
        if (!cur) return;
        const r = await ui.listDrives({ botId, draftId: cur.draft ? cur.id : "" }).catch(() => null);
        const fresh = r?.drives.find((x) => x.id === cur.id);
        if (fresh && fresh.connected && fresh.account !== cur.account) {
          setAuthBusy(false);
          setDraft(fresh);
          setValues((m) => ({ ...fresh.options, ...m }));
        }
      })();
    }, 2000);
    return () => {
      window.removeEventListener("message", onMsg);
      clearInterval(poll);
    };
  }, [authBusy, botId, reloadDraft]);

  async function signIn() {
    setAuthErr("");
    // Open the window inside the click so popup blockers allow it, then point
    // it at the provider once the server has the URL.
    const w = window.open("about:blank", "silo-drive-auth", "width=520,height=720");
    try {
      const d = await sync();
      const r = await ui.beginDriveAuth({ id: d.id });
      if (w) {
        w.location.href = r.url;
        setAuthBusy(true);
      } else {
        setAuthErr("Your browser blocked the sign-in window. Allow pop-ups for Silo and try again.");
      }
    } catch (e) {
      w?.close();
      setAuthErr(fail(e));
    }
  }

  async function runTest() {
    testedSig.current = connSig;
    setTest({ state: "busy", msg: "" });
    try {
      const d = await sync();
      const r = await ui.browseDrive({ id: d.id, path: "" });
      const n = r.dirs.length;
      setTest({ state: "ok", msg: n === 0 ? "Connected. The drive is empty at the top level." : `Connected. Found ${n} folder${n === 1 ? "" : "s"}.` });
    } catch (e) {
      setTest({ state: "fail", msg: fail(e) });
    }
  }

  // Once the owner has tested, editing a connection field re-tests on its
  // own after a pause instead of asking for another click.
  const connSig = JSON.stringify([
    Object.entries(values).filter(([k]) => connectionField(k)).sort(),
    Object.entries(secrets).filter(([k]) => connectionField(k)).sort(),
  ]);
  const testedSig = useRef("");
  useEffect(() => {
    if (oauth || editing || test.state === "busy") return;
    if (test.state === "idle" || connSig === testedSig.current) return;
    if (requiredMissing.length > 0) {
      setTest({ state: "idle", msg: "" });
      return;
    }
    const timer = setTimeout(() => void runTest(), 900);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connSig]);

  const browse = useCallback(
    async (path: string) => {
      const d = await sync();
      return (await ui.browseDrive({ id: d.id, path })).dirs;
    },
    [sync],
  );

  const loaders = useRef<Record<string, () => Promise<DriveOption[]>>>({});
  function pickLoader(key: string) {
    if (!loaders.current[key]) {
      loaders.current[key] = async () => {
        const d = await sync();
        return (await ui.pickDriveOptions({ id: d.id, key })).options;
      };
    }
    return loaders.current[key];
  }
  // A new sign-in means new options.
  useEffect(() => {
    loaders.current = {};
  }, [draft?.account]);

  const nameOk = NAME_RE.test(name) && (!taken.has(name) || name === drive?.name);
  const canSave = connected && nameOk && saver.state !== "saving" && (oauth || requiredMissing.length === 0);

  async function save() {
    setErr("");
    try {
      await saver.run(async () => {
        const cur = draftRef.current;
        await ui.saveDrive({
          botId,
          id: cur?.id ?? "",
          template: t.key,
          name,
          readOnly,
          draft: false,
          options: options(),
        });
      });
      onDone();
    } catch (e) {
      setErr(fail(e));
    }
  }

  const pathPreview = `/workspace/drives/${name || "…"}`;

  return (
    <div className="silo-page">
      <PageHead title={editing ? drive!.name : t.title} subtitle={editing ? t.title : t.blurb} mark={<DriveMark svg={t.iconSvg} size={44} />} onBack={onDone} />

      {!editing && t.guide ? (
        <div className="mb-5 rounded-card border-l-4 border-cobalt bg-well px-4 py-3">
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-[0.08em] text-ink-3">Before you add</p>
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
          {connectVars.map((v) => (
            <VarInput
              key={v.key}
              v={v}
              value={v.secret ? secrets[v.key] ?? "" : values[v.key] ?? ""}
              onChange={(s) => (v.secret ? setSecret(v.key, s) : set(v.key, s))}
              isSet={draft?.secretsSet.includes(v.key)}
            />
          ))}
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
                {advancedVars.map((v) => (
                  <VarInput
                    key={v.key}
                    v={v}
                    value={v.secret ? secrets[v.key] ?? "" : values[v.key] ?? ""}
                    onChange={(s) => (v.secret ? setSecret(v.key, s) : set(v.key, s))}
                    isSet={draft?.secretsSet.includes(v.key)}
                  />
                ))}
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

function Gallery({ botId, templates, admin, onBack }: { botId: string; templates: DriveTemplate[]; admin: boolean; onBack: () => void }) {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const shown = useMemo(() => {
    const s = q.trim().toLowerCase();
    return s ? templates.filter((t) => `${t.title} ${t.blurb} ${CATEGORY_LABEL[t.category] ?? ""}`.toLowerCase().includes(s)) : templates;
  }, [q, templates]);
  const groups = Object.keys(CATEGORY_LABEL)
    .map((c) => ({ c, items: shown.filter((t) => t.category === c) }))
    .filter((g) => g.items.length > 0);

  return (
    <div className="silo-page">
      <PageHead title="Add a drive" subtitle="Mount cloud storage or a file server as a folder the Bot can use." onBack={onBack} />
      <div className="relative mb-6">
        <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-3" />
        <input autoFocus className={`${inputClass} pl-9`} placeholder="Search providers" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search providers" />
      </div>
      {groups.length === 0 ? <p className="text-[13px] text-ink-3">Nothing matches “{q}”.</p> : null}
      <div className="space-y-6">
        {groups.map(({ c, items }) => (
          <section key={c}>
            <h3 className="mb-2.5 text-[11px] font-medium uppercase tracking-[0.08em] text-ink-3">{CATEGORY_LABEL[c]}</h3>
            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
              {items.map((t) => {
                const tile = (
                  <button
                    key={t.key}
                    type="button"
                    aria-disabled={!t.available || undefined}
                    className={`group flex items-center gap-3.5 rounded-card bg-surface p-3.5 text-left shadow-card transition-[box-shadow,transform] duration-[160ms] ease-quiet ${
                      t.available || admin ? "hover:-translate-y-px hover:shadow-float active:scale-[.995]" : "cursor-default"
                    }`}
                    onClick={() => {
                      if (t.available) navigate(`/bots/${botId}/drives/new/${t.key}`);
                      else if (admin) navigate("/admin/drives");
                    }}
                  >
                    <DriveMark svg={t.iconSvg} size={40} muted={!t.available} />
                    <div className="min-w-0 flex-1">
                      <div className={`text-[14px] font-medium leading-5 ${t.available ? "text-ink" : "text-ink-3"}`}>{t.title}</div>
                      <div className="truncate text-[12.5px] leading-[18px] text-ink-3">{t.available ? t.blurb : "Needs admin setup"}</div>
                    </div>
                    {t.available ? <ChevronRight size={15} className="shrink-0 text-ink-3 transition-transform duration-[160ms] ease-quiet group-hover:translate-x-0.5" /> : null}
                  </button>
                );
                return t.available ? (
                  tile
                ) : (
                  <Tip
                    key={t.key}
                    content={
                      <>
                        <TipTitle>{t.title} is not set up yet</TipTitle>
                        {admin ? (
                          <>
                            <p className="mt-1 text-[12px] leading-[17px] text-ink-2">It needs an OAuth client registered with {t.authLabel || t.title}, once for everyone.</p>
                            <TipAction>Click to set it up in Admin → Drives</TipAction>
                          </>
                        ) : (
                          <p className="mt-1 text-[12px] leading-[17px] text-ink-2">An admin has to enable it once, in Admin → Drives.</p>
                        )}
                      </>
                    }
                  >
                    {tile}
                  </Tip>
                );
              })}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}

function DriveRow({ botId, d, t, onReconnect, onRemoved }: { botId: string; d: Drive; t?: DriveTemplate; onReconnect: () => void; onRemoved: () => void }) {
  const navigate = useNavigate();
  const look = driveLook(d);
  const attention = look.lamp === "needs_you";
  return (
    <li
      className={`relative flex flex-col gap-3 overflow-hidden rounded-card bg-surface p-4 shadow-card transition-shadow duration-[160ms] ease-quiet hover:shadow-float sm:flex-row sm:items-center ${
        attention ? "before:absolute before:inset-y-0 before:left-0 before:w-[2px] before:bg-vermilion" : ""
      }`}
    >
      <div className="flex min-w-0 flex-1 items-start gap-3.5">
        <DriveMark svg={t?.iconSvg} size={40} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[15px] font-semibold tracking-[-0.01em] text-ink">{d.name}</span>
            <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">{t?.title ?? d.template}</span>
            {d.readOnly ? <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">Read-only</span> : null}
          </div>
          <div className="mt-0.5 flex min-w-0 items-center gap-1.5">
            <code className="truncate font-mono text-[12px] text-ink-2">{d.path}</code>
            <CopyButton text={d.path} title="Copy path" size={12} />
          </div>
          <div className="mt-1.5 flex min-w-0 items-center gap-1.5 text-[12.5px]">
            <Lamp status={look.lamp} />
            <span className={`shrink-0 font-medium ${look.tone}`}>{look.word}</span>
            {d.account && d.state === "mounted" ? <span className="truncate text-ink-3">· {d.account}</span> : null}
            {d.stateDetail && d.state !== "mounted" ? (
              <Tip content={<p className="max-w-[320px] break-words font-mono text-[12px]">{d.stateDetail}</p>}>
                <span tabIndex={0} className="min-w-0 truncate text-ink-3 outline-none">
                  · {d.stateDetail}
                </span>
              </Tip>
            ) : null}
          </div>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1 self-end sm:self-center">
        {d.state === "needs_auth" ? (
          <Btn kind="primary" size="sm" type="button" onClick={onReconnect}>
            <RefreshCw size={13} className="breathe" />
            Reconnect
          </Btn>
        ) : d.state === "mounted" ? (
          <Btn kind="ghost" size="sm" type="button" onClick={() => navigate(`/bots/${botId}/files?open=${encodeURIComponent(`drives/${d.name}`)}`)}>
            <FolderOpen size={14} />
            Open in Files
          </Btn>
        ) : null}
        <Btn kind="ghost" size="sm" iconOnly type="button" aria-label={`Edit ${d.name}`} title="Edit" onClick={() => navigate(`/bots/${botId}/drives/${d.id}`)}>
          <Pencil size={14} />
        </Btn>
        <ArmedButton
          kind="ghost"
          size="sm"
          iconOnly
          title="Remove drive"
          armedLabel="Click again to remove"
          icon={<Trash2 size={14} />}
          onConfirm={async () => {
            await ui.deleteDrive({ id: d.id });
            onRemoved();
          }}
        />
      </div>
    </li>
  );
}

const POPULAR = ["gdrive", "onedrive", "dropbox", "box", "pcloud", "nextcloud", "s3", "r2", "b2", "sftp"];

export function BotDrives({ botId, sub, admin }: { botId: string; sub: string[]; admin: boolean }) {
  const navigate = useNavigate();
  const [templates, setTemplates] = useState<DriveTemplate[] | null>(null);
  const [drives, setDrives] = useState<Drive[] | null>(null);
  const [bindOk, setBindOk] = useState(true);
  const [err, setErr] = useState("");
  const back = useCallback(() => navigate(`/bots/${botId}/drives`), [botId, navigate]);

  const refresh = useCallback(async () => {
    try {
      const [ts, ds] = await Promise.all([ui.listDriveTemplates({}), ui.listDrives({ botId, draftId: "" })]);
      setTemplates(ts.templates);
      setDrives(ds.drives);
      setBindOk(ds.bindOk);
      setErr("");
    } catch (e) {
      setErr(fail(e));
    }
  }, [botId]);

  // Poll quickly while something is connecting, slowly otherwise.
  const busy = drives?.some((d) => d.state === "mounting") ?? false;
  useEffect(() => {
    void refresh();
    const t = setInterval(() => void refresh(), busy ? 1500 : 6000);
    return () => clearInterval(t);
  }, [refresh, busy]);

  const byKey = useMemo(() => new Map((templates ?? []).map((t) => [t.key, t])), [templates]);
  const taken = useMemo(() => new Set((drives ?? []).map((d) => d.name)), [drives]);

  async function reconnect(d: Drive) {
    const w = window.open("about:blank", "silo-drive-auth", "width=520,height=720");
    try {
      const r = await ui.beginDriveAuth({ id: d.id });
      if (w) w.location.href = r.url;
    } catch (e) {
      w?.close();
      setErr(fail(e));
    }
  }
  useEffect(() => {
    const onMsg = (e: MessageEvent) => {
      if ((e.data as { silo?: string })?.silo === "drive-auth") void refresh();
    };
    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
  }, [refresh]);

  const segs = sub ?? [];
  if (segs[0] === "new" && segs.length === 1) {
    return templates ? <Gallery botId={botId} templates={templates} admin={admin} onBack={back} /> : <Loading onBack={back} />;
  }
  if (segs[0] === "new" && segs[1]) {
    const t = byKey.get(segs[1]);
    if (!templates || !drives) return <Loading onBack={back} />;
    if (!t) return <Missing onBack={back} />;
    return <DriveForm key={t.key} botId={botId} t={t} taken={taken} onDone={back} />;
  }
  if (segs[0]) {
    if (!templates || !drives) return <Loading onBack={back} />;
    const d = drives.find((x) => x.id === segs[0]);
    const t = d && byKey.get(d.template);
    if (!d || !t) return <Missing onBack={back} />;
    return <DriveForm key={d.id} botId={botId} t={t} drive={d} taken={taken} onDone={back} />;
  }

  return (
    <div className="silo-page">
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h2 className="text-[22px] font-medium leading-7 tracking-[-0.015em] text-ink">Drives</h2>
          <p className="mt-1 max-w-[520px] text-[13.5px] leading-[21px] text-ink-2">
            Cloud storage and file servers, mounted as folders in <code className="font-mono text-[12.5px] text-ink">/workspace/drives</code>. The Bot works on them like any file. Sign-ins and passwords stay on the Silo server.
          </p>
        </div>
        {drives && drives.length > 0 ? (
          <button type="button" className={btnClass("primary")} onClick={() => navigate(`/bots/${botId}/drives/new`)}>
            <Plus size={15} />
            Add drive
          </button>
        ) : null}
      </div>

      {err ? (
        <div className="mb-4">
          <ErrorWell>{err}</ErrorWell>
        </div>
      ) : null}

      {!bindOk ? (
        <div className="mb-4 flex flex-wrap items-center gap-3 rounded-card bg-well px-4 py-3">
          <CircleAlert size={16} className="shrink-0 text-vermilion" />
          <p className="min-w-0 flex-1 text-[13px] leading-5 text-ink">This Bot’s container was made before it had drives. Reset it once so the Bot can see them. Its files are kept.</p>
          <Btn kind="secondary" size="sm" type="button" onClick={() => navigate(`/bots/${botId}/container`)}>
            Go to Containers
          </Btn>
        </div>
      ) : null}

      {drives === null || templates === null ? (
        <SkeletonRows rows={2} height={92} />
      ) : drives.length === 0 ? (
        <div className="rounded-card bg-surface p-6 shadow-card sm:p-8">
          <div className="mb-5 flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-sm bg-well text-ink-2">
              <HardDrive size={22} />
            </div>
            <div>
              <h3 className="text-[15px] font-semibold tracking-[-0.01em] text-ink">No drives yet</h3>
              <p className="text-[13px] text-ink-2">Give the Bot a folder from somewhere else.</p>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
            {POPULAR.map((k) => byKey.get(k))
              .filter((t): t is DriveTemplate => !!t)
              .map((t) => (
                <button
                  key={t.key}
                  type="button"
                  className="flex flex-col items-center gap-2 rounded-control bg-well px-2 pb-2.5 pt-3.5 text-center transition-[background-color,transform] duration-[160ms] ease-quiet hover:bg-pressed active:scale-[.97]"
                  onClick={() => navigate(t.available ? `/bots/${botId}/drives/new/${t.key}` : `/bots/${botId}/drives/new`)}
                >
                  <DriveMark svg={t.iconSvg} size={36} className="bg-surface" muted={!t.available} />
                  <span className={`w-full truncate text-[12.5px] font-medium ${t.available ? "text-ink" : "text-ink-3"}`}>{t.title}</span>
                </button>
              ))}
          </div>
          <button type="button" className="mt-4 flex items-center gap-1 text-[13px] font-medium text-cobalt hover:text-cobalt-deep" onClick={() => navigate(`/bots/${botId}/drives/new`)}>
            All {templates.length} providers <ChevronRight size={14} />
          </button>
        </div>
      ) : (
        <ul className="space-y-2.5">
          {drives.map((d) => (
            <DriveRow key={d.id} botId={botId} d={d} t={byKey.get(d.template)} onReconnect={() => void reconnect(d)} onRemoved={() => void refresh()} />
          ))}
        </ul>
      )}
    </div>
  );
}

function Loading({ onBack }: { onBack: () => void }) {
  return (
    <div className="silo-page">
      <PageHead title="Drives" onBack={onBack} />
      <SkeletonRows rows={3} height={64} />
    </div>
  );
}

function Missing({ onBack }: { onBack: () => void }) {
  return (
    <div className="silo-page">
      <PageHead title="Drives" onBack={onBack} />
      <p className="text-[13.5px] text-ink-2">That drive is gone. It may have been removed.</p>
    </div>
  );
}

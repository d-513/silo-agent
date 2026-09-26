import { Plus, RotateCcw, Trash2, Upload } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ui } from "./api";
import { ArmedButton } from "./Feedback";
import { SkeletonRows } from "./Field";
import { Btn } from "./Btn";
import { Segmented } from "./ConnectorForm";
import { SkillBrowserOverlay } from "./FileBrowser";
import { Switch } from "./Switch";
import { skillSource } from "./fs";
import type { BotSkill, Skill } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function InstallField({
  scope,
  onDone,
}: {
  scope: "library" | "personal";
  onDone: () => void;
}) {
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [note, setNote] = useState("");
  const file = useRef<HTMLInputElement>(null);
  function report(r: { installed: string[]; skipped: string[] }) {
    const parts = [];
    if (r.installed.length) parts.push("installed " + r.installed.join(", "));
    if (r.skipped.length) parts.push("already had " + r.skipped.join(", "));
    setNote(parts.join("; ") || "nothing new");
    onDone();
  }
  async function go(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setNote("");
    setBusy(true);
    try {
      report(await ui.installSkill({ scope, url: url.trim() }));
      setUrl("");
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  async function upload(list: FileList | null) {
    const f = list?.[0];
    if (!f) return;
    setErr("");
    setNote("");
    if (f.size > 10 << 20) {
      setErr("zip is larger than 10 MB");
      return;
    }
    setBusy(true);
    try {
      report(await ui.installSkill({ scope, archive: new Uint8Array(await f.arrayBuffer()), filename: f.name }));
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="mb-4 flex flex-wrap items-start gap-2" onSubmit={(e) => void go(e)}>
      <input
        className="h-9 min-w-0 flex-1 rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-3 outline-none wide:min-w-[240px]"
        placeholder="GitHub URL or owner/repo"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
      />
      <Btn kind="primary" type="submit" disabled={busy || !url.trim()} icon={<Plus size={12} />}>
        {busy ? "Installing…" : "Install"}
      </Btn>
      <Btn kind="secondary" type="button" disabled={busy} icon={<Upload size={12} />} onClick={() => file.current?.click()}>
        Upload zip
      </Btn>
      <input
        ref={file}
        type="file"
        accept=".zip,.tgz,.tar.gz,application/zip"
        className="hidden"
        onChange={(e) => {
          void upload(e.target.files);
          e.target.value = "";
        }}
      />
      {err && <p className="w-full text-vermilion">{err}</p>}
      {note && <p className="w-full text-ink-2">{note}</p>}
    </form>
  );
}

export function SkillRows({
  rows,
  onRemove,
  onOpen,
}: {
  rows: Skill[];
  onRemove?: (name: string) => void;
  onOpen?: (s: Skill) => void;
}) {
  if (rows.length === 0) {
    return <p className="text-ink-2">No skills here yet.</p>;
  }
  return (
    <div className="flex flex-col gap-2">
      {rows.map((s) => (
        <div
          key={s.kind + s.name}
          className={`flex items-start gap-3 rounded-card shadow-card bg-surface px-3 py-3 ${onOpen ? "cursor-pointer hover:border-cobalt" : ""}`}
          onClick={() => onOpen?.(s)}
        >
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="font-medium">{s.name}</span>
              {s.seeded ? <span className="font-mono text-[11px] text-ink-3">catalog</span> : null}
            </div>
            <p className="text-ink-2">{s.description}</p>
            {s.source ? <p className="truncate font-mono text-[11px] text-ink-3">{s.source}</p> : null}
          </div>
          {onRemove && (
            <ArmedButton
              kind="ghost"
              size="sm"
              className="shrink-0"
              armedLabel="Click again to remove"
              icon={<Trash2 size={13} />}
              onConfirm={() => onRemove(s.name)}
            >
              Remove
            </ArmedButton>
          )}
        </div>
      ))}
    </div>
  );
}

function SkillPeek({ scope, name, onClose }: { scope: string; name: string; onClose: () => void }) {
  const source = useMemo(() => skillSource(scope, name), [scope, name]);
  return <SkillBrowserOverlay key={scope + name} source={source} onClose={onClose} />;
}

export function AdminSkills() {
  const [rows, setRows] = useState<Skill[] | null>(null);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState("");
  async function load() {
    const r = await ui.listSkills({ scope: "library" });
    setRows(r.skills);
  }
  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
  }, []);
  async function seed() {
    setErr("");
    try {
      await ui.seedSkills({});
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  async function remove(name: string) {
    setErr("");
    try {
      await ui.deleteSkill({ scope: "library", name });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  return (
    <div>
      <div className="mb-4 flex items-center justify-between gap-2">
        <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Skills Library</h2>
        <Btn kind="secondary" type="button" onClick={() => void seed()} icon={<RotateCcw size={12} />}>
          Re-add defaults
        </Btn>
      </div>
      <p className="mb-4 text-ink-2">Site skills every Bot can enable. Install from a GitHub URL, owner/repo, or a skill zip.</p>
      {err && <p className="mb-3 text-vermilion">{err}</p>}
      <InstallField scope="library" onDone={() => void load()} />
      {rows === null ? (
        <SkeletonRows rows={3} height={64} />
      ) : (
        <SkillRows rows={rows} onRemove={(n) => void remove(n)} onOpen={(s) => setOpen(s.name)} />
      )}
      {open ? <SkillPeek scope="library" name={open} onClose={() => setOpen("")} /> : null}
    </div>
  );
}

export function SkillHub() {
  const [scope, setScope] = useState<"personal" | "library">("personal");
  const [rows, setRows] = useState<Skill[] | null>(null);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState("");
  async function load(s = scope) {
    const r = await ui.listSkills({ scope: s });
    setRows(r.skills);
  }
  useEffect(() => {
    setRows(null);
    setOpen("");
    load(scope).catch((e) => setErr(fail(e)));
  }, [scope]);
  async function remove(name: string) {
    setErr("");
    try {
      await ui.deleteSkill({ scope: "personal", name });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  return (
    <div className="silo-page">
      <h1 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Skills</h1>
      <p className="mb-4 text-ink-2">Personal skills are yours. Library skills are site-wide; enable them on a Bot.</p>
      <div className="mb-4 w-full max-w-[280px]">
        <Segmented
          value={scope}
          onChange={(v) => setScope(v as "personal" | "library")}
          options={[
            { id: "personal", label: "Personal" },
            { id: "library", label: "Library" },
          ]}
        />
      </div>
      {err && <p className="mb-3 text-vermilion">{err}</p>}
      {scope === "personal" && <InstallField scope="personal" onDone={() => void load()} />}
      {rows === null ? (
        <SkeletonRows rows={3} height={64} />
      ) : (
        <SkillRows
          rows={rows}
          onRemove={scope === "personal" ? (n) => void remove(n) : undefined}
          onOpen={(s) => setOpen(s.name)}
        />
      )}
      {open ? <SkillPeek scope={scope} name={open} onClose={() => setOpen("")} /> : null}
    </div>
  );
}

export function BotSkills({ botId }: { botId: string }) {
  const [rows, setRows] = useState<BotSkill[] | null>(null);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState<BotSkill | null>(null);
  async function load() {
    const r = await ui.listBotSkills({ botId });
    setRows(r.skills);
  }
  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
  }, [botId]);
  async function toggle(s: BotSkill) {
    setErr("");
    try {
      await ui.setBotSkill({ botId, kind: s.kind, name: s.name, enabled: !s.enabled });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  const library = (rows ?? []).filter((s) => s.kind === "library");
  const personal = (rows ?? []).filter((s) => s.kind === "personal");
  return (
    <div className="silo-page">
      <h2 className="mb-2 text-[22px] leading-7 font-medium tracking-[-0.015em]">Skills</h2>
      <p className="mb-4 text-ink-2">Enabled skills show as name + description in the prompt. The Bot loads the rest with `skill`.</p>
      {err && <p className="mb-3 text-vermilion">{err}</p>}
      {rows === null ? (
        <SkeletonRows rows={3} height={64} />
      ) : (
        <>
          <Group title="Library" rows={library} onToggle={(s) => void toggle(s)} onOpen={setOpen} />
          <Group title="Personal" rows={personal} onToggle={(s) => void toggle(s)} onOpen={setOpen} />
        </>
      )}
      {open ? <SkillPeek scope={open.kind} name={open.name} onClose={() => setOpen(null)} /> : null}
    </div>
  );
}

function Group({
  title,
  rows,
  onToggle,
  onOpen,
}: {
  title: string;
  rows: BotSkill[];
  onToggle: (s: BotSkill) => void;
  onOpen: (s: BotSkill) => void;
}) {
  return (
    <div className="mb-6">
      <h3 className="mb-2 text-[12px] font-medium tracking-wide text-ink-3">{title}</h3>
      {rows.length === 0 ? (
        <p className="text-ink-2">None.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((s) => (
            <div
              key={s.kind + s.name}
              className="flex cursor-pointer items-start gap-3 rounded-card shadow-card bg-surface px-3 py-3 hover:shadow-float"
              onClick={() => onOpen(s)}
            >
              <div className="min-w-0 flex-1">
                <div className="font-medium">{s.name}</div>
                <p className="text-ink-2">{s.description}</p>
              </div>
              <Switch on={s.enabled} onChange={() => onToggle(s)} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

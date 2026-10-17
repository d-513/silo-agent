import { AppWindow, Check, ChevronLeft, ChevronRight, Download, Globe, History, Link2, Maximize2, Play, ScrollText } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { btnClass } from "./Btn";
import { fail } from "./errors";
import { CopyButton, useArmed } from "./Feedback";
import { extOf, kindLabel } from "./fileKind";
import type { ArtifactBlock, ArtifactType, ArtifactVersion } from "./fold";
import { fmtBytes } from "./format";
import { t } from "./i18n.ts";
import { Tip } from "./Tip";

export type { ArtifactBlock, ArtifactType, ArtifactVersion } from "./fold";

// What opens an artifact: its card, and the version the card shows.
export type Artifact = Omit<ArtifactBlock, "key" | "type"> & { version: number };

export function latestOf(versions: ArtifactVersion[]): ArtifactVersion {
  return versions[versions.length - 1];
}

export function versionOf(a: Pick<Artifact, "versions">, n: number): ArtifactVersion {
  return a.versions.find((v) => v.n === n) ?? latestOf(a.versions);
}

// A card from before versions has no id the server knows.
export const isLegacy = (artifactId: string) => artifactId.startsWith("path:");

export function downloadURL(botId: string, artifactId: string, n: number): string {
  const q = new URLSearchParams({ bot_id: botId, artifact_id: artifactId, v: String(n) });
  return `/artifacts/download?${q}`;
}

function downloadName(a: Artifact, v: ArtifactVersion) {
  const base = v.name || a.path.split("/").filter(Boolean).pop() || "artifact";
  return v.kind === "tree" ? `${base}.zip` : base;
}

export async function downloadArtifact(botId: string, a: Artifact): Promise<string> {
  const v = versionOf(a, a.version);
  try {
    const r = await fetch(downloadURL(botId, a.artifactId, v.n), { credentials: "include" });
    if (!r.ok) throw new Error((await r.text()).trim() || t("Download failed ({{status}})", { status: r.status }));
    const href = URL.createObjectURL(await r.blob());
    const el = document.createElement("a");
    el.href = href;
    el.download = downloadName(a, v);
    document.body.appendChild(el);
    el.click();
    el.remove();
    URL.revokeObjectURL(href);
    return "";
  } catch (e) {
    return e instanceof Error ? e.message : String(e);
  }
}

// Type badge for cards: the extension in a small well (PDFs are vermilion), a
// scroll for a skill, a window for an app.
export function TypeBadge({ name, kind }: { name: string; kind?: ArtifactType }) {
  if (kind === "skill" || kind === "app") {
    return (
      <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">
        {kind === "skill" ? <ScrollText size={18} /> : <AppWindow size={18} />}
      </span>
    );
  }
  const ext = extOf(name).toUpperCase().slice(0, 4) || "FILE";
  const pdf = ext === "PDF";
  return (
    <span
      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none font-semibold tracking-[0.04em] ${
        pdf ? "bg-vermilion-pale text-vermilion" : "bg-well text-ink-2"
      }`}
    >
      {ext}
    </span>
  );
}

const iconBtn =
  "flex h-8 w-8 shrink-0 items-center justify-center rounded-sm text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms] disabled:pointer-events-none disabled:opacity-35";

// VersionSwitch steps through a card's versions: ‹ v2 of 3 ›.
export function VersionSwitch({ versions, n, onPick, hatch }: { versions: ArtifactVersion[]; n: number; onPick: (n: number) => void; hatch?: boolean }) {
  if (versions.length < 2) return null;
  const at = Math.max(0, versions.findIndex((v) => v.n === n));
  const btn = hatch
    ? "flex h-7 w-7 items-center justify-center rounded-sm text-on-hatch/70 hover:bg-white/10 hover:text-on-hatch disabled:opacity-30"
    : "flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 hover:bg-well hover:text-ink disabled:opacity-30";
  return (
    <span className={`flex shrink-0 items-center font-mono text-[11.5px] ${hatch ? "text-on-hatch/70" : "text-ink-3"}`}>
      <button type="button" className={btn} disabled={at === 0} onClick={() => onPick(versions[at - 1].n)} title={t("Older version")} aria-label={t("Older version")}>
        <ChevronLeft size={14} />
      </button>
      <span className="min-w-[3.25rem] text-center">{t("v{{n}} of {{total}}", { n: versions[at].n, total: versions.length })}</span>
      <button type="button" className={btn} disabled={at === versions.length - 1} onClick={() => onPick(versions[at + 1].n)} title={t("Newer version")} aria-label={t("Newer version")}>
        <ChevronRight size={14} />
      </button>
    </span>
  );
}

// The subtitle under a card's title: what it is, how big, which version.
function subtitle(a: Artifact, v: ArtifactVersion): string {
  const parts: string[] = [];
  if (a.artifactType === "skill") parts.push(a.saved === v.n ? t("Skill · saved to your skills") : t("Skill"));
  else if (a.artifactType === "app") parts.push(t("App"));
  else parts.push(kindLabel(v.name || a.path));
  if (v.size) parts.push(fmtBytes(v.size));
  if (a.versions.length < 2 && v.n > 1) parts.push(`v${v.n}`);
  return parts.join(" · ");
}

// useShownVersion is the version a card shows: the newest until the human
// steps to another.
function useShownVersion(versions: ArtifactVersion[]) {
  const [picked, setPicked] = useState<number | null>(null);
  const n = picked ?? latestOf(versions).n;
  return [n, setPicked] as const;
}

export function ArtifactCard({
  botId,
  block,
  autoRun,
  onOpen,
  onSave,
}: {
  botId: string;
  block: ArtifactBlock;
  // The newest app in the thread runs without a click.
  autoRun?: boolean;
  onOpen: (a: Artifact) => void;
  onSave?: (a: Artifact) => void;
}) {
  const [n, setN] = useShownVersion(block.versions);
  const v = versionOf(block, n);
  const a: Artifact = { ...block, version: v.n };
  const [err, setErr] = useState("");
  const [sharing, setSharing] = useState(false);
  if (block.artifactType === "app") return <AppCard botId={botId} a={a} v={v} autoRun={autoRun} onPick={setN} onOpen={onOpen} />;

  const skill = block.artifactType === "skill";
  const saved = skill && block.saved === v.n;
  const legacy = isLegacy(block.artifactId);
  return (
    <div className="group w-full max-w-[420px] overflow-hidden rounded-card bg-surface shadow-card transition-[box-shadow,transform] duration-[200ms] ease-quiet hover:-translate-y-px hover:shadow-float motion-reduce:hover:translate-y-0">
      <div className="flex items-center gap-3 p-3">
        <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={() => onOpen(a)} disabled={legacy}>
          <TypeBadge name={v.name || block.path} kind={block.artifactType} />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[13.5px] leading-5 font-medium text-ink">{block.title}</span>
            <span className="flex items-center gap-1 truncate text-[12.5px] leading-[18px] text-ink-3">
              {subtitle(a, v)}
              <LiveNote v={v} />
            </span>
          </span>
        </button>
        <VersionSwitch versions={block.versions} n={v.n} onPick={setN} />
        {!skill && !legacy ? <ShareButton on={sharing} onToggle={() => setSharing((x) => !x)} /> : null}
        <button
          type="button"
          className={iconBtn}
          title={t("Download")}
          aria-label={t("Download")}
          disabled={legacy}
          onClick={(e) => {
            e.stopPropagation();
            void downloadArtifact(botId, a).then(setErr);
          }}
        >
          <Download size={16} />
        </button>
        {skill && !saved && onSave && !legacy ? (
          <button type="button" className={btnClass("primary", "", "sm")} onClick={() => onSave(a)}>
            {t("Save skill")}
          </button>
        ) : null}
        {saved ? (
          <span className="pop flex h-8 w-8 shrink-0 items-center justify-center text-emerald" title={t("Saved")}>
            <Check size={17} strokeWidth={2.25} />
          </span>
        ) : null}
      </div>
      {v.preview?.image || v.preview?.text ? (
        <button type="button" className="block w-full text-left" onClick={() => onOpen(a)} disabled={legacy}>
          <Preview v={v} skill={skill} />
        </button>
      ) : null}
      {sharing ? <SharePanel botId={botId} artifactId={a.artifactId} n={v.n} /> : null}
      {err ? <p className="px-3 pb-2.5 text-[12.5px] text-vermilion">{err}</p> : null}
    </div>
  );
}

function Preview({ v, skill }: { v: ArtifactVersion; skill: boolean }) {
  const p = v.preview!;
  if (p.image) {
    return (
      <div className="flex max-h-56 justify-center overflow-hidden bg-well">
        <img src={p.image} alt="" className="max-h-56 w-full object-cover object-top" />
      </div>
    );
  }
  if (skill) return <p className="line-clamp-3 px-3 pb-3 text-[13px] leading-5 text-ink-2">{p.text}</p>;
  return (
    <pre className="mx-3 mb-3 max-h-36 overflow-hidden rounded-sm bg-well px-3 py-2 font-mono text-[11.5px] leading-[17px] whitespace-pre-wrap break-words text-ink-2">
      {p.text}
      {p.more ? "\n…" : ""}
    </pre>
  );
}

// LiveNote marks a version too large to keep: the card shows the live file.
function LiveNote({ v }: { v: ArtifactVersion }) {
  if (v.pinned) return null;
  return (
    <Tip content={<span className="text-[12.5px]">{t("This version was too large to keep ({{why}}), so the card shows the file as it is now.", { why: v.note || "" })}</span>}>
      <span className="rounded-xs bg-well px-1.5 text-[11px] font-medium text-ink-2">{t("live file")}</span>
    </Tip>
  );
}

// --- apps -------------------------------------------------------------------

export const APP_SANDBOX = "allow-scripts allow-forms allow-popups allow-modals allow-downloads allow-pointer-lock";

// AppFrame runs one app version: the owner's pass to it is minted on the
// content host, and the page there is sandboxed by its own headers too.
export function AppFrame({ botId, artifactId, n, title, className }: { botId: string; artifactId: string; n: number; title: string; className: string }) {
  const [url, setUrl] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    setUrl("");
    setErr("");
    ui.openArtifactApp({ botId, artifactId, version: n })
      .then((r) => !dead && setUrl(r.url))
      .catch((e) => !dead && setErr(fail(e)));
    return () => {
      dead = true;
    };
  }, [botId, artifactId, n]);
  if (err) return <div className={`flex items-center justify-center bg-well p-6 text-center text-[13px] text-ink-2 ${className}`}>{err}</div>;
  if (!url) return <div className={`skeleton ${className}`} />;
  return <iframe key={url} src={url} title={title} sandbox={APP_SANDBOX} referrerPolicy="no-referrer" className={`block w-full border-0 bg-surface ${className}`} />;
}

function AppCard({
  botId,
  a,
  v,
  autoRun,
  onPick,
  onOpen,
}: {
  botId: string;
  a: Artifact;
  v: ArtifactVersion;
  autoRun?: boolean;
  onPick: (n: number) => void;
  onOpen: (a: Artifact) => void;
}) {
  const [run, setRun] = useState(!!autoRun);
  const [err, setErr] = useState("");
  const [sharing, setSharing] = useState(false);
  const legacy = isLegacy(a.artifactId);
  return (
    <div className="w-full overflow-hidden rounded-card bg-surface shadow-card">
      <div className="flex items-center gap-3 p-3">
        <TypeBadge name={v.name} kind="app" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13.5px] leading-5 font-medium text-ink">{a.title}</span>
          <span className="flex items-center gap-1.5 truncate text-[12.5px] leading-[18px] text-ink-3">
            {subtitle(a, v)}
            {v.network ? (
              <Tip content={<span className="text-[12.5px]">{t("This app may load from and talk to the internet.")}</span>}>
                <span className="inline-flex items-center gap-1 rounded-xs bg-well px-1.5 text-[11px] font-medium text-ink-2">
                  <Globe size={11} />
                  {t("Network")}
                </span>
              </Tip>
            ) : null}
            <LiveNote v={v} />
          </span>
        </span>
        <VersionSwitch versions={a.versions} n={v.n} onPick={onPick} />
        {!legacy ? <ShareButton on={sharing} onToggle={() => setSharing((x) => !x)} /> : null}
        <button type="button" className={iconBtn} title={t("Download")} aria-label={t("Download")} disabled={legacy} onClick={() => void downloadArtifact(botId, a).then(setErr)}>
          <Download size={16} />
        </button>
        <button type="button" className={iconBtn} title={t("Open full screen")} aria-label={t("Open full screen")} disabled={legacy} onClick={() => onOpen(a)}>
          <Maximize2 size={15} />
        </button>
      </div>
      {sharing ? <SharePanel botId={botId} artifactId={a.artifactId} n={v.n} /> : null}
      {!v.pinned || legacy ? (
        <p className="border-t border-line px-4 py-6 text-center text-[13px] text-ink-2">{t("This app was too large to keep, so it cannot run here. Download it instead.")}</p>
      ) : run ? (
        <AppFrame botId={botId} artifactId={a.artifactId} n={v.n} title={a.title} className="h-[440px] border-t border-line" />
      ) : (
        <button
          type="button"
          onClick={() => setRun(true)}
          className="flex h-40 w-full items-center justify-center gap-2 border-t border-line bg-well text-[13.5px] font-medium text-ink transition-colors duration-[160ms] hover:bg-pressed"
        >
          <Play size={15} />
          {t("Run app")}
        </button>
      )}
      {err ? <p className="px-3 py-2 text-[12.5px] text-vermilion">{err}</p> : null}
    </div>
  );
}

// --- shared links -----------------------------------------------------------

function ShareButton({ on, onToggle }: { on: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      className={`${iconBtn} ${on ? "bg-well text-ink" : ""}`}
      title={t("Share a public link")}
      aria-label={t("Share a public link")}
      aria-pressed={on}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
    >
      <Link2 size={15} />
    </button>
  );
}

type Share = { id: string; url: string; version: number };

// SharePanel makes, shows and removes the public link to one version.
export function SharePanel({ botId, artifactId, n, hatch }: { botId: string; artifactId: string; n: number; hatch?: boolean }) {
  const [share, setShare] = useState<Share | null | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const del = useArmed();
  useEffect(() => {
    let dead = false;
    setShare(undefined);
    ui.listArtifactShares({ botId, artifactId })
      .then((r) => {
        if (dead) return;
        const s = r.shares.find((x) => x.version === n);
        setShare(s ? { id: s.id, url: s.url, version: s.version } : null);
        if (!r.available) setErr(t("Public links need a content host: the operator sets content.host."));
      })
      .catch((e) => !dead && setErr(fail(e)));
    return () => {
      dead = true;
    };
  }, [botId, artifactId, n]);
  const make = () => {
    setBusy(true);
    setErr("");
    ui.shareArtifact({ botId, artifactId, version: n })
      .then((s) => setShare({ id: s.id, url: s.url, version: s.version }))
      .catch((e) => setErr(fail(e)))
      .finally(() => setBusy(false));
  };
  const remove = () =>
    del.fire(() => {
      if (!share) return;
      ui.deleteArtifactShare({ botId, id: share.id })
        .then(() => setShare(null))
        .catch((e) => setErr(fail(e)));
    });
  const tone = hatch ? "text-on-hatch" : "text-ink";
  return (
    <div className={`border-t px-3 py-2.5 text-[13px] ${hatch ? "border-white/10" : "border-line bg-canvas"} ${tone}`}>
      {share === undefined && !err ? <div className="skeleton h-6 rounded-xs" /> : null}
      {share === null && !err ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className={`min-w-0 flex-1 ${hatch ? "text-on-hatch/70" : "text-ink-2"}`}>{t("Anyone with the link can open version {{n}}, even while the Bot is stopped.", { n })}</span>
          <button type="button" className={btnClass("primary", "", "sm")} disabled={busy} onClick={make}>
            {t("Create link")}
          </button>
        </div>
      ) : null}
      {share ? (
        <div className="flex flex-wrap items-center gap-2">
          <a href={share.url} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate font-mono text-[12.5px] underline-offset-2 hover:underline">
            {share.url}
          </a>
          <CopyButton text={share.url} title={t("Copy link")} size={13} />
          <button type="button" onClick={remove} className={btnClass(del.armed ? "deny" : "ghost", "", "sm")}>
            {del.armed ? t("Click again to remove") : t("Remove link")}
          </button>
        </div>
      ) : null}
      {err ? <p className="text-[12.5px] text-vermilion">{err}</p> : null}
    </div>
  );
}

// ArtifactUpdateRow marks where a card further up gained a version.
export function ArtifactUpdateRow({ title, version }: { title: string; version: number }) {
  return (
    <div className="flex h-8 items-center gap-2 text-[13px] text-ink-3">
      <History size={14} className="shrink-0" />
      <span className="min-w-0 truncate">{t("Updated {{title}} to version {{n}}", { title, n: version })}</span>
    </div>
  );
}

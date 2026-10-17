import { AppWindow, Download, FolderTree, GitCompare, Link2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ui } from "./api";
import { AppFrame, downloadArtifact, SharePanel, VersionSwitch, versionOf, type Artifact } from "./Artifact";
import { Btn } from "./Btn";
import { fail } from "./errors";
import { isTextKind, kindOf } from "./fileKind";
import { FileBrowser, TypeIcon } from "./FileBrowser";
import { FilePreview } from "./FilePreview";
import { artifactSource } from "./fs";
import { Highlighted } from "./Highlighted";
import { t } from "./i18n.ts";
import { lineDiff } from "./lineDiff";

const hatchBtn = "flex h-8 w-8 shrink-0 items-center justify-center rounded-sm text-on-hatch/70 transition-colors duration-[160ms] hover:bg-white/10 hover:text-on-hatch";
const hatchToggle = (on: boolean) =>
  `flex h-8 items-center gap-1.5 rounded-sm px-2.5 text-[12.5px] font-medium transition-colors duration-[160ms] ${on ? "bg-white/15 text-on-hatch" : "text-on-hatch/70 hover:bg-white/10 hover:text-on-hatch"}`;

// The full-screen inspector for an artifact card, at one of its versions: a
// file's preview (and what changed since the version before), a skill's
// files, or an app running full size with its source a click away. It is its
// own chunk, loaded on first open.
export function ArtifactOverlay({
  botId,
  artifact,
  onSave,
  onClose,
}: {
  botId: string;
  artifact: Artifact;
  onSave?: (a: Artifact) => void;
  onClose: () => void;
}) {
  const [n, setN] = useState(artifact.version);
  const v = versionOf(artifact, n);
  const a: Artifact = { ...artifact, version: v.n };
  const app = artifact.artifactType === "app";
  const tree = v.kind === "tree";
  const pdf = !tree && kindOf(v.name || artifact.path) === "pdf";
  const [mode, setMode] = useState<"app" | "files">(app && v.pinned ? "app" : "files");
  const [diff, setDiff] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [err, setErr] = useState("");
  const prev = artifact.versions[artifact.versions.findIndex((x) => x.n === v.n) - 1];
  const canDiff = !tree && !!prev && isTextKind(v.name || artifact.path);
  const source = useMemo(() => artifactSource(botId, artifact.artifactId, v.n, artifact.path), [botId, artifact.artifactId, artifact.path, v.n]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const skillPending = artifact.artifactType === "skill" && artifact.saved !== v.n && !!onSave;
  const header = (
    <>
      <VersionSwitch versions={artifact.versions} n={v.n} onPick={setN} hatch />
      {app ? (
        <span className="flex items-center gap-0.5">
          <button type="button" className={hatchToggle(mode === "app")} onClick={() => setMode("app")} disabled={!v.pinned}>
            <AppWindow size={14} />
            {t("App")}
          </button>
          <button type="button" className={hatchToggle(mode === "files")} onClick={() => setMode("files")}>
            <FolderTree size={14} />
            {t("Files")}
          </button>
        </span>
      ) : null}
      {canDiff ? (
        <button type="button" className={hatchToggle(diff)} onClick={() => setDiff((x) => !x)} aria-pressed={diff}>
          <GitCompare size={14} />
          {t("Changes since v{{n}}", { n: prev.n })}
        </button>
      ) : null}
      {skillPending ? (
        <Btn kind="primary" type="button" className="h-8" onClick={() => onSave?.(a)}>
          {t("Save skill")}
        </Btn>
      ) : null}
      {artifact.artifactType !== "skill" ? (
        <button type="button" className={`${hatchBtn} ${sharing ? "bg-white/15 text-on-hatch" : ""}`} title={t("Share a public link")} aria-label={t("Share a public link")} onClick={() => setSharing((x) => !x)}>
          <Link2 size={15} />
        </button>
      ) : null}
      <button type="button" className={hatchBtn} title={t("Download")} aria-label={t("Download")} onClick={() => void downloadArtifact(botId, a).then(setErr)}>
        <Download size={16} />
      </button>
      <button type="button" className={hatchBtn} title={t("Close")} aria-label={t("Close")} onClick={onClose}>
        <X size={16} />
      </button>
    </>
  );

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-hatch p-0 wide:p-3">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-none bg-hatch ring-1 ring-white/10 wide:rounded-[10px]">
        <div className="flex shrink-0 items-center gap-2 border-b border-white/10 px-3 py-2 text-on-hatch">
          <span className="text-on-hatch/70">
            <TypeIcon name={v.name || artifact.path} dir={tree && !app} size={18} />
          </span>
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{artifact.title}</span>
          {header}
        </div>
        {sharing ? <SharePanel botId={botId} artifactId={artifact.artifactId} n={v.n} hatch /> : null}
        {err ? <p className="border-b border-white/10 px-3 py-1.5 text-[12.5px] text-vermilion">{err}</p> : null}
        <div className="flex min-h-0 flex-1 flex-col">
          {app && mode === "app" ? (
            <AppFrame key={v.n} botId={botId} artifactId={artifact.artifactId} n={v.n} title={artifact.title} className="h-full flex-1" />
          ) : tree ? (
            <FileBrowser key={v.n} source={source} chrome="hatch" initialFile={app ? v.entry || "index.html" : "SKILL.md"} />
          ) : (
            <div className={`flex min-h-0 flex-1 flex-col bg-surface text-ink ${pdf ? "overflow-hidden" : "overflow-auto p-4"}`}>
              <FileVersion botId={botId} a={a} diffWith={diff && prev ? prev.n : 0} full />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

type Loaded = { name: string; data: Uint8Array };

function useVersionFile(botId: string, artifactId: string, n: number) {
  const [file, setFile] = useState<Loaded | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!n) return;
    let dead = false;
    setFile(null);
    setErr("");
    ui.readArtifactFile({ botId, artifactId, version: n })
      .then((r) => !dead && setFile({ name: r.name, data: r.data }))
      .catch((e) => !dead && setErr(fail(e)));
    return () => {
      dead = true;
    };
  }, [botId, artifactId, n]);
  return { file, err };
}

// FileVersion is one version of a file artifact, or what changed in it since
// the version diffWith.
function FileVersion({ botId, a, diffWith, full }: { botId: string; a: Artifact; diffWith: number; full?: boolean }) {
  const { file, err } = useVersionFile(botId, a.artifactId, a.version);
  const before = useVersionFile(botId, a.artifactId, diffWith);
  const pdf = kindOf(file?.name || a.path) === "pdf";
  if (err) return <p className={`${pdf ? "p-4 " : ""}text-[13px] text-vermilion`}>{err}</p>;
  if (!file) return <div className={pdf ? "skeleton min-h-0 flex-1" : "skeleton h-40 rounded-sm"} />;
  const name = file.name || a.path;
  const text = isTextKind(name);
  if (diffWith) {
    if (before.err) return <p className="text-[13px] text-vermilion">{before.err}</p>;
    if (!before.file) return <div className="skeleton h-40 rounded-sm" />;
    const d = lineDiff(new TextDecoder().decode(before.file.data), new TextDecoder().decode(file.data));
    return (
      <div className="space-y-2">
        <p className="font-mono text-[12px] text-ink-3">
          <span className="text-emerald">+{d.stat.added}</span> <span className="text-vermilion">{"\u2212"}{d.stat.removed}</span>
        </p>
        {d.text ? (
          <Highlighted code={d.text} lang="diff" preClass="whitespace-pre-wrap break-words rounded-sm bg-well p-3 font-mono text-[12.5px] leading-5" />
        ) : (
          <p className="text-ink-2">{t("No changes.")}</p>
        )}
      </div>
    );
  }
  return (
    <FilePreview
      name={name}
      content={text ? new TextDecoder().decode(file.data) : ""}
      data={file.data}
      binary={!text}
      full={full}
      quote={{ source: "artifact", ref: `${a.artifactId}@${a.version}`, label: `${a.path} \u00b7 v${a.version}` }}
    />
  );
}

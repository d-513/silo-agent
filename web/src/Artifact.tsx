import { useEffect, useMemo, useState } from "react";
import { Check, Download, LoaderCircle, ScrollText, X } from "lucide-react";
import { btnClass } from "./Btn";
import { FilePreview } from "./FilePreview";
import { SkillBrowserOverlay, TypeIcon } from "./FileBrowser";
import { extOf, isTextKind, kindLabel } from "./fileKind";
import { fmtSize, skillSource, workspaceDirSource, type FsSource } from "./fs";

export type Artifact = {
  type: "skill" | "file";
  name: string;
  title: string;
  path: string;
  scope: string;
  status: string;
  size?: number;
  approvalId: string;
  runId?: string;
};

export function parseArtifact(body: string): Artifact | null {
  try {
    const v = JSON.parse(body) as Record<string, unknown>;
    const type = v.type === "file" ? "file" : v.type === "skill" ? "skill" : null;
    if (!type) return null;
    return {
      type,
      name: String(v.name || ""),
      title: String(v.title || v.name || (type === "skill" ? "Skill" : "File")),
      path: String(v.path || ""),
      scope: String(v.scope || ""),
      status: String(v.status || ""),
      size: typeof v.size === "number" ? v.size : undefined,
      approvalId: String(v.approval_id || ""),
      runId: typeof v.run_id === "string" ? v.run_id : undefined,
    };
  } catch {
    return null;
  }
}

export function artifactSource(botId: string, a: Artifact): FsSource {
  if (a.status === "pending" || a.scope === "workspace") {
    return workspaceDirSource(botId, a.path);
  }
  return skillSource(a.scope || "personal", a.name);
}

// Type badge for file cards: the extension in a small well; PDFs are
// vermilion-pale with a vermilion label. Skills show a scroll mark.
export function TypeBadge({ name, skill }: { name: string; skill?: boolean }) {
  if (skill) {
    return (
      <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">
        <ScrollText size={18} />
      </span>
    );
  }
  const ext = extOf(name).toUpperCase().slice(0, 4) || "FILE";
  const pdf = ext === "PDF";
  return (
    <span
      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none font-semibold tracking-[0.04em] ${ pdf ? "bg-vermilion-pale text-vermilion" : "bg-well text-ink-2"
      }`}
    >
      {ext}
    </span>
  );
}

export function ArtifactCard({
  artifact,
  onOpen,
  onSave,
  onDownload,
}: {
  artifact: Artifact;
  onOpen: () => void;
  onSave?: () => void;
  onDownload?: () => void;
}) {
  const skill = artifact.type === "skill";
  const pending = skill && artifact.status === "pending";
  const saved = skill && artifact.status === "saved";
  const name = artifact.name || artifact.path;
  return (
    <div className="group flex w-full max-w-[380px] items-center gap-3 rounded-card bg-surface p-3 shadow-card transition-[box-shadow,transform] duration-[200ms] ease-quiet hover:-translate-y-px hover:shadow-float motion-reduce:hover:translate-y-0">
      <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={onOpen}>
        <TypeBadge name={name} skill={skill} />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13.5px] leading-5 font-medium text-ink">{artifact.title || artifact.name}</span>
          <span className="block truncate text-[12.5px] leading-[18px] text-ink-3">
            {skill
              ? saved
                ? "Skill · saved to your skills"
                : "Skill"
              : `${kindLabel(name)}${artifact.size ? ` · ${fmtSize(artifact.size)}` : ""} · saved to Files`}
          </span>
        </span>
      </button>
      <button
        type="button"
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-sm text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms]"
        title="Download"
        aria-label="Download"
        onClick={(e) => {
          e.stopPropagation();
          onDownload?.();
        }}
      >
        <Download size={16} />
      </button>
      {pending && onSave ? (
        <button
          type="button"
          className={btnClass("primary", "", "sm")}
          onClick={(e) => {
            e.stopPropagation();
            onSave();
          }}
        >
          Save skill
        </button>
      ) : null}
      {saved ? (
        <span className="pop flex h-8 w-8 shrink-0 items-center justify-center text-emerald" title="Saved">
          <Check size={17} strokeWidth={2.25} />
        </span>
      ) : null}
    </div>
  );
}

function artifactName(a: Artifact) {
  const base = a.name || a.path.split("/").filter(Boolean).pop() || "file";
  return a.type === "skill" ? `${base}.zip` : base;
}

function qs(params: Record<string, string>) {
  return Object.entries(params)
    .filter(([, v]) => v !== "")
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join("&");
}

export function artifactURL(botId: string, a: Artifact): string {
  if (a.type === "skill") {
    if (a.status === "pending" || a.scope === "workspace") {
      return `/artifacts/skill.zip?${qs({ bot_id: botId, path: a.path })}`;
    }
    return `/artifacts/skill.zip?${qs({ scope: a.scope || "personal", name: a.name })}`;
  }
  return `/artifacts/file?${qs({ bot_id: botId, path: a.path })}`;
}

export async function downloadArtifact(botId: string, a: Artifact) {
  const url = artifactURL(botId, a);
  try {
    const r = await fetch(url, { credentials: "include" });
    if (!r.ok) throw new Error((await r.text()).trim() || `download failed (${r.status})`);
    const href = URL.createObjectURL(await r.blob());
    const el = document.createElement("a");
    el.href = href;
    el.download = artifactName(a);
    document.body.appendChild(el);
    el.click();
    el.remove();
    URL.revokeObjectURL(href);
  } catch (e) {
    console.error("artifact download failed", e);
  }
}

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
  const source = useMemo(
    () => artifactSource(botId, artifact),
    [botId, artifact.name, artifact.path, artifact.scope, artifact.status],
  );
  if (artifact.type === "file") {
    return <FileArtifactOverlay botId={botId} artifact={artifact} onClose={onClose} />;
  }
  return (
    <SkillBrowserOverlay
      key={`${artifact.scope}:${artifact.name}:${artifact.path}:${artifact.status}`}
      source={source}
      pending={artifact.status === "pending" && onSave ? { onSave: () => onSave(artifact) } : undefined}
      onClose={onClose}
    />
  );
}

function FileArtifactOverlay({ botId, artifact, onClose }: { botId: string; artifact: Artifact; onClose: () => void }) {
  const name = artifact.name || artifact.path.split("/").filter(Boolean).pop() || "file";
  const [data, setData] = useState<Uint8Array | null>(null);
  const [content, setContent] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    let dead = false;
    setErr("");
    setData(null);
    setContent("");
    const url = artifactURL(botId, artifact);
    fetch(`${url}&inline=1`, { credentials: "include" })
      .then(async (r) => {
        if (!r.ok) throw new Error(await r.text());
        return r.arrayBuffer();
      })
      .then((buf) => {
        if (dead) return;
        const bytes = new Uint8Array(buf);
        setData(bytes);
        if (isTextKind(name)) setContent(new TextDecoder().decode(bytes));
      })
      .catch((e) => {
        if (!dead) setErr(e instanceof Error ? e.message : "could not open file");
      });
    return () => {
      dead = true;
    };
  }, [botId, artifact.path, name]);

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-hatch p-0 wide:p-3">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-none bg-hatch ring-1 ring-white/10 wide:rounded-[10px]">
        <div className="flex shrink-0 items-center gap-2 border-b border-white/10 px-3 py-2 text-canvas">
          <span className="text-canvas/70">
            <TypeIcon name={name} dir={false} size={18} />
          </span>
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{artifact.title || name}</span>
          <button
            type="button"
            className="shrink-0 text-canvas/70 hover:text-canvas"
            title="Download"
            onClick={() => downloadArtifact(botId, artifact)}
          >
            <Download size={16} />
          </button>
          <button type="button" className="shrink-0 text-canvas/70 hover:text-canvas" title="Close" onClick={onClose}>
            <X size={16} />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-auto p-4">
          {err ? (
            <p className="text-[13px] text-vermilion">{err}</p>
          ) : !data ? (
            <div className="flex items-center gap-2 text-ink-3">
              <LoaderCircle size={14} className="spin" />
              <span className="text-[13px]">Opening…</span>
            </div>
          ) : (
            <FilePreview
              name={name}
              content={content}
              data={data}
              binary={!isTextKind(name)}
            />
          )}
        </div>
      </div>
    </div>
  );
}

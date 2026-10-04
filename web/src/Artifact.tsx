import { Check, Download, ScrollText } from "lucide-react";
import { btnClass } from "./Btn";
import { extOf, kindLabel } from "./fileKind";
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

import { useMemo } from "react";
import { Check, DownloadSimple, Scroll } from "@phosphor-icons/react";
import { SkillBrowserOverlay } from "./FileBrowser";
import { downloadFile } from "./FilePreview";
import { skillSource, workspaceDirSource, type FsSource } from "./fs";

export type SkillArtifact = {
  type: "skill";
  name: string;
  title: string;
  path: string;
  scope: string;
  approvalId: string;
  status: string;
  runId?: string;
};

export function parseArtifact(body: string): SkillArtifact | null {
  try {
    const v = JSON.parse(body) as Record<string, unknown>;
    if (v.type !== "skill") return null;
    return {
      type: "skill",
      name: String(v.name || ""),
      title: String(v.title || v.name || "Skill"),
      path: String(v.path || ""),
      scope: String(v.scope || ""),
      approvalId: String(v.approval_id || ""),
      status: String(v.status || ""),
      runId: typeof v.run_id === "string" ? v.run_id : undefined,
    };
  } catch {
    return null;
  }
}

export function artifactSource(botId: string, a: SkillArtifact): FsSource {
  if (a.status === "pending" || a.scope === "workspace") {
    return workspaceDirSource(botId, a.path);
  }
  return skillSource(a.scope || "personal", a.name);
}

export function ArtifactCard({
  artifact,
  onOpen,
  onSave,
  onDownload,
}: {
  artifact: SkillArtifact;
  onOpen: () => void;
  onSave?: () => void;
  onDownload?: () => void;
}) {
  const pending = artifact.status === "pending";
  const saved = artifact.status === "saved";
  return (
    <div className="flex max-w-[440px] items-center gap-3 rounded-[10px] bg-hatch px-3 py-2.5 text-plaster">
      <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={onOpen}>
        <Scroll size={22} className="shrink-0 text-plaster/70" />
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium">{artifact.title || artifact.name}</span>
          <span className="block text-[12px] text-plaster/50">Skill</span>
        </span>
      </button>
      <button
        type="button"
        className="shrink-0 text-plaster/60 hover:text-plaster"
        title="Download"
        onClick={(e) => {
          e.stopPropagation();
          onDownload?.();
        }}
      >
        <DownloadSimple size={16} />
      </button>
      {pending && onSave ? (
        <button
          type="button"
          className="h-8 shrink-0 rounded bg-bindery px-2.5 text-[13px] font-medium text-plaster hover:bg-bindery-deep"
          onClick={(e) => {
            e.stopPropagation();
            onSave();
          }}
        >
          Save skill
        </button>
      ) : null}
      {saved ? <Check size={16} className="shrink-0 text-pine" /> : null}
    </div>
  );
}

export async function downloadArtifact(botId: string, a: SkillArtifact) {
  const src = artifactSource(botId, a);
  const f = await src.read("SKILL.md");
  downloadFile(f.name || "SKILL.md", f.content, f.data);
}

export function ArtifactOverlay({
  botId,
  artifact,
  onSave,
  onClose,
}: {
  botId: string;
  artifact: SkillArtifact;
  onSave?: (a: SkillArtifact) => void;
  onClose: () => void;
}) {
  const source = useMemo(
    () => artifactSource(botId, artifact),
    [botId, artifact.name, artifact.path, artifact.scope, artifact.status],
  );
  return (
    <SkillBrowserOverlay
      key={`${artifact.scope}:${artifact.name}:${artifact.path}:${artifact.status}`}
      source={source}
      pending={artifact.status === "pending" && onSave ? { onSave: () => onSave(artifact) } : undefined}
      onClose={onClose}
    />
  );
}

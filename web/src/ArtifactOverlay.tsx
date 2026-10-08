import { Download, LoaderCircle, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { artifactSource, artifactURL, downloadArtifact, type Artifact } from "./Artifact";
import { isTextKind } from "./fileKind";
import { SkillBrowserOverlay, TypeIcon } from "./FileBrowser";
import { FilePreview } from "./FilePreview";

// The full-screen inspector for a skill or file card: a file tree for a skill
// directory, a preview for a single file. It is its own chunk, loaded on first
// open.
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
        <div className="flex shrink-0 items-center gap-2 border-b border-white/10 px-3 py-2 text-on-hatch">
          <span className="text-on-hatch/70">
            <TypeIcon name={name} dir={false} size={18} />
          </span>
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{artifact.title || name}</span>
          <button
            type="button"
            className="shrink-0 text-on-hatch/70 hover:text-on-hatch"
            title="Download"
            onClick={() => downloadArtifact(botId, artifact)}
          >
            <Download size={16} />
          </button>
          <button type="button" className="shrink-0 text-on-hatch/70 hover:text-on-hatch" title="Close" onClick={onClose}>
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

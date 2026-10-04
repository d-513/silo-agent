import { Download } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "../api";
import { TypeBadge } from "../Artifact";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { kindLabel } from "../fileKind";
import { downloadFile, FilePreview } from "../FilePreview";
import { fmtBytes } from "../format";

export function isBotScratch(path: string): boolean {
  const p = path.replace(/^\/+/, "").replace(/^(workspace\/)+/, "");
  return p === "bot" || p.startsWith("bot/");
}

type LoadedFile = { name: string; content: string; data?: Uint8Array; binary: boolean; truncated: boolean };

function useWorkspaceFile(botId: string, path: string) {
  const [file, setFile] = useState<LoadedFile | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    ui.readFile({ botId, path })
      .then((r) => {
        if (!dead) setFile({ name: r.name, content: r.content, data: r.data, binary: r.binary, truncated: r.truncated });
      })
      .catch((ex) => {
        if (!dead) setErr(fail(ex));
      });
    return () => {
      dead = true;
    };
  }, [botId, path]);
  return { file, err };
}

function fileSize(f: LoadedFile) {
  return f.data ? f.data.length : new TextEncoder().encode(f.content).length;
}

// What a workspace file shows while it loads, after it fails, and once it is read.
function PreviewBody({ file, err, className = "" }: { file: LoadedFile | null; err: string; className?: string }) {
  return (
    <div className={`space-y-2 ${className}`.trim()}>
      {file?.truncated ? <p className="text-[12.5px] text-ink-3">Showing the first 2 MB.</p> : null}
      {err ? <p className="text-[13px] text-vermilion">{err}</p> : null}
      {!file && !err ? <div className="skeleton h-40 rounded-sm" /> : null}
      {file ? <FilePreview name={file.name} content={file.content} data={file.data} binary={file.binary} /> : null}
    </div>
  );
}

// `present` of a bot/… path or a `look` screenshot: the preview inside a folded row.
export function QuietPreview({ botId, path }: { botId: string; path: string }) {
  const { file, err } = useWorkspaceFile(botId, path);
  return <PreviewBody file={file} err={err} />;
}

// `present` of a user-facing path shows the file itself in a card.
export function PresentFile({ botId, path }: { botId: string; path: string }) {
  const { file, err } = useWorkspaceFile(botId, path);
  const name = file?.name || path.split("/").filter(Boolean).pop() || path;
  return (
    <div className="max-w-full overflow-hidden rounded-card bg-surface shadow-card">
      <div className="flex items-center gap-3 px-3 py-2.5">
        <TypeBadge name={name} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13.5px] leading-5 font-medium text-ink">{name}</div>
          <div className="truncate text-[12.5px] leading-[18px] text-ink-3">
            {kindLabel(name)}
            {file ? ` · ${fmtBytes(fileSize(file))}` : ""} · in Files
          </div>
        </div>
        {file ? (
          <Btn
            kind="ghost"
            size="sm"
            iconOnly
            title="Download"
            aria-label="Download"
            icon={<Download size={15} />}
            onClick={() => downloadFile(file.name, file.content, file.data)}
          />
        ) : null}
      </div>
      <PreviewBody file={file} err={err} className="px-3 pb-3" />
    </div>
  );
}

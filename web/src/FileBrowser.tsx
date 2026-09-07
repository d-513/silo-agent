import {
  CaretRight,
  DownloadSimple,
  File,
  FileArchive,
  FileAudio,
  FileCode,
  FileCss,
  FileCsv,
  FileDoc,
  FileHtml,
  FileImage,
  FileJs,
  FilePdf,
  FilePy,
  FileText,
  FileTs,
  FileVideo,
  Folder,
  Trash,
  X,
} from "@phosphor-icons/react";
import { useEffect, useState, type ReactNode } from "react";
import { Btn } from "./Btn";
import { downloadFile, FilePreview } from "./FilePreview";
import { extOf, kindOf } from "./fileKind";
import { crumbs, fmtSize, type FsEntry, type FsFile, type FsSource } from "./fs";

export function TypeIcon({ name, dir }: { name: string; dir: boolean }) {
  const cls = "shrink-0 text-current opacity-70";
  if (dir) return <Folder size={16} className={cls} />;
  const e = extOf(name);
  const k = kindOf(name);
  if (k === "pdf") return <FilePdf size={16} className={cls} />;
  if (k === "image") return <FileImage size={16} className={cls} />;
  if (k === "video") return <FileVideo size={16} className={cls} />;
  if (k === "audio") return <FileAudio size={16} className={cls} />;
  if (k === "csv") return <FileCsv size={16} className={cls} />;
  if (k === "docx") return <FileDoc size={16} className={cls} />;
  if (e === "zip" || e === "gz" || e === "tgz" || e === "tar" || e === "7z") return <FileArchive size={16} className={cls} />;
  if (e === "py") return <FilePy size={16} className={cls} />;
  if (e === "ts" || e === "tsx") return <FileTs size={16} className={cls} />;
  if (e === "js" || e === "jsx") return <FileJs size={16} className={cls} />;
  if (e === "css") return <FileCss size={16} className={cls} />;
  if (e === "html" || e === "htm") return <FileHtml size={16} className={cls} />;
  if (k === "code" || k === "json") return <FileCode size={16} className={cls} />;
  if (k === "markdown" || k === "text") return <FileText size={16} className={cls} />;
  return <File size={16} className={cls} />;
}

export function FileBrowser({
  source,
  chrome = "folio",
  initialFile = "",
  headerLeft,
  headerRight,
  onSelect,
  onDelete,
  pendingDel,
  refreshKey = 0,
}: {
  source: FsSource;
  chrome?: "folio" | "hatch";
  initialFile?: string;
  headerLeft?: ReactNode;
  headerRight?: ReactNode;
  onSelect?: (path: string, dir: boolean) => void;
  onDelete?: (e: FsEntry) => void;
  pendingDel?: string;
  refreshKey?: number;
}) {
  const hatch = chrome === "hatch";
  const [kids, setKids] = useState<Record<string, FsEntry[]>>({});
  const [open, setOpen] = useState<Set<string>>(() => new Set([""]));
  const [sel, setSel] = useState(initialFile);
  const [file, setFile] = useState<FsFile | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function loadDir(path: string) {
    const ents = await source.list(path);
    setKids((m) => ({ ...m, [path]: ents }));
    return ents;
  }

  useEffect(() => {
    let dead = false;
    setErr("");
    loadDir("")
      .then(() => {
        if (!dead && initialFile) return openFile(initialFile);
      })
      .catch((e) => {
        if (!dead) setErr(fail(e));
      });
    return () => {
      dead = true;
    };
  }, [source.rootLabel, refreshKey]);

  async function openFile(path: string) {
    setBusy(true);
    setErr("");
    setSel(path);
    onSelect?.(path, false);
    try {
      const r = await source.read(path);
      setFile(r);
    } catch (ex) {
      setErr(fail(ex));
      setFile(null);
    } finally {
      setBusy(false);
    }
  }

  async function toggleDir(path: string) {
    setSel(path);
    onSelect?.(path, true);
    const next = new Set(open);
    if (path !== "") {
      if (next.has(path)) next.delete(path);
      else next.add(path);
    }
    setOpen(next);
    if (!kids[path]) {
      try {
        await loadDir(path);
      } catch (ex) {
        setErr(fail(ex));
      }
    }
  }

  const trail = crumbs(sel, source.rootLabel);

  function goCrumb(path: string, i: number) {
    if (i < trail.length - 1 || path === "") {
      setFile(null);
      void toggleDir(path);
      return;
    }
    if (file) return;
    void toggleDir(path);
  }
  const treeCls = hatch
    ? "min-h-0 w-[240px] shrink-0 overflow-auto border-r border-white/10 text-plaster max-wide:w-full max-wide:border-r-0"
    : "min-h-0 w-[240px] shrink-0 overflow-auto border-r border-thread text-iron max-wide:w-full max-wide:border-r-0";
  const headCls = hatch
    ? "flex h-10 shrink-0 items-center gap-2 border-b border-white/10 px-3 text-[13px] text-plaster"
    : "flex h-10 shrink-0 items-center gap-2 border-b border-thread-2 px-3 text-[13px]";

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className={headCls}>
        {headerLeft}
        <div className="silo-scroll-x flex min-w-0 flex-1 items-center gap-2">
          {trail.map((c, i) => (
            <span key={c.path || "root"} className="flex items-center gap-2">
              {i > 0 && <span className={hatch ? "text-plaster/40" : "text-thread"}>/</span>}
              <button
                type="button"
                className={i === trail.length - 1 ? (hatch ? "text-plaster" : "text-iron") : hatch ? "text-plaster/60 hover:text-plaster" : "text-stone hover:text-iron"}
                onClick={() => goCrumb(c.path, i)}
              >
                {c.label}
              </button>
            </span>
          ))}
        </div>
        {file && (
          <button
            type="button"
            className={hatch ? "text-plaster/70 hover:text-plaster" : "text-stone hover:text-iron"}
            title="Download"
            onClick={() => downloadFile(file.name, file.content, file.data)}
          >
            <DownloadSimple size={16} />
          </button>
        )}
        {headerRight}
      </div>
      {err && <p className={`px-4 py-2 ${hatch ? "text-carmine" : "text-carmine"}`}>{err}</p>}
      <div className="flex min-h-0 flex-1 overflow-hidden">
        <nav className={`${treeCls} ${file ? "max-wide:hidden" : ""}`}>
          <Tree
            path=""
            depth={0}
            kids={kids}
            open={open}
            sel={sel}
            hatch={hatch}
            pendingDel={pendingDel}
            onToggle={toggleDir}
            onFile={openFile}
            onDelete={onDelete}
          />
        </nav>
        <section className={`flex min-w-0 flex-1 flex-col bg-folio text-iron ${file ? "" : "max-wide:hidden"}`}>
          {file?.truncated && <p className="border-b border-thread-2 px-3 py-1 text-[12px] text-stone">Showing the first 2 MB.</p>}
          <div className="min-h-0 flex-1 overflow-auto p-3">
            {busy ? <p className="text-stone">Opening…</p> : null}
            {!busy && file ? <FilePreview name={file.name} content={file.content} data={file.data} binary={file.binary} /> : null}
            {!busy && !file && !err ? <p className="text-stone">Select a file.</p> : null}
          </div>
        </section>
      </div>
    </div>
  );
}

function Tree({
  path,
  depth,
  kids,
  open,
  sel,
  hatch,
  pendingDel,
  onToggle,
  onFile,
  onDelete,
}: {
  path: string;
  depth: number;
  kids: Record<string, FsEntry[]>;
  open: Set<string>;
  sel: string;
  hatch: boolean;
  pendingDel?: string;
  onToggle: (path: string) => void;
  onFile: (path: string) => void;
  onDelete?: (e: FsEntry) => void;
}) {
  const ents = kids[path];
  if (!ents) return depth === 0 ? <p className="px-3 py-4 text-stone">Loading…</p> : null;
  if (ents.length === 0 && depth === 0) return <p className="px-3 py-4 text-stone">This folder is empty.</p>;
  return (
    <ul>
      {ents.map((e) => {
        const expanded = e.dir && open.has(e.path);
        const active = sel === e.path;
        return (
          <li key={e.path}>
            <div
              className={`group flex h-8 items-center gap-1 pr-2 ${active ? (hatch ? "bg-white/10" : "bg-cloth") : hatch ? "hover:bg-white/5" : "hover:bg-cloth"}`}
              style={{ paddingLeft: 8 + depth * 12 }}
            >
              {e.dir ? (
                <button type="button" className="shrink-0 text-current opacity-50" onClick={() => onToggle(e.path)}>
                  <CaretRight size={12} className={expanded ? "rotate-90" : ""} />
                </button>
              ) : (
                <span className="inline-block w-3 shrink-0" />
              )}
              <button
                type="button"
                className="flex min-w-0 flex-1 items-center gap-2 text-left"
                onClick={() => (e.dir ? onToggle(e.path) : onFile(e.path))}
              >
                <TypeIcon name={e.name} dir={e.dir} />
                <span className="min-w-0 truncate text-[13px]">{e.name}</span>
                {!e.dir && <span className="ml-auto shrink-0 font-mono text-[11px] opacity-50">{fmtSize(e.size)}</span>}
              </button>
              {onDelete && (
                <button
                  type="button"
                  className={`shrink-0 opacity-0 group-hover:opacity-100 ${pendingDel === e.path ? "text-carmine opacity-100" : hatch ? "text-plaster/70 hover:text-carmine" : "text-stone hover:text-carmine"}`}
                  title={pendingDel === e.path ? "Click again to delete" : "Delete"}
                  onClick={() => onDelete(e)}
                >
                  {pendingDel === e.path ? "Delete?" : <Trash size={12} />}
                </button>
              )}
            </div>
            {expanded ? (
              <Tree
                path={e.path}
                depth={depth + 1}
                kids={kids}
                open={open}
                sel={sel}
                hatch={hatch}
                pendingDel={pendingDel}
                onToggle={onToggle}
                onFile={onFile}
                onDelete={onDelete}
              />
            ) : null}
          </li>
        );
      })}
    </ul>
  );
}

export function SkillBrowserOverlay({
  source,
  pending,
  onClose,
}: {
  source: FsSource;
  pending?: { onSave: () => void };
  onClose: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-hatch p-0 wide:p-3">
      <div className="flex min-h-0 flex-1 overflow-hidden rounded-none bg-hatch ring-1 ring-white/10 wide:rounded-[10px]">
        <FileBrowser
          source={source}
          chrome="hatch"
          initialFile="SKILL.md"
          headerRight={
            <>
              {pending && (
                <Btn kind="primary" type="button" className="h-8" onClick={pending.onSave}>
                  Save skill
                </Btn>
              )}
              <button type="button" className="text-plaster/70 hover:text-plaster" title="Close" onClick={onClose}>
                <X size={16} />
              </button>
            </>
          }
        />
      </div>
    </div>
  );
}

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

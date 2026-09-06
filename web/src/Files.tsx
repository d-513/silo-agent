import {
  ArrowLeft,
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
  FolderPlus,
  Plus,
  Trash,
  UploadSimple,
  X,
} from "@phosphor-icons/react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { NeedMachine } from "./NeedMachine";
import { downloadFile, FilePreview } from "./FilePreview";
import { extOf, kindOf } from "./fileKind";
import type { Bot, FileEntry } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function fmtSize(n: bigint | number) {
  const v = typeof n === "bigint" ? Number(n) : n;
  if (v < 1024) return `${v} B`;
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`;
  return `${(v / (1024 * 1024)).toFixed(1)} MB`;
}

function crumbs(path: string) {
  if (!path) return [{ label: "workspace", path: "" }];
  const parts = path.split("/").filter(Boolean);
  const out = [{ label: "workspace", path: "" }];
  let acc = "";
  for (const p of parts) {
    acc = acc ? `${acc}/${p}` : p;
    out.push({ label: p, path: acc });
  }
  return out;
}

function joinPath(dir: string, name: string) {
  const n = name
    .replaceAll("\\", "/")
    .split("/")
    .filter((p) => p && p !== "." && p !== "..")
    .join("/");
  if (!n) return "";
  return dir ? `${dir}/${n}` : n;
}

function TypeIcon({ name, dir }: { name: string; dir: boolean }) {
  const cls = "shrink-0 text-stone";
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

type OpenFile = {
  path: string;
  name: string;
  content: string;
  data?: Uint8Array;
  binary: boolean;
  truncated: boolean;
  size: bigint;
};

export function FilesPane({ bot, onStart }: { bot: Bot; onStart: () => void }) {
  const [path, setPath] = useState("");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [err, setErr] = useState("");
  const [file, setFile] = useState<OpenFile | null>(null);
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<"dir" | "file" | null>(null);
  const [draftName, setDraftName] = useState("");
  const [pendingDel, setPendingDel] = useState("");
  const upload = useRef<HTMLInputElement>(null);

  async function refresh() {
    const r = await ui.listFiles({ botId: bot.id, path });
    setEntries(r.entries);
  }

  useEffect(() => {
    if (!bot.workerConnected) return;
    let dead = false;
    setErr("");
    setFile(null);
    ui.listFiles({ botId: bot.id, path })
      .then((r) => {
        if (!dead) setEntries(r.entries);
      })
      .catch((e) => {
        if (!dead) {
          setEntries([]);
          setErr(fail(e));
        }
      });
    return () => {
      dead = true;
    };
  }, [bot.id, bot.workerConnected, path]);

  useEffect(() => {
    if (!pendingDel) return;
    const t = setTimeout(() => setPendingDel(""), 2500);
    return () => clearTimeout(t);
  }, [pendingDel]);

  if (!bot.workerConnected) {
    return (
      <NeedMachine
        copy="Start the Bot to browse /workspace."
        starting={bot.status === "starting"}
        onStart={onStart}
      />
    );
  }

  async function openFile(e: { path: string }) {
    setBusy(true);
    setErr("");
    try {
      const r = await ui.readFile({ botId: bot.id, path: e.path });
      setFile({
        path: e.path,
        name: r.name,
        content: r.content,
        data: r.data,
        binary: r.binary,
        truncated: r.truncated,
        size: r.size,
      });
    } catch (ex) {
      setErr(fail(ex));
      setFile(null);
    } finally {
      setBusy(false);
    }
  }

  async function make(e: FormEvent) {
    e.preventDefault();
    const dest = joinPath(path, draftName.trim());
    if (!dest || !draft) return;
    setErr("");
    try {
      if (draft === "dir") await ui.mkdir({ botId: bot.id, path: dest });
      else await ui.putFile({ botId: bot.id, path: dest, data: new Uint8Array() });
      const kind = draft;
      setDraft(null);
      setDraftName("");
      await refresh();
      if (kind === "file") {
        await openFile({ path: dest });
      }
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function onUpload(list: FileList | null) {
    if (!list?.length) return;
    setErr("");
    try {
      for (const f of list) {
        const dest = joinPath(path, f.name);
        if (!dest) continue;
        const buf = new Uint8Array(await f.arrayBuffer());
        if (buf.byteLength > 2 << 20) {
          setErr(`${f.name} is larger than 2 MB`);
          continue;
        }
        await ui.putFile({ botId: bot.id, path: dest, data: buf });
      }
      await refresh();
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function remove(e: FileEntry) {
    if (pendingDel !== e.path) {
      setPendingDel(e.path);
      return;
    }
    setPendingDel("");
    setErr("");
    try {
      await ui.removeFile({ botId: bot.id, path: e.path });
      if (file?.path === e.path || file?.path.startsWith(`${e.path}/`)) setFile(null);
      if (path === e.path || path.startsWith(`${e.path}/`)) setPath(crumbs(e.path).at(-2)?.path ?? "");
      else await refresh();
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  const trail = crumbs(path);
  const parent = trail.length > 1 ? trail[trail.length - 2].path : null;

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-10 shrink-0 items-center gap-2 border-b border-thread-2 px-3 text-[13px]">
          {parent !== null && (
            <button className="text-stone hover:text-iron" title="Up" onClick={() => setPath(parent)}>
              <ArrowLeft size={14} />
            </button>
          )}
          <div className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
            {trail.map((c, i) => (
              <span key={c.path || "root"} className="flex items-center gap-2">
                {i > 0 && <span className="text-thread">/</span>}
                <button className={i === trail.length - 1 ? "text-iron" : "text-stone hover:text-iron"} onClick={() => setPath(c.path)}>
                  {c.label}
                </button>
              </span>
            ))}
          </div>
          <button className="text-stone hover:text-iron" title="New folder" onClick={() => { setDraft("dir"); setDraftName(""); }}>
            <FolderPlus size={16} />
          </button>
          <button className="text-stone hover:text-iron" title="New file" onClick={() => { setDraft("file"); setDraftName(""); }}>
            <Plus size={16} />
          </button>
          <button className="text-stone hover:text-iron" title="Upload" onClick={() => upload.current?.click()}>
            <UploadSimple size={16} />
          </button>
          <input ref={upload} type="file" multiple className="hidden" onChange={(e) => { onUpload(e.target.files); e.target.value = ""; }} />
        </div>
        {draft && (
          <form onSubmit={make} className="flex items-center gap-2 border-b border-thread-2 px-3 py-2">
            <input
              autoFocus
              value={draftName}
              onChange={(e) => setDraftName(e.target.value)}
              placeholder={draft === "dir" ? "Folder name" : "File name"}
              className="h-9 flex-1 rounded border border-thread bg-folio px-2 outline-none focus:border-bindery"
            />
            <Btn kind="primary" type="submit" disabled={!draftName.trim()}>
              Create
            </Btn>
            <button type="button" className="text-stone hover:text-iron" onClick={() => setDraft(null)}>
              <X size={14} />
            </button>
          </form>
        )}
        {err && <p className="px-4 py-2 text-carmine">{err}</p>}
        <div className="min-h-0 flex-1 overflow-auto">
          {entries.length === 0 && !err && !draft && <p className="px-4 py-6 text-stone">This folder is empty.</p>}
          <table className="w-full text-left">
            <tbody>
              {entries.map((e) => (
                <tr key={e.path} className="border-b border-thread-2 hover:bg-cloth">
                  <td className="w-8 px-4 py-2">
                    <TypeIcon name={e.name} dir={e.dir} />
                  </td>
                  <td className="px-1 py-2">
                    <button className="text-left hover:text-bindery" onClick={() => (e.dir ? setPath(e.path) : openFile(e))}>
                      {e.name}
                    </button>
                  </td>
                  <td className="w-24 px-3 py-2 text-right font-mono text-[12px] text-stone">{e.dir ? "—" : fmtSize(e.size)}</td>
                  <td className="w-44 px-3 py-2 font-mono text-[12px] text-stone">{e.modified.replace("T", " ").replace("Z", "")}</td>
                  <td className="w-20 px-3 py-2 text-right">
                    <button
                      className={pendingDel === e.path ? "text-carmine" : "text-stone hover:text-carmine"}
                      title={pendingDel === e.path ? "Click again to delete" : "Delete"}
                      onClick={() => remove(e)}
                    >
                      {pendingDel === e.path ? "Delete?" : <Trash size={14} />}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {file && (
        <aside className="flex min-w-0 flex-[1.1] flex-col border-l border-thread bg-folio">
          <div className="flex h-10 items-center gap-2 border-b border-thread-2 px-3">
            <span className="min-w-0 flex-1 truncate font-medium">{file.name}</span>
            <button className="text-stone hover:text-iron" title="Download" onClick={() => downloadFile(file.name, file.content, file.data)}>
              <DownloadSimple size={16} />
            </button>
            <button className="text-stone hover:text-iron" title="Close" onClick={() => setFile(null)}>
              <X size={14} />
            </button>
          </div>
          {file.truncated && <p className="border-b border-thread-2 px-3 py-1 text-[12px] text-stone">Showing the first 2 MB.</p>}
          <div className="min-h-0 flex-1 overflow-auto p-3">
            {busy ? (
              <p className="text-stone">Opening…</p>
            ) : (
              <FilePreview name={file.name} content={file.content} data={file.data} binary={file.binary} />
            )}
          </div>
        </aside>
      )}
    </div>
  );
}

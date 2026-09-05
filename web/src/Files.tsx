import { File, Folder, ArrowLeft } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
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

export function FilesPane({ bot, onStart }: { bot: Bot; onStart: () => void }) {
  const [path, setPath] = useState("");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [err, setErr] = useState("");
  const [file, setFile] = useState<{ name: string; content: string; binary: boolean; truncated: boolean; size: bigint } | null>(null);
  const [busy, setBusy] = useState(false);

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

  if (!bot.workerConnected) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-start justify-center px-6">
        <p className="mb-3 text-stone">Start the Bot to browse /workspace.</p>
        <Btn kind="secondary" onClick={onStart} disabled={bot.status === "starting"}>
          {bot.status === "starting" ? "Starting…" : "Start Bot"}
        </Btn>
      </div>
    );
  }

  async function openFile(e: FileEntry) {
    setBusy(true);
    setErr("");
    try {
      const r = await ui.readFile({ botId: bot.id, path: e.path });
      setFile({ name: r.name, content: r.content, binary: r.binary, truncated: r.truncated, size: r.size });
    } catch (ex) {
      setErr(fail(ex));
      setFile(null);
    } finally {
      setBusy(false);
    }
  }

  const trail = crumbs(path);
  const parent = trail.length > 1 ? trail[trail.length - 2].path : null;

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-10 shrink-0 items-center gap-2 border-b border-thread-2 px-4 text-[13px]">
          {parent !== null && (
            <button className="text-stone hover:text-iron" title="Up" onClick={() => setPath(parent)}>
              <ArrowLeft size={14} />
            </button>
          )}
          {trail.map((c, i) => (
            <span key={c.path || "root"} className="flex items-center gap-2">
              {i > 0 && <span className="text-thread">/</span>}
              <button className={i === trail.length - 1 ? "text-iron" : "text-stone hover:text-iron"} onClick={() => setPath(c.path)}>
                {c.label}
              </button>
            </span>
          ))}
        </div>
        {err && <p className="px-4 py-2 text-carmine">{err}</p>}
        <div className="min-h-0 flex-1 overflow-auto">
          {entries.length === 0 && !err && <p className="px-4 py-6 text-stone">This folder is empty.</p>}
          <table className="w-full text-left">
            <tbody>
              {entries.map((e) => (
                <tr key={e.path} className="border-b border-thread-2 hover:bg-cloth">
                  <td className="w-8 px-4 py-2 text-stone">{e.dir ? <Folder size={16} /> : <File size={16} />}</td>
                  <td className="px-1 py-2">
                    <button
                      className="text-left hover:text-bindery"
                      onClick={() => (e.dir ? setPath(e.path) : openFile(e))}
                    >
                      {e.name}
                    </button>
                  </td>
                  <td className="w-28 px-4 py-2 text-right font-mono text-[12px] text-stone">{e.dir ? "—" : fmtSize(e.size)}</td>
                  <td className="w-48 px-4 py-2 font-mono text-[12px] text-stone">{e.modified.replace("T", " ").replace("Z", "")}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {file && (
        <aside className="flex w-[420px] shrink-0 flex-col border-l border-thread bg-folio">
          <div className="flex h-10 items-center justify-between border-b border-thread-2 px-3">
            <span className="truncate font-medium">{file.name}</span>
            <button className="text-stone hover:text-iron" onClick={() => setFile(null)}>
              Close
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-3">
            {busy ? (
              <p className="text-stone">Opening…</p>
            ) : file.binary ? (
              <p className="text-stone">Binary file ({fmtSize(file.size)}). Open it on the Desktop.</p>
            ) : (
              <>
                {file.truncated && <p className="mb-2 text-[12px] text-stone">Showing the first 512 KB.</p>}
                <pre className="whitespace-pre-wrap font-mono text-[13px]">{file.content || " "}</pre>
              </>
            )}
          </div>
        </aside>
      )}
    </div>
  );
}

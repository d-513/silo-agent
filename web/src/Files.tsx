import { FolderPlus, Plus, UploadSimple, X } from "@phosphor-icons/react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { FileBrowser } from "./FileBrowser";
import { botSource, joinPath, parentPath, type FsEntry } from "./fs";
import { NeedMachine } from "./NeedMachine";
import type { Bot } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function FilesPane({ bot, onStart }: { bot: Bot; onStart: () => void }) {
  const source = useMemo(() => botSource(bot.id), [bot.id]);
  const [cwd, setCwd] = useState("");
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<"dir" | "file" | null>(null);
  const [draftName, setDraftName] = useState("");
  const [pendingDel, setPendingDel] = useState("");
  const [tick, setTick] = useState(0);
  const [selFile, setSelFile] = useState("");
  const upload = useRef<HTMLInputElement>(null);

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

  async function make(e: FormEvent) {
    e.preventDefault();
    const dest = joinPath(cwd, draftName.trim());
    if (!dest || !draft) return;
    setErr("");
    try {
      if (draft === "dir") await ui.mkdir({ botId: bot.id, path: dest });
      else await ui.putFile({ botId: bot.id, path: dest, data: new Uint8Array() });
      setDraft(null);
      setDraftName("");
      setTick((n) => n + 1);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function onUpload(list: FileList | null) {
    if (!list?.length) return;
    setErr("");
    try {
      for (const f of list) {
        const dest = joinPath(cwd, f.name);
        if (!dest) continue;
        const buf = new Uint8Array(await f.arrayBuffer());
        if (buf.byteLength > 2 << 20) {
          setErr(`${f.name} is larger than 2 MB`);
          continue;
        }
        await ui.putFile({ botId: bot.id, path: dest, data: buf });
      }
      setTick((n) => n + 1);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function remove(e: FsEntry) {
    if (pendingDel !== e.path) {
      setPendingDel(e.path);
      return;
    }
    setPendingDel("");
    setErr("");
    try {
      await ui.removeFile({ botId: bot.id, path: e.path });
      if (selFile === e.path || selFile.startsWith(`${e.path}/`)) setSelFile("");
      if (cwd === e.path || cwd.startsWith(`${e.path}/`)) setCwd(parentPath(e.path));
      setTick((n) => n + 1);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
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
      <FileBrowser
        key={bot.id}
        source={source}
        refreshKey={tick}
        pendingDel={pendingDel}
        onDelete={(e) => void remove(e)}
        onSelect={(path, dir) => {
          setCwd(dir ? path : parentPath(path));
          if (!dir) setSelFile(path);
        }}
        headerRight={
          <>
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
          </>
        }
      />
    </div>
  );
}

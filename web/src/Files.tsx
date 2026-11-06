import { FolderPlus, Plus, RefreshCw, Upload, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { FileBrowser } from "./FileBrowser";
import { Spinner, useArmedRow } from "./Feedback";
import { botSource, joinPath, parentPath, type FsEntry } from "./fs";
import { NeedMachine } from "./NeedMachine";
import type { Bot } from "./gen/silo/v1/ui_pb";
import { fail } from "./errors";
import { t } from "./i18n.ts";
import { machineStarting } from "./statusLook";

// openAt lands the tree in that folder (Drives → Open in Files). `actions` are
// the pane's own controls (dock, open as a page, close), at the header's end.
// `visible` is false while another pane is up (the pane stays mounted); coming
// back to it refreshes the tree.
export function FilesPane({ bot, visible = true, onStart, openAt, actions }: { bot: Bot; visible?: boolean; onStart: () => void; openAt: string; actions?: ReactNode }) {
  const source = useMemo(() => botSource(bot.id), [bot.id]);
  const [cwd, setCwd] = useState("");
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<"dir" | "file" | null>(null);
  const [draftName, setDraftName] = useState("");
  const del = useArmedRow(2500);
  // Files on their way up: the button spins and a second upload waits.
  const [uploading, setUploading] = useState(0);
  const [tick, setTick] = useState(0);
  const [selFile, setSelFile] = useState("");
  const upload = useRef<HTMLInputElement>(null);
  const wasVisible = useRef(visible);

  useEffect(() => {
    if (visible && !wasVisible.current) setTick((n) => n + 1);
    wasVisible.current = visible;
  }, [visible]);

  if (!bot.workerConnected) {
    return (
      <NeedMachine
        copy={t("Start the Bot to browse /workspace.")}
        starting={machineStarting(bot)}
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
    if (!list?.length || uploading) return;
    setErr("");
    const files = [...list];
    setUploading(files.length);
    try {
      for (const f of files) {
        const dest = joinPath(cwd, f.name);
        if (dest && f.size > 50 << 20) setErr(t("{{name}} is larger than 50 MB", { name: f.name }));
        else if (dest) await ui.putFile({ botId: bot.id, path: dest, data: new Uint8Array(await f.arrayBuffer()) });
        setUploading((n) => n - 1);
      }
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setUploading(0);
      setTick((n) => n + 1);
    }
  }

  function remove(e: FsEntry) {
    del.fire(e.path, () => void removeNow(e));
  }

  async function removeNow(e: FsEntry) {
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
        <form onSubmit={make} className="flex items-center gap-2 border-b border-line-strong px-3 py-2">
          <input
            autoFocus
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            placeholder={draft === "dir" ? t("Folder name") : t("File name")}
            className="h-9 flex-1 rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-2 outline-none"
          />
          <Btn kind="primary" type="submit" disabled={!draftName.trim()}>
            {t("Create")}
          </Btn>
          <button type="button" className="text-ink-2 hover:text-ink" onClick={() => setDraft(null)}>
            <X size={14} />
          </button>
        </form>
      )}
      {err && <p className="px-4 py-2 text-vermilion">{err}</p>}
      <FileBrowser
        key={`${bot.id}:${openAt}`}
        source={source}
        initialDir={openAt}
        refreshKey={tick}
        pendingDel={del.armed}
        onDelete={remove}
        onSelect={(path, dir) => {
          setCwd(dir ? path : parentPath(path));
          if (!dir) setSelFile(path);
        }}
        headerRight={
          <>
            <button className="text-ink-2 hover:text-ink" title={t("Refresh")} onClick={() => setTick((n) => n + 1)}>
              <RefreshCw size={16} />
            </button>
            <button className="text-ink-2 hover:text-ink" title={t("New folder")} onClick={() => { setDraft("dir"); setDraftName(""); }}>
              <FolderPlus size={16} />
            </button>
            <button className="text-ink-2 hover:text-ink" title={t("New file")} onClick={() => { setDraft("file"); setDraftName(""); }}>
              <Plus size={16} />
            </button>
            <button
              className="text-ink-2 hover:text-ink aria-disabled:cursor-wait"
              title={uploading ? t("Uploading {{count}} files…", { count: uploading }) : t("Upload")}
              aria-disabled={uploading > 0}
              onClick={() => (uploading ? undefined : upload.current?.click())}
            >
              {uploading ? <Spinner size={16} /> : <Upload size={16} />}
            </button>
            <input ref={upload} type="file" multiple className="hidden" onChange={(e) => { onUpload(e.target.files); e.target.value = ""; }} />
            {actions ? (
              <>
                <span aria-hidden className="h-4 w-px shrink-0 bg-line-strong" />
                <span className="-mx-1.5 flex shrink-0 items-center gap-0.5">{actions}</span>
              </>
            ) : null}
          </>
        }
      />
    </div>
  );
}

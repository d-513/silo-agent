import { FolderPlus, RefreshCw, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { fail } from "./errors";
import { ArmedButton, Spinner } from "./Feedback";
import { FolderNav, type FolderEntry } from "./FolderNav";
import { ago, day } from "./format";
import { Panel } from "./Field";
import { UI, type Bot, type KnowledgeFolder, type KnowledgeHit } from "./gen/silo/v1/ui_pb";
import { NeedMachine } from "./NeedMachine";
import { patch, reload } from "./query";
import { SearchBox } from "./SearchBox";
import { useSearch } from "./useSearch";
import { t } from "./i18n.ts";
import { Trans } from "./Trans.tsx";

// Folders the Bot cannot index even if picked; the server refuses them too.
const scratch = new Set(["bot", "tmp"]);

function within(child: string, parent: string) {
  return child === parent || child.startsWith(`${parent}/`);
}

// FolderPicker browses the Bot's workspace one folder at a time and indexes the
// current one. Folders only: what is inside is the Files tab's business.
function FolderPicker({
  bot,
  taken,
  adding,
  onPick,
  onCancel,
  onStart,
}: {
  bot: Bot;
  taken: string[];
  adding: boolean;
  onPick: (path: string) => void;
  onCancel: () => void;
  onStart: () => void;
}) {
  const [cwd, setCwd] = useState("");
  const [dirs, setDirs] = useState<FolderEntry[] | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!bot.workerConnected) return;
    let dead = false;
    setDirs(null);
    setErr("");
    ui.listFiles({ botId: bot.id, path: cwd })
      .then((r) => {
        if (dead) return;
        setDirs(
          r.entries
            .filter((e) => e.dir && !e.name.startsWith(".") && !(cwd === "" && scratch.has(e.name)))
            .map((e) => ({ name: e.name, path: e.path }))
            .sort((a, b) => a.path.localeCompare(b.path)),
        );
      })
      .catch((e) => {
        if (!dead) setErr(fail(e));
      });
    return () => {
      dead = true;
    };
  }, [bot.id, bot.workerConnected, cwd]);

  if (!bot.workerConnected) {
    return (
      <div className="flex min-h-[200px] flex-col">
        <NeedMachine copy={t("Start the Bot to browse its folders.")} starting={bot.status === "starting"} onStart={onStart} />
      </div>
    );
  }

  const drive = cwd === "drives" || cwd.startsWith("drives/");
  let blocked = "";
  if (cwd === "") blocked = t("Open a folder first.");
  else if (cwd === "drives") blocked = t("Open one drive folder, not all drives at once.");
  else if (taken.some((t) => within(cwd, t))) blocked = t("Already indexed.");
  else if (taken.some((t) => within(t, cwd))) blocked = t("Holds a folder that is already indexed.");

  return (
    <div>
      <FolderNav rootLabel="workspace" at={cwd} dirs={dirs} err={err} onGo={setCwd} empty={t("No subfolders.")} />
      {drive && (
        <p className="px-5 pt-3 text-[12.5px] leading-[18px] text-ink-2">
          {t("This folder is on a drive. Indexing reads every file over the network, so a large folder, or a provider that rate-limits, can be slow. It is re-checked less often than local folders.")}
        </p>
      )}
      <div className="flex items-center gap-3 px-5 py-4">
        <Btn kind="primary" disabled={blocked !== "" || adding} icon={adding ? <Spinner size={13} tone="on-ink" /> : <FolderPlus size={14} />} onClick={() => onPick(cwd)}>
          {cwd ? <Trans i18nKey="Index <mono>{{folder}}</mono>" values={{ folder: cwd }} components={{ mono: <span className="font-mono" /> }} /> : t("Index this folder")}
        </Btn>
        <Btn kind="ghost" onClick={onCancel}>
          {t("Cancel")}
        </Btn>
        {blocked && <span className="text-[12.5px] text-ink-3">{blocked}</span>}
      </div>
    </div>
  );
}

function FolderRow({
  f,
  canSync,
  onSync,
  onRemove,
}: {
  f: KnowledgeFolder;
  canSync: boolean;
  onSync: () => void;
  onRemove: () => void;
}) {
  const [open, setOpen] = useState(false);
  const syncing = f.status === "syncing";
  return (
    <div className="px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[13.5px] break-all">{f.path}</span>
            {f.drive && <span className="rounded-xs bg-well px-1.5 py-px font-mono text-[11px] text-ink-2">{t("drive")}</span>}
          </p>
          <p className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[12.5px] text-ink-2">
            {syncing ? (
              <>
                <Spinner size={12} />
                <span>{f.files > 0 ? t("Indexing… {{count}} files so far", { count: f.files }) : t("Indexing…")}</span>
              </>
            ) : f.status === "error" ? (
              <span className="text-vermilion">{f.detail || t("Sync failed.")}</span>
            ) : (
              <span>
                {[t("{{count}} files", { count: f.files }), t("{{count}} passages", { count: f.chunks }), t("synced {{when}}", { when: ago(f.lastSyncAt) })].join(" · ")}
              </span>
            )}
            {f.status === "error" && f.lastSyncAt && <span className="text-ink-3">{t("last attempt {{when}}", { when: ago(f.lastSyncAt) })}</span>}
          </p>
          {f.status === "idle" && f.detail && <p className="mt-0.5 text-[12.5px] text-ink-2">{f.detail}</p>}
          {f.skipped > 0 && (
            <button type="button" className="mt-1 text-[12.5px] text-cobalt-deep hover:underline" onClick={() => setOpen(!open)}>
              {t("{{count}} not indexed", { count: f.skipped })} {open ? "▾" : "▸"}
            </button>
          )}
          {open && (
            <ul className="mt-1.5 space-y-0.5 font-mono text-[12px] text-ink-2">
              {f.issues.map((i) => (
                <li key={i.path} className="break-all">
                  {i.path} <span className="text-ink-3">— {i.detail}</span>
                </li>
              ))}
              {f.skipped > f.issues.length && <li className="text-ink-3">{t("…and {{count}} more", { count: f.skipped - f.issues.length })}</li>}
            </ul>
          )}
        </div>
        <Btn
          kind="ghost"
          size="sm"
          iconOnly
          className="shrink-0"
          title={canSync ? t("Check for changes now") : t("Start the Bot to sync")}
          aria-label={t("Sync now")}
          aria-disabled={!canSync || syncing}
          icon={<RefreshCw size={13} />}
          onClick={() => {
            if (canSync && !syncing) onSync();
          }}
        />
        <ArmedButton kind="ghost" size="sm" iconOnly className="shrink-0" title={t("Stop indexing and forget this folder")} icon={<Trash2 size={13} />} onConfirm={onRemove}>
          {t("Remove")}
        </ArmedButton>
      </div>
    </div>
  );
}

function SearchPanel({ botId, onError }: { botId: string; onError: (s: string) => void }) {
  const [query, setQuery] = useState("");
  const { hits, searching, error } = useSearch<KnowledgeHit>(query, (q) => ui.searchKnowledge({ botId, query: q }).then((r) => r.hits), 400, [botId]);
  useEffect(() => {
    if (error) onError(error);
  }, [error, onError]);

  return (
    <Panel
      title={t("Try a search")}
      note={t("The same search the Bot gets as search_docs: meaning and exact words together. Use it to check what the Bot will find.")}
      padded={false}
      className="mt-4"
    >
      <div className="px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)]">
        <SearchBox value={query} onChange={setQuery} searching={searching} placeholder={t("Ask about your documents, or paste a name or part number")} />
      </div>
      {hits === null ? (
        <p className="px-5 py-4 text-ink-3">{searching ? t("Searching…") : t("Type to search.")}</p>
      ) : hits.length === 0 ? (
        <p className="px-5 py-4 text-ink-3">{t("Nothing matches.")}</p>
      ) : (
        hits.map((h, i) => (
          <div key={`${h.path}:${h.locator}:${i}`} className="px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
            <p className="font-mono text-[12.5px] break-all">
              {h.path} <span className="text-ink-3">· {h.locator}</span>
            </p>
            <p className="mt-1 line-clamp-4 text-[13.5px] break-words whitespace-pre-wrap text-ink-2">{h.snippet}</p>
            <p className="mt-0.5 font-mono text-[11px] text-ink-3">{t("indexed {{when}}", { when: day(h.indexedAt) })}</p>
          </div>
        ))
      )}
    </Panel>
  );
}

export function KnowledgePane({ bot, onError, onStart }: { bot: Bot; onError: (s: string) => void; onStart: () => void }) {
  // Poll while something is indexing; an idle page does not need to.
  const q = useQuery(
    UI.method.listKnowledge,
    { botId: bot.id },
    { refetchInterval: (x) => (x.state.data?.folders.some((f) => f.status === "syncing") ? 2500 : false) },
  );
  const folders = q.data?.folders ?? null;
  const enabled = q.data?.enabled ?? true;
  const [picking, setPicking] = useState(false);
  const [adding, setAdding] = useState(false);
  const load = () => reload(UI.method.listKnowledge, { botId: bot.id });

  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error, onError]);
  useEffect(() => setPicking(false), [bot.id]);

  async function add(path: string) {
    onError("");
    setAdding(true);
    try {
      await ui.addKnowledgeFolder({ botId: bot.id, path });
      setPicking(false);
      await load();
    } catch (e) {
      onError(fail(e));
    } finally {
      setAdding(false);
    }
  }

  async function sync(id: string) {
    onError("");
    try {
      await ui.syncKnowledge({ botId: bot.id, id });
      await load();
    } catch (e) {
      onError(fail(e));
    }
  }

  async function remove(id: string) {
    onError("");
    try {
      await ui.removeKnowledgeFolder({ botId: bot.id, id });
      patch(UI.method.listKnowledge, { botId: bot.id }, (r) => ({ ...r, folders: r.folders.filter((f) => f.id !== id) }));
    } catch (e) {
      onError(fail(e));
    }
  }

  return (
    <div className="silo-page pb-12">
      <h2 className="text-title">{t("Knowledge")}</h2>
      <p className="mb-6 text-ink-2">
        {t("Pick folders of documents and the Bot can search them by meaning and by exact words, with the file and page cited. Files stay on the Bot's machine; only their text and an embedding are kept in the control plane.")}
      </p>
      {!enabled && <p className="mb-4 text-[13px] text-vermilion">{t("The operator turned knowledge off (knowledge.enabled), so nothing new is indexed and the Bot has no search_docs tool.")}</p>}
      <Panel
        title={t("Indexed folders")}
        note={t("PDFs, Word and OpenDocument files, HTML, Markdown, and plain text or code. Scanned PDF pages and images (English and Polish) are read with OCR. Changes are picked up every few minutes, sooner when the Bot or you write there.")}
        padded={false}
        action={
          !picking && (
            <Btn size="sm" icon={<FolderPlus size={13} />} disabled={!enabled} onClick={() => setPicking(true)}>
              {t("Add folder")}
            </Btn>
          )
        }
      >
        {picking && (
          <div className="shadow-[inset_0_-1px_0_var(--color-line)]">
            <FolderPicker bot={bot} taken={(folders ?? []).map((f) => f.path)} adding={adding} onPick={(p) => void add(p)} onCancel={() => setPicking(false)} onStart={onStart} />
          </div>
        )}
        {folders === null ? (
          <p className="px-5 py-4 text-ink-3">{t("Loading…")}</p>
        ) : folders.length === 0 ? (
          !picking && <p className="px-5 py-4 text-ink-3">{t("No folders yet. Add one to give the Bot something to search.")}</p>
        ) : (
          folders.map((f) => <FolderRow key={f.id} f={f} canSync={bot.workerConnected} onSync={() => void sync(f.id)} onRemove={() => void remove(f.id)} />)
        )}
      </Panel>
      {folders && folders.length > 0 && <SearchPanel botId={bot.id} onError={onError} />}
    </div>
  );
}

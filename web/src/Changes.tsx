import { ChevronRight, GitCompareArrows, HardDrive, MessageCircle, Undo2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { ui } from "./api";
import { canRestore, changesNotice, changesPollMs, driveLine, drivePath, fileCount, fileNote, parsePatch, restoreLabel, restoreNotice, sizeChange, sourceHint, sourceTitle, type DiffLine } from "./changeset";
import { Collapse } from "./Collapse";
import { fail, isGone } from "./errors";
import { ArmedButton } from "./Feedback";
import { ErrorWell, SkeletonRows } from "./Field";
import { day, feedStamp, fmtBytes } from "./format";
import { UI, type Bot, type Change, type ChangeFile, type DriveChangeEntry, type ListChangesResponse } from "./gen/silo/v1/ui_pb";
import { chatLink } from "./links";
import { NeedMachine } from "./NeedMachine";
import { reload } from "./query";
import { TabPill, TabPills } from "./TabPills";

// ChangesPane is the right-rail pane for what changed. Workspace is one row per
// stretch between two snapshots, newest first, each opening into its files and
// each file into its diff, and either can be put back as it was before that
// change; that history lives on the Bot's machine, so it reads only while the
// machine is up, and it asks only while `visible`: every look snapshots the
// workspace. Drives is the journal of what was written, deleted
// or renamed on the Bot's drives, kept by the control plane, so it reads either
// way; it is offered once the Bot has a drive.
export function ChangesPane({
  bot,
  visible,
  onStart,
  onError,
  actions,
}: {
  bot: Bot;
  visible: boolean;
  onStart: () => void;
  onError: (s: string) => void;
  actions?: ReactNode;
}) {
  const botId = bot.id;
  const online = bot.workerConnected;
  const q = useQuery(UI.method.listChanges, { botId }, { enabled: visible && online, refetchInterval: changesPollMs(visible, online) });
  const dq = useQuery(UI.method.listDriveChanges, { botId }, { enabled: visible, refetchInterval: changesPollMs(visible, true) });
  const [view, setView] = useState<"workspace" | "drives">("workspace");
  const hasDrives = !!dq.data && (dq.data.hasDrives || dq.data.changes.length > 0);
  const drives = hasDrives && view === "drives";

  useEffect(() => {
    const e = q.error ?? dq.error;
    if (e) onError(fail(e));
  }, [q.error, dq.error]);
  useEffect(() => setView("workspace"), [botId]);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <header className="flex h-10 shrink-0 items-center gap-2 border-b border-line-strong px-3 text-[13px]">
        <GitCompareArrows size={15} className="shrink-0 text-ink-2" />
        <span className="shrink-0 font-medium">Changes</span>
        <span className="truncate font-mono text-[12px] text-ink-3">{drives ? "/workspace/drives" : "/workspace"}</span>
        {actions ? (
          <>
            <span aria-hidden className="ml-auto h-4 w-px shrink-0 bg-line-strong" />
            <span className="-mx-1 flex shrink-0 items-center gap-0.5">{actions}</span>
          </>
        ) : null}
      </header>
      <div className="flex min-h-0 flex-1 flex-col overflow-auto p-3">
        {hasDrives ? (
          <div className="mb-3 flex shrink-0">
            <TabPills>
              <TabPill on={!drives} onClick={() => setView("workspace")}>
                Workspace
              </TabPill>
              <TabPill on={drives} onClick={() => setView("drives")}>
                Drives
              </TabPill>
            </TabPills>
          </div>
        ) : null}
        {drives ? (
          <DriveJournal entries={dq.data?.changes ?? []} keep={dq.data?.keep ?? 0} off={dq.data?.state === "off"} />
        ) : online ? (
          <WorkspaceChanges botId={botId} data={q.data ?? null} onError={onError} />
        ) : (
          <NeedMachine copy="Start the Bot to see what changed in /workspace." starting={bot.status === "starting"} onStart={onStart} />
        )}
      </div>
    </div>
  );
}

// What a row or a file asks for: put one file back ("" for the whole change).
type Restore = (path: string, oldPath: string) => void;

function WorkspaceChanges({ botId, data, onError }: { botId: string; data: ListChangesResponse | null; onError: (s: string) => void }) {
  const [open, setOpen] = useState("");
  const [busy, setBusy] = useState(false);
  // What the last restore did, until the reader moves on.
  const [done, setDone] = useState("");
  useEffect(() => {
    setOpen("");
    setDone("");
  }, [botId]);
  const restore = async (c: Change, path: string, oldPath: string) => {
    setBusy(true);
    setDone("");
    try {
      const res = await ui.restoreChange({ botId, base: c.base, head: c.head, path, oldPath });
      setDone(restoreNotice(res, path));
      await reload(UI.method.listChanges, { botId });
    } catch (e) {
      onError(isGone(e) ? "This change is no longer in the history." : fail(e));
    } finally {
      setBusy(false);
    }
  };
  const notice = data ? changesNotice(data.state) : null;
  if (notice) {
    return (
      <div className="rounded-card bg-well px-4 py-3 text-[12.5px] leading-[19px] text-ink-2">
        {notice.text}{" "}
        {notice.reset ? (
          <Link to="/bots/$botId/container" params={{ botId }} className="font-medium text-cobalt hover:underline">
            Open Containers
          </Link>
        ) : null}
      </div>
    );
  }
  if (data === null) return <SkeletonRows rows={3} height={56} />;
  const rows = [...(data.pending ? [data.pending] : []), ...data.changes];
  const cap = data.maxFileBytes;
  return (
    <>
      {data.indexing ? (
        <p className="mb-3 rounded-card bg-well px-4 py-3 text-[12.5px] leading-[19px] text-ink-2">Still reading this workspace for the first time. Changes show once that is done.</p>
      ) : null}
      {done ? (
        <p role="status" className="mb-3 rounded-card bg-well px-4 py-3 text-[12.5px] leading-[19px] text-ink-2">
          {done}
        </p>
      ) : null}
      {rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <GitCompareArrows size={20} className="text-ink-3" />
          <p className="text-ink-2">Nothing has changed yet.</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">When the Bot, a script it runs, or you change a file in /workspace, the difference shows here.</p>
        </div>
      ) : (
        <ul className="overflow-hidden rounded-card bg-surface shadow-card">
          {rows.map((c, i) => {
            const key = c.pending ? "pending" : c.id;
            return (
              <ChangeRow
                key={key}
                botId={botId}
                change={c}
                cap={cap}
                first={i === 0}
                open={open === key}
                busy={busy}
                onToggle={() => {
                  setOpen(open === key ? "" : key);
                  setDone("");
                }}
                onRestore={(path, oldPath) => void restore(c, path, oldPath)}
              />
            );
          })}
        </ul>
      )}
      <p className="mt-2 px-1 text-[12.5px] leading-[19px] text-ink-3">
        The workspace is compared before and after each run{data.since ? `, since ${day(data.since)}` : ""}. Drives, <span className="font-mono text-[12px]">tmp/</span>,{" "}
        <span className="font-mono text-[12px]">bot/</span> and dependency folders are left out, and a file over {fmtBytes(cap)} is recorded by its size only, so it cannot be put back. Undoing a
        change keeps what it replaces: the restore is listed as a change of its own.
      </p>
    </>
  );
}

// DriveJournal is what happened on the Bot's drives, newest first: a line per
// file written, deleted or renamed. There is nothing to open: no copy of a
// drive's files is kept, so there is no diff.
function DriveJournal({ entries, keep, off }: { entries: DriveChangeEntry[]; keep: number; off: boolean }) {
  if (off) {
    return <div className="rounded-card bg-well px-4 py-3 text-[12.5px] leading-[19px] text-ink-2">{changesNotice("off")?.text}</div>;
  }
  return (
    <>
      {entries.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <HardDrive size={20} className="text-ink-3" />
          <p className="text-ink-2">Nothing has been changed on a drive yet.</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">A file written, deleted or renamed under /workspace/drives is noted here, whatever did it.</p>
        </div>
      ) : (
        <ul className="overflow-hidden rounded-card bg-surface shadow-card">
          {entries.map((e, i) => (
            <li key={e.id} className={`px-4 py-2.5 ${i === 0 ? "" : "shadow-[inset_0_1px_0_var(--color-line)]"}`}>
              <div className="flex min-w-0 items-center gap-2">
                <span title={drivePath(e.drive, e.path)} className="min-w-0 truncate font-mono text-[12.5px] text-ink [direction:rtl] [text-align:left]">
                  <bdi>{drivePath(e.drive, e.path)}</bdi>
                </span>
                <span className="ml-auto shrink-0 font-mono text-[12px] text-ink-3">{feedStamp(e.at)}</span>
              </div>
              <p className={`mt-0.5 truncate text-[12.5px] ${e.op === "deleted" ? "text-ink-2" : "text-ink-3"}`}>{driveLine(e)}</p>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-2 px-1 text-[12.5px] leading-[19px] text-ink-3">
        A record of what was written, deleted or renamed on a drive, not a copy of it: to get a file back, use the provider's own version history or trash. An upload is noted when it reaches the
        provider, a few seconds after the file is saved.{keep ? ` The newest ${keep} lines are kept.` : ""}
      </p>
    </>
  );
}

function ChangeRow({
  botId,
  change: c,
  cap,
  first,
  open,
  busy,
  onToggle,
  onRestore,
}: {
  botId: string;
  change: Change;
  cap: bigint;
  first: boolean;
  open: boolean;
  busy: boolean;
  onToggle: () => void;
  onRestore: Restore;
}) {
  const hint = sourceHint(c.sources, c.restore);
  const chats = c.sources.filter((s) => s.kind === "chat");
  return (
    <li className={first ? "" : "shadow-[inset_0_1px_0_var(--color-line)]"}>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-start gap-3 px-4 py-3 text-left transition-[background-color] duration-[160ms] ease-quiet hover:bg-well outline-offset-[-2px]"
      >
        <ChevronRight size={14} className={`mt-[3px] shrink-0 text-ink-3 transition-transform duration-200 ease-quiet ${open ? "rotate-90" : ""}`} />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate text-[13.5px] font-medium text-ink">{sourceTitle(c.sources, c.restore)}</span>
            <span className="ml-auto shrink-0 font-mono text-[12px] text-ink-3">{c.pending ? "Now" : feedStamp(c.at)}</span>
          </span>
          <span className="mt-0.5 flex items-center gap-2 text-[12.5px] text-ink-3">
            <span>{fileCount(c.files)}</span>
            <Counts added={c.added} deleted={c.deleted} />
          </span>
        </span>
      </button>
      <Collapse open={open}>
        <div className="px-4 pb-4 pl-[42px]">
          {hint ? <p className="mb-2 text-[12.5px] leading-[18px] text-ink-2">{hint}</p> : null}
          <ChangeFiles botId={botId} change={c} cap={cap} busy={busy} onRestore={onRestore} />
          <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
            <ArmedButton
              kind="secondary"
              size="sm"
              disabled={busy}
              title="Put every file of this change back as it was before it"
              armedLabel={`Click again to put ${fileCount(c.files)} back`}
              icon={<Undo2 size={13} />}
              onConfirm={() => onRestore("", "")}
            >
              Undo this change
            </ArmedButton>
            {chats.map((s) => (
              <Link key={s.chatId} {...chatLink(botId, s.chatId)} className="flex min-w-0 items-center gap-1.5 text-[12.5px] font-medium text-cobalt hover:underline">
                <MessageCircle size={13} className="shrink-0" />
                <span className="truncate">Open “{s.name}”</span>
              </Link>
            ))}
          </div>
        </div>
      </Collapse>
    </li>
  );
}

// Line counts as git writes them. No colour: the diff itself carries that.
function Counts({ added, deleted }: { added: number; deleted: number }) {
  if (added === 0 && deleted === 0) return null;
  return (
    <span className="font-mono text-[12px] tabular-nums">
      {added > 0 ? `+${added}` : ""}
      {added > 0 && deleted > 0 ? " " : ""}
      {deleted > 0 ? `−${deleted}` : ""}
    </span>
  );
}

function ChangeFiles({ botId, change: c, cap, busy, onRestore }: { botId: string; change: Change; cap: bigint; busy: boolean; onRestore: Restore }) {
  // A pending change is a new pair on every look; keep showing the last list
  // while the next one loads.
  const q = useQuery(UI.method.listChangeFiles, { botId, base: c.base, head: c.head }, { placeholderData: (prev) => prev, staleTime: c.pending ? 0 : Infinity });
  const [open, setOpen] = useState("");
  if (q.error && !q.data) {
    return <ErrorWell>{isGone(q.error) ? "This change is no longer in the history." : fail(q.error)}</ErrorWell>;
  }
  if (!q.data) return <SkeletonRows rows={Math.min(c.files, 3)} height={28} />;
  return (
    <>
      <ul className="-mx-2">
        {q.data.files.map((f) => (
          <FileRow
            key={f.path}
            botId={botId}
            change={c}
            file={f}
            cap={cap}
            open={open === f.path}
            busy={busy}
            onToggle={() => setOpen(open === f.path ? "" : f.path)}
            onRestore={onRestore}
          />
        ))}
      </ul>
      {q.data.truncated ? <p className="mt-2 text-[12.5px] text-ink-3">Only the first {q.data.files.length} files are listed.</p> : null}
    </>
  );
}

const statusWords: Record<string, string> = { added: "Added", deleted: "Deleted", renamed: "Renamed", repo: "Repository" };

function FileRow({
  botId,
  change: c,
  file: f,
  cap,
  open,
  busy,
  onToggle,
  onRestore,
}: {
  botId: string;
  change: Change;
  file: ChangeFile;
  cap: bigint;
  open: boolean;
  busy: boolean;
  onToggle: () => void;
  onRestore: Restore;
}) {
  const note = fileNote(f, cap);
  const [label, armedLabel] = restoreLabel(f.status);
  const sizes = f.status === "repo" ? "" : sizeChange(f.oldSize, f.newSize);
  return (
    <li>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}
        className="flex h-8 w-full min-w-0 items-center gap-2 rounded-control px-2 text-left text-[12.5px] transition-colors duration-[160ms] ease-quiet hover:bg-well"
      >
        <ChevronRight size={12} className={`shrink-0 text-ink-3 transition-transform duration-200 ease-quiet ${open ? "rotate-90" : ""}`} />
        {/* The end of a path names the file, so a long one is cut at its start. */}
        <span className="min-w-0 truncate font-mono text-ink [direction:rtl] [text-align:left]">
          <bdi>{f.path}</bdi>
        </span>
        {statusWords[f.status] ? <span className="shrink-0 text-ink-3">{statusWords[f.status]}</span> : null}
        <span className="ml-auto shrink-0 text-ink-3">{note ? <span className="font-mono text-[12px] tabular-nums">{sizes}</span> : <Counts added={f.added} deleted={f.deleted} />}</span>
      </button>
      <Collapse open={open}>
        <div className="px-2 pt-1 pb-2">
          {f.oldPath ? (
            <p className="mb-1.5 text-[12.5px] text-ink-3">
              Was <span className="font-mono text-[12px]">{f.oldPath}</span>
            </p>
          ) : null}
          {note ? <p className="rounded-sm bg-well px-3 py-2.5 text-[12.5px] leading-[18px] text-ink-2">{note}</p> : <FilePatch botId={botId} change={c} file={f} />}
          {canRestore(f) ? (
            <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1">
              <ArmedButton kind="ghost" size="sm" disabled={busy} armedLabel={armedLabel} icon={<Undo2 size={13} />} onConfirm={() => onRestore(f.path, f.oldPath)}>
                {label}
              </ArmedButton>
              <span className="text-[12.5px] text-ink-3">As it was before this change.</span>
            </div>
          ) : null}
        </div>
      </Collapse>
    </li>
  );
}

function FilePatch({ botId, change: c, file: f }: { botId: string; change: Change; file: ChangeFile }) {
  const q = useQuery(UI.method.getChangePatch, { botId, base: c.base, head: c.head, path: f.path, oldPath: f.oldPath }, { placeholderData: (prev) => prev, staleTime: c.pending ? 0 : Infinity });
  const lines = useMemo(() => parsePatch(q.data?.patch ?? ""), [q.data?.patch]);
  if (q.error && !q.data) {
    return <ErrorWell>{isGone(q.error) ? "This change is no longer in the history." : fail(q.error)}</ErrorWell>;
  }
  if (!q.data) return <SkeletonRows rows={1} height={64} />;
  if (lines.length === 0) {
    const why = q.data.binary ? "Not text, so there are no lines to compare." : f.status === "renamed" ? "Moved without a change to its lines." : "No lines changed.";
    return <p className="rounded-sm bg-well px-3 py-2.5 text-[12.5px] leading-[18px] text-ink-2">{why}</p>;
  }
  return (
    <>
      <Diff lines={lines} />
      {q.data.truncated ? <p className="mt-1.5 text-[12.5px] text-ink-3">The diff is long; only its start is shown.</p> : null}
    </>
  );
}

const lineTone: Record<DiffLine["kind"], string> = {
  add: "text-emerald",
  del: "text-vermilion",
  ctx: "text-ink-2",
  hunk: "text-ink-3",
  note: "text-ink-3",
};
const lineSign: Record<DiffLine["kind"], string> = { add: "+", del: "−", ctx: "", hunk: "", note: "" };

// Diff is one file's hunks in the thread's diff colours: additions `emerald`,
// deletions `vermilion`, on a `well` block that scrolls sideways on its own.
function Diff({ lines }: { lines: DiffLine[] }) {
  return (
    <div className="max-h-[480px] overflow-auto rounded-sm bg-well py-2 font-mono text-[12px] leading-[18px]">
      <table className="border-separate border-spacing-0">
        <tbody>
          {lines.map((l, i) =>
            l.kind === "hunk" ? (
              <tr key={i}>
                <td colSpan={3} className={`px-3 text-ink-3 ${i === 0 ? "" : "pt-2"}`}>
                  <span aria-hidden className="select-none">
                    ⋯{" "}
                  </span>
                  {l.text}
                </td>
              </tr>
            ) : (
              <tr key={i} className={lineTone[l.kind]}>
                <td className="w-px pr-2 pl-3 text-right tabular-nums text-ink-3 select-none">{l.no ?? ""}</td>
                <td className="w-px pr-1.5 select-none">{lineSign[l.kind]}</td>
                <td className="pr-3 whitespace-pre">{l.kind === "note" ? l.text : l.text || " "}</td>
              </tr>
            ),
          )}
        </tbody>
      </table>
    </div>
  );
}

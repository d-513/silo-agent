// Pure helpers for the Changes pane, kept out of the component so node can test
// them (not changes.ts: that would clash with Changes.tsx on a case-insensitive
// disk): reading a unified diff into numbered lines, and the words for a
// change's source, its file count and a file's sizes.
import { fmtBytes } from "./format.ts";
import { msg, t } from "./i18n.ts";

export type DiffLine = {
  kind: "hunk" | "add" | "del" | "ctx" | "note";
  text: string;
  // The line's number in the file: the new one, or the old one for a deletion.
  no?: number;
};

// parsePatch reads the hunks of one file's unified diff. The worker has already
// dropped the file header, so the first line is a hunk's "@@ -a,b +c,d @@".
export function parsePatch(patch: string): DiffLine[] {
  const out: DiffLine[] = [];
  let oldNo = 0;
  let newNo = 0;
  const lines = patch.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  for (const raw of lines) {
    const hunk = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$/.exec(raw);
    if (hunk) {
      oldNo = Number(hunk[1]);
      newNo = Number(hunk[2]);
      out.push({ kind: "hunk", text: hunk[3] });
    } else if (raw.startsWith("+")) {
      out.push({ kind: "add", text: raw.slice(1), no: newNo++ });
    } else if (raw.startsWith("-")) {
      out.push({ kind: "del", text: raw.slice(1), no: oldNo++ });
    } else if (raw.startsWith("\\")) {
      // "\ No newline at end of file"
      out.push({ kind: "note", text: raw.slice(2) });
    } else {
      out.push({ kind: "ctx", text: raw.slice(1), no: newNo++ });
      oldNo++;
    }
  }
  return out;
}

const kindWords: Record<string, string> = { chat: msg("Chat"), automation: msg("Automation"), channel: msg("Channel"), subagent: msg("Subagent") };

function sourceNames(sources: { kind: string; name: string }[]): string {
  return sources.map((s) => `${t(kindWords[s.kind] ?? msg("Run"))} “${s.name}”`).join(", ");
}

// Who made a change, as the row's title: the runs at work, or nobody's run. A
// restore is the owner's, whoever else was at work.
export function sourceTitle(sources: { kind: string; name: string }[], restore = false): string {
  if (restore) return t("Restored by you");
  if (sources.length === 0) return t("Outside a run");
  return sourceNames(sources);
}

// Why a change has more than one name on it, or none.
export function sourceHint(sources: { kind: string; name: string }[], restore = false): string {
  if (restore) {
    const undo = t("Files put back from this pane. Undo it to bring back what it replaced.");
    return sources.length > 0 ? `${undo} ${t("{{sources}} was at work at the time, so its changes may be in here too.", { sources: sourceNames(sources) })}` : undo;
  }
  if (sources.length === 0) return t("Changed by you in Files or the Console, or by a process left running on the machine.");
  if (sources.length > 1) return t("These runs were at work on the same machine at once, so the change is theirs together.");
  return "";
}

// A file the history can put back: it holds no content for a file over the size
// limit or for a repository of its own.
export function canRestore(f: { status: string; large: boolean }): boolean {
  return !f.large && f.status !== "repo";
}

// What putting a file back as it was before a change does to it, as the
// button's label and its armed label.
export function restoreLabel(status: string): [string, string] {
  if (status === "added") return [t("Remove this file"), t("Click again to remove it")];
  if (status === "deleted") return [t("Bring this file back"), t("Click again to bring it back")];
  if (status === "renamed") return [t("Move this file back"), t("Click again to move it back")];
  return [t("Restore this file"), t("Click again to restore it")];
}

const skipWords: Record<string, string> = {
  large: msg("over the size limit, so what was in it was never kept"),
  repo: msg("a git repository of its own"),
  blocked: msg("something else is in its place"),
};

// What a restore did, as the sentence shown above the list. `path` is the one
// file that was asked for, "" for a whole change.
export function restoreNotice(res: { restored: number; skipped: { path: string; reason: string }[]; skippedTotal: number }, path: string): string {
  const name = path.split("/").pop() ?? "";
  const parts: string[] = [];
  if (res.restored > 0) {
    parts.push(
      name
        ? t("{{name}} is back as it was before that change. The restore is listed here as a change, so it can be undone.", { name })
        : t("The files are back as they were before that change. The restore is listed here as a change, so it can be undone."),
    );
  } else if (res.skippedTotal === 0) {
    parts.push(name ? t("Nothing to put back: {{name}} is already as it was before that change.", { name }) : t("Nothing to put back: the files are already as they were before that change."));
  }
  if (res.skippedTotal > 0) {
    const named = res.skipped.slice(0, 3).map((s) => `${s.path} (${t(skipWords[s.reason] ?? msg("it could not be replaced"))})`);
    const more = res.skippedTotal - named.length;
    const list = more > 0 ? t("{{list}} and {{count}} more", { list: named.join(", "), count: more }) : named.join(", ");
    parts.push(t("Left as they are: {{list}}.", { list, count: res.skippedTotal }));
  }
  return parts.join(" ");
}

export function fileCount(n: number): string {
  return t("{{count}} files", { count: n });
}

// A file's sizes on the two sides; -1 is the side where it does not exist.
export function sizeChange(oldSize: bigint | number, newSize: bigint | number): string {
  const a = Number(oldSize);
  const b = Number(newSize);
  if (a < 0 && b < 0) return "";
  if (a < 0) return fmtBytes(b);
  if (b < 0) return t("was {{size}}", { size: fmtBytes(a) });
  return a === b ? fmtBytes(b) : `${fmtBytes(a)} → ${fmtBytes(b)}`;
}

// What a file row says in place of a diff, when it has none to show.
export function fileNote(f: { status: string; binary: boolean; large: boolean; oldSize: bigint | number; newSize: bigint | number }, maxFileBytes: bigint | number): string {
  if (f.status === "repo") return t("A git repository of its own. Only the commit it is on is recorded here, not its files.");
  if (f.large) return t("Over {{size}}, so only its size is recorded, not what is in it.", { size: fmtBytes(maxFileBytes) });
  if (f.binary) return t("Not text, so there are no lines to compare.");
  return "";
}

// The ListChanges state, as words; null when changes can be shown.
export function changesNotice(state: string): { text: string; reset: boolean } | null {
  if (state === "off") return { text: t("Change tracking is turned off by the operator (changes.enabled)."), reset: false };
  if (state === "outdated") return { text: t("This Bot's machine was made before change tracking. Reset its container to start recording what changes."), reset: true };
  return null;
}

/** Each look snapshots the workspace on the Bot, so it only polls while watched. */
export function changesPollMs(visible: boolean, connected: boolean): number | false {
  return visible && connected ? 8000 : false;
}

const opWords: Record<string, string> = { added: msg("Added"), modified: msg("Changed"), deleted: msg("Deleted"), renamed: msg("Renamed") };

// What a drive journal line did, as a word.
export function driveOpWord(op: string): string {
  return t(opWords[op] ?? msg("Changed"));
}

// A journal line's path as the Bot sees it under /workspace/drives.
export function drivePath(drive: string, path: string): string {
  return path ? `${drive}/${path}` : drive;
}

// The line under a journal row: what happened, how big, and whose run it was.
export function driveLine(e: { op: string; oldPath: string; size: bigint | number; sources: { kind: string; name: string }[] }): string {
  const parts = [e.op === "renamed" && e.oldPath ? t("Renamed from {{oldPath}}", { oldPath: e.oldPath }) : driveOpWord(e.op)];
  if (Number(e.size) > 0) parts.push(fmtBytes(e.size));
  parts.push(sourceTitle(e.sources));
  return parts.join(" · ");
}

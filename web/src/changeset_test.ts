import { canRestore, changesNotice, changesPollMs, driveLine, driveOpWord, drivePath, fileCount, fileNote, parsePatch, restoreLabel, restoreNotice, sizeChange, sourceHint, sourceTitle } from "./changeset.ts";
import { eq } from "./testing.ts";

// A patch becomes numbered lines: additions and context carry the new number,
// deletions the old one.
const patch = "@@ -2,4 +2,5 @@ func main() {\n ctx\n-old\n+new\n+more\n tail\n\\ No newline at end of file\n@@ -40 +41 @@\n-x\n+y\n";
eq(
  parsePatch(patch),
  [
    { kind: "hunk", text: "func main() {" },
    { kind: "ctx", text: "ctx", no: 2 },
    { kind: "del", text: "old", no: 3 },
    { kind: "add", text: "new", no: 3 },
    { kind: "add", text: "more", no: 4 },
    { kind: "ctx", text: "tail", no: 5 },
    { kind: "note", text: "No newline at end of file" },
    { kind: "hunk", text: "" },
    { kind: "del", text: "x", no: 40 },
    { kind: "add", text: "y", no: 41 },
  ],
  "parsePatch",
);
eq(parsePatch(""), [], "empty patch");
// A line that only looks like a header is still content.
eq(parsePatch("@@ -1 +1 @@\n+@@ not a hunk\n+\n"), [{ kind: "hunk", text: "" }, { kind: "add", text: "@@ not a hunk", no: 1 }, { kind: "add", text: "", no: 2 }], "content that looks like a hunk");

eq(sourceTitle([]), "Outside a run", "no source");
eq(sourceTitle([{ kind: "chat", name: "Tidy up" }]), "Chat “Tidy up”", "one source");
eq(sourceTitle([{ kind: "automation", name: "Weekly" }, { kind: "subagent", name: "scout" }]), "Automation “Weekly”, Subagent “scout”", "two sources");
const tidy = { kind: "chat", name: "Tidy up" };
eq([sourceHint([]) !== "", sourceHint([tidy]), sourceHint([tidy, tidy]) !== ""], [true, "", true], "hints");
// A restore is the owner's doing, whoever else was at work.
eq([sourceTitle([], true), sourceTitle([tidy], true)], ["Restored by you", "Restored by you"], "a restore's title");
eq(sourceHint([], true).includes("Undo"), true, "a restore's hint says it can be undone");
eq(sourceHint([tidy], true).includes("Chat “Tidy up”"), true, "a restore during a run names the run");

// What the button will do to the file, said before it does it.
eq(
  [restoreLabel("added")[0], restoreLabel("deleted")[0], restoreLabel("renamed")[0], restoreLabel("modified")[0]],
  ["Remove this file", "Bring this file back", "Move this file back", "Restore this file"],
  "restore labels",
);
eq(restoreLabel("added")[1].startsWith("Click again"), true, "armed label");
eq(
  [canRestore({ status: "modified", large: false }), canRestore({ status: "modified", large: true }), canRestore({ status: "repo", large: false })],
  [true, false, false],
  "what the history can put back",
);

const none = { restored: 0, skipped: [], skippedTotal: 0 };
eq(restoreNotice({ ...none, restored: 3 }, ""), "The files are back as they were before that change. The restore is listed here as a change, so it can be undone.", "a change restored");
eq(restoreNotice({ ...none, restored: 1 }, "src/app.py").startsWith("app.py is back as it was before that change."), true, "one file restored");
eq(restoreNotice(none, ""), "Nothing to put back: the files are already as they were before that change.", "nothing to do");
eq(restoreNotice(none, "a/b.txt"), "Nothing to put back: b.txt is already as it was before that change.", "nothing to do for a file");
eq(
  restoreNotice({ restored: 0, skipped: [{ path: "video.bin", reason: "large" }], skippedTotal: 1 }, "video.bin"),
  "Left as it is: video.bin (over the size limit, so what was in it was never kept).",
  "a file that cannot be put back",
);
eq(
  restoreNotice(
    {
      restored: 2,
      skipped: [
        { path: "a.bin", reason: "large" },
        { path: "vendor/lib", reason: "repo" },
        { path: "data", reason: "blocked" },
        { path: "x", reason: "new-reason" },
      ],
      skippedTotal: 9,
    },
    "",
  ),
  "The files are back as they were before that change. The restore is listed here as a change, so it can be undone. Left as they are: a.bin (over the size limit, so what was in it was never kept), vendor/lib (a git repository of its own), data (something else is in its place) and 6 more.",
  "some restored, some skipped",
);

eq([fileCount(1), fileCount(0), fileCount(12)], ["1 file", "0 files", "12 files"], "file count");

eq([sizeChange(-1, 2048), sizeChange(2048, -1), sizeChange(1024, 2048), sizeChange(512n, 512n), sizeChange(-1, -1)], ["2.0 KB", "was 2.0 KB", "1.0 KB → 2.0 KB", "512 B", ""], "sizes");

const f = { status: "modified", binary: false, large: false, oldSize: 1, newSize: 2 };
eq(fileNote(f, 8 << 20), "", "a text file has a diff");
eq(fileNote({ ...f, large: true }, 8 << 20).startsWith("Over 8.0 MB"), true, "large");
eq(fileNote({ ...f, binary: true }, 8 << 20) !== "", true, "binary");
eq(fileNote({ ...f, status: "repo", large: true }, 8 << 20).includes("repository"), true, "a repository wins");

eq([changesNotice("ok"), changesNotice("off")?.reset, changesNotice("outdated")?.reset], [null, false, true], "notices");
eq([changesPollMs(true, true), changesPollMs(false, true), changesPollMs(true, false)], [8000, false, false], "poll only while watched");

eq([driveOpWord("added"), driveOpWord("modified"), driveOpWord("deleted"), driveOpWord("renamed"), driveOpWord("?")], ["Added", "Changed", "Deleted", "Renamed", "Changed"], "drive op words");
eq([drivePath("files", "a/b.txt"), drivePath("files", "")], ["files/a/b.txt", "files"], "drive paths");
eq(driveLine({ op: "added", oldPath: "", size: 2048n, sources: [{ kind: "chat", name: "Tidy up" }] }), "Added · 2.0 KB · Chat “Tidy up”", "an upload");
eq(driveLine({ op: "renamed", oldPath: "a.txt", size: 0, sources: [] }), "Renamed from a.txt · Outside a run", "a rename");
eq(driveLine({ op: "deleted", oldPath: "", size: 0, sources: [] }), "Deleted · Outside a run", "a delete");

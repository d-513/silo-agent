import { changesNotice, changesPollMs, fileCount, fileNote, parsePatch, sizeChange, sourceHint, sourceTitle } from "./changeset.ts";
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
eq([sourceHint([]) !== "", sourceHint([1]), sourceHint([1, 2]) !== ""], [true, "", true], "hints");

eq([fileCount(1), fileCount(0), fileCount(12)], ["1 file", "0 files", "12 files"], "file count");

eq([sizeChange(-1, 2048), sizeChange(2048, -1), sizeChange(1024, 2048), sizeChange(512n, 512n), sizeChange(-1, -1)], ["2.0 KB", "was 2.0 KB", "1.0 KB → 2.0 KB", "512 B", ""], "sizes");

const f = { status: "modified", binary: false, large: false, oldSize: 1, newSize: 2 };
eq(fileNote(f, 8 << 20), "", "a text file has a diff");
eq(fileNote({ ...f, large: true }, 8 << 20).startsWith("Over 8.0 MB"), true, "large");
eq(fileNote({ ...f, binary: true }, 8 << 20) !== "", true, "binary");
eq(fileNote({ ...f, status: "repo", large: true }, 8 << 20).includes("repository"), true, "a repository wins");

eq([changesNotice("ok"), changesNotice("off")?.reset, changesNotice("outdated")?.reset], [null, false, true], "notices");
eq([changesPollMs(true, true), changesPollMs(false, true), changesPollMs(true, false)], [8000, false, false], "poll only while watched");

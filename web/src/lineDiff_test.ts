import { lineDiff } from "./lineDiff.ts";

function eq<T>(got: T, want: T, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

eq(lineDiff("a\nb\nc\n", "a\nB\nc\n").text, " a\n-b\n+B\n c", "one line changed");
eq(lineDiff("a\nb\n", "a\nb\n").stat, { added: 0, removed: 0 }, "nothing changed");
eq(lineDiff("", "x\ny").stat, { added: 2, removed: 0 }, "a new file");
const long = Array.from({ length: 20 }, (_, i) => `l${i}`).join("\n");
const edited = long.replace("l10", "L10");
const d = lineDiff(long, edited);
eq(d.stat, { added: 1, removed: 1 }, "one edit in the middle");
eq(d.text.split("\n")[0], "@@ 7 @@", "folded head");
eq(d.text.split("\n").length, 1 + 3 + 2 + 3 + 1, "context around the edit");
console.log("ok");

import { FILTERS, kindWord, shows } from "./memoryView.ts";

function eq(got: unknown, want: unknown, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

const rows = [
  { id: "f", kind: "fact", status: "active" },
  { id: "old", kind: "", status: "active" },
  { id: "p", kind: "preference", status: "active" },
  { id: "l", kind: "lesson", status: "active" },
  { id: "w", kind: "work", status: "active" },
  { id: "guess", kind: "preference", status: "tentative" },
  { id: "gone", kind: "fact", status: "retired" },
];
const under = (f: Parameters<typeof shows>[0]) => rows.filter((m) => shows(f, m)).map((m) => m.id);

eq(under("all"), ["f", "old", "p", "l", "w", "guess"], "All leaves out what was retired");
eq(under("fact"), ["f", "old"], "a memory with no kind is a fact");
eq(under("preference"), ["p"], "a noticed preference is not yet a preference");
eq(under("lesson"), ["l"], "lessons");
eq(under("work"), ["w"], "work");
eq(under("noticed"), ["guess"], "noticed");
eq(under("retired"), ["gone"], "retired");
eq(FILTERS.map((f) => f.key), ["all", "preference", "lesson", "fact", "work", "noticed", "retired"], "chip order");
eq([kindWord("fact"), kindWord("preference"), kindWord("lesson"), kindWord("work"), kindWord("")], ["", "preference", "lesson", "work", ""], "kind words");

console.log("memoryView ok");

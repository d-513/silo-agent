import { addQuote, clip, lineLocator, quoteTitle, regionLocator, MAX_QUOTES } from "./model.ts";

function eq<T>(got: T, want: T, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

const doc = "one\ntwo\nthree\nfour\n";
eq(lineLocator(doc, "two\nthree"), "lines 2–3", "two lines");
eq(lineLocator(doc, "  four "), "line 4", "one line, trimmed");
eq(lineLocator(doc, "five"), "", "not in the file");
eq(lineLocator("", "x"), "", "no text");

eq(regionLocator({ x: 0.1234, y: 0, w: 1.5, h: -1 }), "0.123,0,1,0", "clamped fractions");

eq(quoteTitle({ source: "message", ref: "reply", label: "x" }), "Reply", "the Bot's words");
eq(quoteTitle({ source: "message", ref: "own", label: "" }), "Your message", "the human's words");
eq(quoteTitle({ source: "file", ref: "a/b.md", label: "" }), "a/b.md", "a file without a label");

eq(clip("a".repeat(10), 20), "a".repeat(10), "short");
eq(clip("aaaa\nbbbb\ncccc", 12), "aaaa\nbbbb…", "cut at a line");

let list = addQuote([], { source: "message", ref: "reply", label: "", text: " hi ", locator: "" });
eq(list.length, 1, "added");
eq(list[0].text, "hi", "trimmed");
list = addQuote(list, { source: "message", ref: "reply", label: "", text: "hi", locator: "" });
eq(list.length, 1, "the same quote once");
list = addQuote(list, { source: "message", ref: "reply", label: "", text: "  ", locator: "" });
eq(list.length, 1, "empty is nothing");
for (let i = 0; i < 20; i++) list = addQuote(list, { source: "file", ref: "f", label: "", text: `t${i}`, locator: "" });
eq(list.length, MAX_QUOTES, "capped");

console.log("ok");

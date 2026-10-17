import { filterRows, guessDelim, parseCsv, sortRows } from "./csv.ts";

function eq<T>(got: T, want: T, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

eq(parseCsv('a,b\n"x, y","say ""hi"""\n'), [["a", "b"], ["x, y", 'say "hi"']], "quotes");
eq(parseCsv('a,b\r\n"two\nlines",2'), [["a", "b"], ["two\nlines", "2"]], "a break inside quotes");
eq(guessDelim("a\tb\tc\n1\t2\t3"), "\t", "tabs");
eq(guessDelim("a;b;c"), ";", "semicolons");
const rows = [["b", "10"], ["a", "9"], ["c", "100"]];
eq(sortRows(rows, 1, 1).map((r) => r[1]), ["9", "10", "100"], "numbers as numbers");
eq(sortRows(rows, 0, -1).map((r) => r[0]), ["c", "b", "a"], "text, descending");
eq(filterRows(rows, " A "), [["a", "9"]], "filter ignores case");
console.log("ok");

import assert from "node:assert/strict";
import { actions, age, bars, checked, columnsFor, interval, nowSplit, numeric, obj, receipt, reorder, safeUrl, span, sparkPoints, text, toggle } from "./widgets.ts";

assert.deepEqual(obj('{"a":1}'), { a: 1 });
assert.deepEqual(obj("[1]"), {});
assert.deepEqual(obj("nope"), {});

assert.equal(columnsFor(900), 4);
assert.equal(columnsFor(600), 2);
assert.equal(columnsFor(300), 1);
assert.equal(span("s", 4), 1);
assert.equal(span("m", 4), 2);
assert.equal(span("l", 4), 4);
assert.equal(span("l", 2), 2);
assert.equal(span("m", 1), 1);

const now = new Date("2026-10-17T12:00:00Z");
assert.equal(age("2026-10-17T11:59:30Z", now), "now");
assert.deepEqual(age("2026-10-17T11:56:00Z", now), { n: 4, unit: "m" });
assert.deepEqual(age("2026-10-17T09:00:00Z", now), { n: 3, unit: "h" });
assert.deepEqual(age("2026-10-14T12:00:00Z", now), { n: 3, unit: "d" });
assert.equal(age("", now), null);
assert.deepEqual(interval(900), { n: 15, unit: "m" });
assert.deepEqual(interval(7200), { n: 2, unit: "h" });
assert.deepEqual(interval(86400), { n: 1, unit: "d" });
assert.equal(interval(0), null);

assert.equal(text("a"), "a");
assert.equal(text(3), "3");
assert.equal(text(true), "", "a yes/no is the page's to word");

assert.deepEqual(
  actions(
    JSON.stringify({
      actions: [
        { id: "rm", label: "Remove", kind: "run", row: true, confirm: true },
        { id: "ask", label: "Ask", kind: "hand" },
        { id: "", label: "x" },
      ],
    }),
  ),
  [
    { id: "rm", label: "Remove", kind: "run", row: true, confirm: true },
    { id: "ask", label: "Ask", kind: "hand", row: false, confirm: false },
  ],
);

const pts = sparkPoints([1, 2, 3], 100, 20, 0);
assert.deepEqual(pts, [
  [0, 20],
  [50, 10],
  [100, 0],
]);
assert.deepEqual(sparkPoints([5], 100, 20), []);
assert.deepEqual(bars([2, 4, -1]), [0.5, 1, 0]);

const day = (h: number) => new Date(Date.UTC(2026, 9, 17, h)).toISOString();
assert.equal(nowSplit([day(8), day(11), day(14)], now), 2);
assert.equal(nowSplit(["2026-10-20T08:00:00Z"], now), -1, "nothing today: no rule");
assert.equal(nowSplit(["09:30"], now), -1, "unreadable time: no rule");

assert.equal(safeUrl("https://example.com/a"), "https://example.com/a");
assert.equal(safeUrl("javascript:alert(1)"), "");
assert.equal(safeUrl(3), "");

assert.deepEqual(checked('{"checked":["a"]}'), ["a"]);
assert.deepEqual(checked("{}"), []);
assert.deepEqual(JSON.parse(toggle('{"checked":["a"],"x":1}', "b")), { checked: ["a", "b"], x: 1 });
assert.deepEqual(JSON.parse(toggle('{"checked":["a"]}', "a")), { checked: [] });

assert.equal(receipt(""), null);
assert.deepEqual(receipt('{"text":"Removed 3","ok":true,"at":"t"}'), { text: "Removed 3", ok: true, at: "t" });

assert.deepEqual(reorder(["a", "b", "c"], "a", "c"), ["b", "c", "a"]);
assert.deepEqual(reorder(["a", "b", "c"], "c", "a"), ["c", "a", "b"]);
assert.deepEqual(reorder(["a", "b"], "a", "a"), ["a", "b"]);

assert.equal(numeric("3.1 GB"), true);
assert.equal(numeric(412), true);
assert.equal(numeric("12%"), true);
assert.equal(numeric("datasets/osm"), false);
assert.equal(numeric("-"), false);

console.log("widgets ok");

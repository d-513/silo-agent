import { byDay, chips, isLong, matches, opensWith, plain, shows, summary } from "./feedView.ts";

function eq(got: unknown, want: unknown, what: string) {
  if (JSON.stringify(got) !== JSON.stringify(want)) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

const post = (id: string, sourceKind: string, createdAt = "", title = "", body = "x", sourceName = "") => ({ id, title, body, sourceKind, sourceName, createdAt });

// Chips.
const mixed = [post("a", "automation"), post("b", "main"), post("c", "chat"), post("d", "room")];
eq(mixed.filter((p) => shows("chat", p)).map((p) => p.id), ["b", "c"], "the main chat is a chat");
eq(mixed.filter((p) => shows("all", p)).length, 4, "All shows a source with no chip");
eq(chips(mixed).map((c) => [c.key, c.n]), [["all", 4], ["automation", 1], ["chat", 2]], "a chip per source that has a post");
eq(chips([post("a", "automation"), post("b", "automation")]), [], "one source needs no chips");
eq(chips([]), [], "no posts, no chips");

// Markdown as words.
eq(plain("## Done\n\n- **Papyrus Lab** dropped `A5` by 6%.\n- See [the sheet](https://x.test/a)."), "Done Papyrus Lab dropped A5 by 6%. See the sheet.", "marks go, words stay");
eq(plain("| a | b |\n|---|---|\n| 1 | 2 |"), "a b 1 2", "a table reads as its cells");
eq(plain("snake_case_name and 2 * 3 * 4"), "snake_case_name and 2 * 3 * 4", "an underscore or star inside words is not emphasis");
eq(plain("It was *really* slow."), "It was really slow.", "emphasis is its word");

// A closed row.
eq(summary({ title: "Digest", body: "**Three** things happened." }), { head: "Digest", rest: "Three things happened." }, "a title heads the row, the body follows");
eq(summary({ title: "", body: "\n# Nightly check\n\nNothing found." }), { head: "Nightly check", rest: "Nothing found." }, "with no title the first line heads the row");
eq(summary({ title: "", body: "Booked The Quay." }), { head: "Booked The Quay.", rest: "" }, "one line is said once");
eq(summary({ title: "", body: "   " }), { head: "", rest: "" }, "an empty body heads nothing");
const long = summary({ title: "", body: "w".repeat(400) });
eq([long.head.length, long.head.endsWith("…")], [110, true], "a long line is cut");

// What is cut.
eq([isLong("Two lines.\nThat is all."), isLong("w".repeat(901)), isLong(Array(13).fill("- x").join("\n"))], [false, true, true], "a post is cut past 900 characters or 12 lines");

// Search.
const p = post("a", "automation", "", "Morning brief", "Two **price** changes", "Competitors");
eq([matches(p, "price brief"), matches(p, "PRICE"), matches(p, "competitors"), matches(p, "price nothing"), matches(p, "  ")], [true, true, true, false, true], "every word, anywhere, in any case");

// Days.
const now = new Date(2026, 10, 27, 15, 0);
const at = (d: number, h: number) => new Date(2026, 10, d, h, 0).toISOString();
const days = byDay([post("1", "chat", at(27, 16)), post("2", "chat", at(27, 9)), post("3", "chat", at(26, 23)), post("4", "chat", at(20, 8)), post("5", "chat", "nonsense")], now);
eq(days.map((d) => [d.key, d.days, d.posts.map((x) => x.id)]), [["d0", 0, ["1", "2"]], ["d1", 1, ["3"]], ["d7", 7, ["4"]], ["undated", -1, ["5"]]], "one group per calendar day, newest first");

// What starts open.
eq(opensWith(["a", "b", "c"]), ["a", "b", "c"], "a few new posts all open");
eq(opensWith(["a", "b", "c", "d"]), ["a"], "a backlog opens its newest");
eq(opensWith([]), [], "nothing new, nothing open");

console.log("feedView ok");

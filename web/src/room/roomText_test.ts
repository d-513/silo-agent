import { eq } from "../testing.ts";
import { anyWorking, eligible, isPass, mention, onBot, parts, passSoFar } from "./roomText.ts";

const bots = [
  { id: "r", name: "Research" },
  { id: "rb", name: "Research Bot" },
  { id: "w", name: "Writer" },
];

// A mention is the name whole, in any case, the longest name first.
eq(parts("ask @writer and @Research Bot", bots), [{ text: "ask " }, { text: "@writer", mention: "w" }, { text: " and " }, { text: "@Research Bot", mention: "rb" }], "mentions");
eq(parts("@Writers is nobody", bots), [{ text: "@Writers is nobody" }], "not a whole name");
eq(parts("mail me at ann@writer.com", bots), [{ text: "mail me at ann@writer.com" }], "an address names nobody");
eq(parts("@all, look", bots), [{ text: "@all", mention: "all" }, { text: ", look" }], "everyone");
eq(parts("plain", []), [{ text: "plain" }], "no Bots");

// The composer's chips add a name once.
eq(mention("", "Writer", bots), "@Writer ", "empty draft");
eq(mention("hello", "Writer", bots), "hello @Writer ", "after a word");
eq(mention("hello @writer ", "Writer", bots), "hello @writer ", "already named");

// Everyone in a room is on every Bot in it; a Bot that lists nobody is mine alone.
eq(onBot({ id: "a", people: [] }, "me", "me"), true, "mine alone");
eq(onBot({ id: "a", people: [] }, "bob", "me"), false, "not bob's");
eq(
  eligible(["bob", "ann"], [{ id: "a", people: ["me", "bob", "ann"] }, { id: "b", people: ["me", "ann"] }], "me", { a: "Alpha", b: "Beta" }),
  [
    { userId: "bob", why: "Beta" },
    { userId: "ann", why: "" },
  ],
  "eligible",
);

eq([anyWorking([]), anyWorking([{ working: false }, { working: true }])], [false, true], "working");

// A pass is the whole reply; while it streams, its start already is one.
eq([isPass("<pass/>"), isPass(" <pass/>\n"), isPass("<pass/> but 4"), isPass("")], [true, true, false, false], "pass");
eq([passSoFar("<pa"), passSoFar("<"), passSoFar("<pre>"), passSoFar("<pass/>")], [true, false, false, true], "pass so far");

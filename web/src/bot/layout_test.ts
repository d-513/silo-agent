import { barKeys, chatKey, chatOf, moveBefore, neighbour, railKeys, routeKey, splitAfterMove, splittable, type Avail, type TabKey } from "./layout.ts";
import { eq } from "../testing.ts";

const avail: Avail = { main: true, mail: false, runs: true, chats: new Set(["a", "b", "c"]) };

// The route names one tab.
eq(
  [
    routeKey("run", "m", false, "m"),
    routeKey("run", "a", false, "m"),
    routeKey("run", undefined, true, "m"),
    routeKey("run", undefined, false, "m"),
    routeKey("rules", undefined, false, "m"),
    routeKey("desktop", undefined, false, "m"),
    routeKey("feed", undefined, false, "m"),
  ],
  ["main", "chat:a", "new", "main", "settings", "desktop", "feed"],
  "route to key",
);
eq([chatOf("chat:x"), chatOf("main"), chatOf("")], ["x", "", ""], "chat of a key");

// Dashboard and the main chat are pinned by default; the rail keeps the rest.
const rail = railKeys(["dashboard", "main"], avail);
eq(rail.pages, ["automations", "artifacts", "memories", "knowledge", "feed", "channels"], "rail pages");
eq(rail.machine, ["files", "desktop", "console", "changes"], "mail waits for an address");
eq(rail.foot, ["settings"], "settings at the foot");
eq(railKeys(["main", "files"], avail).pages[0], "dashboard", "an unpinned Dashboard goes to the rail");
eq(railKeys(["files"], avail).machine, ["desktop", "console", "changes"], "a pinned pane leaves it");

// The bar: pinned first, then what was opened and the open chats, in your order.
const bar = barKeys({ pinned: ["dashboard", "main", "chat:gone"], order: ["memories", "chat:b", "desktop"], openChats: ["a", "b"], current: "", avail });
eq(bar.pinned, ["dashboard", "main"], "a pinned chat that is gone is not shown");
eq(bar.strip, ["memories", "chat:b", "desktop", "chat:a"], "order, then chats not yet placed");
eq(barKeys({ pinned: ["main"], order: [], openChats: [], current: "feed", avail }).strip, ["feed"], "the open page is a tab");
eq(barKeys({ pinned: ["main"], order: [], openChats: [], current: "main", avail }).strip, [], "a pinned page is not twice");
eq(barKeys({ pinned: ["dashboard"], order: ["chat:a"], openChats: [], current: "", avail }).strip, [], "a closed chat drops out");
eq(barKeys({ pinned: ["chat:a"], order: [], openChats: ["a"], current: "", avail }), { pinned: ["chat:a"], strip: [] }, "a pinned chat heads the bar");
eq(barKeys({ pinned: [], order: ["mail"], openChats: [], current: "", avail }).strip, [], "mail without an address");

// Moving tabs around.
const list: TabKey[] = ["a" as never, "b" as never, "c" as never].map((x) => chatKey(x));
eq(moveBefore(list, "chat:c", "chat:a"), ["chat:c", "chat:a", "chat:b"], "move before");
eq(moveBefore(list, "chat:a", ""), ["chat:b", "chat:c", "chat:a"], "move to the end");
eq(moveBefore(list, "feed", "chat:b"), ["chat:a", "feed", "chat:b", "chat:c"], "put in");
eq([neighbour(list, "chat:a"), neighbour(list, "chat:c"), neighbour(["feed"], "feed"), neighbour(list, "feed")], ["chat:b", "chat:b", "", ""], "neighbour");

// Opening the tab in the split swaps the two.
eq(splitAfterMove("files", "main", "files"), "main", "swap");
eq(splitAfterMove("files", "new", "files"), "", "the empty chat is never split");
eq(splitAfterMove("files", "main", "feed"), "files", "any other move keeps it");
eq([splittable("new"), splittable("main"), splittable("")], [false, true, false], "splittable");

console.log("ok");

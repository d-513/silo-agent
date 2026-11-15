import { afterClose, chatTabs, closedChats } from "./chatTabs.ts";
import { eq } from "../testing.ts";

const chat = (id: string, day: number, o: Partial<{ main: boolean; mine: boolean; hidden: boolean }> = {}) => ({
  id,
  main: false,
  mine: true,
  hidden: false,
  createdAt: `2026-11-${String(day).padStart(2, "0")}T10:00:00Z`,
  ...o,
});
const ids = (xs: { id: string }[]) => xs.map((x) => x.id);

// The list arrives by last activity; tabs keep the order the chats were made.
const chats = [chat("c", 3), chat("main", 1, { main: true }), chat("a", 1), chat("closed", 2, { hidden: true }), chat("theirs", 2, { mine: false }), chat("b", 2)];
eq(ids(chatTabs(chats)), ["a", "b", "c"], "yours, open, oldest first");
eq(ids(chatTabs(chats, "b")), ["a", "b", "c"], "the open tab keeps its place");
eq(ids(chatTabs(chats, "closed")), ["a", "b", "c", "closed"], "a closed chat is a tab while it is open");
eq(ids(chatTabs(chats, "theirs")), ["a", "b", "c", "theirs"], "so is someone else's");
eq(ids(chatTabs(chats, "main")), ["a", "b", "c"], "the main chat has its own tab");
eq(ids(chatTabs([...chats, chat("their-main", 1, { main: true, mine: false })], "their-main")), ["a", "b", "c", "their-main"], "someone else's main chat is a tab while open");
eq(ids(chatTabs(chats, "gone")), ["a", "b", "c"], "an unknown chat adds nothing");
eq(ids(closedChats(chats)), ["closed"], "behind the arrow");

// Closing a tab opens its neighbour, the one after first.
const tabs = [{ id: "a" }, { id: "b" }, { id: "c" }];
eq([afterClose(tabs, "a"), afterClose(tabs, "b"), afterClose(tabs, "c"), afterClose([{ id: "a" }], "a"), afterClose(tabs, "x")], ["b", "c", "b", "", ""], "after a close");

console.log("ok");

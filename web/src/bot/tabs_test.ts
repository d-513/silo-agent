import { clampShare } from "./paneStore.ts";
import { onChatSide, onCustomize, onSettings, paneOf } from "./tabs.ts";
import { eq } from "../testing.ts";

// Every page belongs to exactly one place.
eq([onChatSide("run"), onChatSide("feed"), onChatSide("mail"), onChatSide("rules"), onChatSide("files")], [true, true, true, false, false], "chat side");
eq([onCustomize("connectors"), onCustomize("channels"), onCustomize("secrets"), onCustomize("run")], [true, true, false, false], "customize");
eq([onSettings("settings"), onSettings("container"), onSettings("tunnels"), onSettings("drives")], [true, true, true, false], "settings");

// Only the machine panes have a page that is the pane itself.
eq([paneOf("files"), paneOf("desktop"), paneOf("console"), paneOf("run"), paneOf("settings")], ["files", "desktop", "console", "", ""], "pane of a tab");

// The docked pane never squeezes the chat, or itself, out of the row.
eq([clampShare(0.5), clampShare(0), clampShare(2), clampShare(Number.NaN)], [0.5, 0.25, 0.75, 0.5], "share");

console.log("ok");

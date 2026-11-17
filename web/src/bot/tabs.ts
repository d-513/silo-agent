// Every page of a Bot lights one of these (router.tsx `staticData.tab`). They
// fall into four places: the conversations (the chat and the Dashboard, which
// are the tab bar's), the pages beside them that the right rail opens
// (Automations, Artifacts, Memories, Knowledge, Feed, and Channels), Settings
// and its tabs, and the panes the rail docks beside the page: the machine's
// Files, Desktop, Console and Changes (what changed in the workspace), and
// Mail (the Bot's inbox, which needs no machine).
export const sideTabs = ["dashboard", "automations", "artifacts", "memories", "knowledge", "feed"] as const;
export type SideTab = (typeof sideTabs)[number];
export const settingsTabs = ["settings", "people", "rules", "secrets", "tunnels", "container", "audit"] as const;
export type SettingsTab = (typeof settingsTabs)[number];
export const paneKinds = ["files", "desktop", "console", "changes", "mail"] as const;
export type PaneKind = (typeof paneKinds)[number];
// Channels is the Bot's own page: what talks to it. It keeps its full width
// (no docked pane). What a Bot shares with its Workspace (connectors, skills,
// drives) is the Workspace's.
// Runs (everything the Bot did, for its managers) is on the conversation
// side too, but nothing in the bar or the rail is lit for it: it opens from
// the closed chats' menu.
export type Tab = "run" | "runs" | SideTab | "channels" | SettingsTab | PaneKind;

const has = (xs: readonly string[], t: Tab) => xs.includes(t);

export function onChatSide(t: Tab) {
  return t === "run" || t === "runs" || has(sideTabs, t);
}

export function onSettings(t: Tab) {
  return has(settingsTabs, t);
}

/** The machine pane a tab is the dedicated page of, if it is one. */
export function paneOf(t: Tab): PaneKind | "" {
  return has(paneKinds, t) ? (t as PaneKind) : "";
}

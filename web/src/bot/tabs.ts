// Every page of a Bot lights one of these (router.tsx `staticData.tab`). They
// fall into four places: the chat and the conversation-side pages above the
// chats list, the two sections the sidebar opens (Customize, Settings), and the
// panes the right rail docks beside the chat — the machine's Files, Desktop and
// Console, and Mail (the Bot's inbox, which needs no machine).
export const sideTabs = ["automations", "memories", "knowledge", "feed"] as const;
export type SideTab = (typeof sideTabs)[number];
export const customizeTabs = ["connectors", "skills", "drives", "channels"] as const;
export type CustomizeTab = (typeof customizeTabs)[number];
export const settingsTabs = ["settings", "rules", "secrets", "tunnels", "container"] as const;
export type SettingsTab = (typeof settingsTabs)[number];
export const paneKinds = ["files", "desktop", "console", "mail"] as const;
export type PaneKind = (typeof paneKinds)[number];
export type Tab = "run" | SideTab | CustomizeTab | SettingsTab | PaneKind;

const has = (xs: readonly string[], t: Tab) => xs.includes(t);

export function onChatSide(t: Tab) {
  return t === "run" || has(sideTabs, t);
}

export function onCustomize(t: Tab) {
  return has(customizeTabs, t);
}

export function onSettings(t: Tab) {
  return has(settingsTabs, t);
}

/** The machine pane a tab is the dedicated page of, if it is one. */
export function paneOf(t: Tab): PaneKind | "" {
  return has(paneKinds, t) ? (t as PaneKind) : "";
}

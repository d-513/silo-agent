export const tabs = ["run", "desktop", "files", "drives", "connectors", "channels", "skills", "secrets", "rules", "container", "settings"] as const;
export type NavTab = (typeof tabs)[number];
// Side tabs live in the chat sidebar (the conversation lifecycle); the top
// strip is config and machine. The Chat tab stays lit on all of them.
export const sideTabs = ["automations", "memories", "knowledge", "feed"] as const;
export type SideTab = (typeof sideTabs)[number];
export type Tab = NavTab | SideTab | "console";

export function isNavTab(s: string | undefined): s is NavTab {
  return !!s && (tabs as readonly string[]).includes(s);
}

export function isSideTab(s: string | undefined): s is SideTab {
  return !!s && (sideTabs as readonly string[]).includes(s);
}

export function onChatSide(t: Tab) {
  return t === "run" || isSideTab(t);
}

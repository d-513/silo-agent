import { linkOptions } from "@tanstack/react-router";
import {
  Brain,
  Folder,
  GitCompareArrows,
  Inbox,
  LayoutGrid,
  Library,
  ListTree,
  Mail,
  MessageCircle,
  Monitor,
  Package,
  Radio,
  Settings,
  SquareTerminal,
  Timer,
  type LucideIcon,
} from "lucide-react";
import type { Chat } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";
import { chatLink, newChatLink } from "../links";
import { chatOf, type PageKey, type TabKey } from "./layout";

// What each page key is called and drawn with, wherever it stands: a tab, the
// rail, a menu, the split's head.
export const keyMeta: Record<PageKey, { label: string; icon: LucideIcon }> = {
  dashboard: { label: msg("Dashboard"), icon: LayoutGrid },
  main: { label: msg("Main chat"), icon: MessageCircle },
  automations: { label: msg("Automations"), icon: Timer },
  artifacts: { label: msg("Artifacts"), icon: Package },
  memories: { label: msg("Memories"), icon: Brain },
  knowledge: { label: msg("Knowledge"), icon: Library },
  feed: { label: msg("Feed"), icon: Inbox },
  channels: { label: msg("Channels"), icon: Radio },
  files: { label: msg("Files"), icon: Folder },
  desktop: { label: msg("Desktop"), icon: Monitor },
  console: { label: msg("Console"), icon: SquareTerminal },
  changes: { label: msg("Changes"), icon: GitCompareArrows },
  mail: { label: msg("Mail"), icon: Mail },
  settings: { label: msg("Settings"), icon: Settings },
  runs: { label: msg("Runs"), icon: ListTree },
};

/** A key's name in the reader's language: a chat by its title. */
export function keyLabel(k: TabKey, chats: readonly Chat[]): string {
  if (k === "new") return t("New chat");
  const cid = chatOf(k);
  if (cid) {
    const c = chats.find((x) => x.id === cid);
    return c?.main ? t("Main chat") : c?.title || t("New chat");
  }
  return t(keyMeta[k as PageKey].label);
}

export function keyIcon(k: TabKey): LucideIcon {
  return chatOf(k) || k === "new" ? MessageCircle : keyMeta[k as PageKey].icon;
}

/** Where a key leads. */
export function keyLink(k: TabKey, botId: string, mainId: string | undefined) {
  const params = { botId };
  switch (k) {
    case "main":
      return chatLink(botId, mainId);
    case "new":
      return newChatLink(botId);
    case "dashboard":
      return linkOptions({ to: "/bots/$botId/dashboard", params });
    case "automations":
      return linkOptions({ to: "/bots/$botId/automations", params });
    case "artifacts":
      return linkOptions({ to: "/bots/$botId/artifacts", params });
    case "memories":
      return linkOptions({ to: "/bots/$botId/memories", params });
    case "knowledge":
      return linkOptions({ to: "/bots/$botId/knowledge", params });
    case "feed":
      return linkOptions({ to: "/bots/$botId/feed", params });
    case "channels":
      return linkOptions({ to: "/bots/$botId/channels", params });
    case "files":
      return linkOptions({ to: "/bots/$botId/files", params });
    case "desktop":
      return linkOptions({ to: "/bots/$botId/desktop", params });
    case "console":
      return linkOptions({ to: "/bots/$botId/console", params });
    case "changes":
      return linkOptions({ to: "/bots/$botId/changes", params });
    case "mail":
      return linkOptions({ to: "/bots/$botId/mail", params });
    case "settings":
      return linkOptions({ to: "/bots/$botId/settings", params });
    case "runs":
      return linkOptions({ to: "/bots/$botId/runs", params });
  }
  return chatLink(botId, chatOf(k));
}

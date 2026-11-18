import { Columns2, Maximize2, PanelRightClose, Pencil, Pin, PinOff, SquareArrowOutUpRight, X } from "lucide-react";
import type { Chat } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { chatOf, pinnable, splittable, type TabKey } from "./layout";
import type { MenuItem } from "./TabMenu";

// TabControl is what the tab bar, the rail and the split may do with a tab.
// BotPage makes it; every place a tab stands asks it, so a tab behaves the
// same wherever it is.
export type TabControl = {
  botId: string;
  mainId: string | undefined;
  chats: readonly Chat[];
  // The page open now, and the tab shown in the split ("" with none).
  current: TabKey | "";
  split: TabKey | "";
  pinned: readonly TabKey[];
  // There is room for a split (the wide layout).
  wide: boolean;
  open: (k: TabKey) => void;
  close: (k: TabKey) => void;
  pin: (k: TabKey, before?: TabKey | "") => void;
  unpin: (k: TabKey) => void;
  toSplit: (k: TabKey) => void;
  unsplit: () => void;
  // A dragged tab let go on the bar: among the pinned, or in the strip.
  drop: (k: TabKey, group: "pinned" | "strip", before: TabKey | "") => void;
  rename: (c: Chat) => void;
};

/** The context menu of a tab, wherever it stands. */
export function tabMenu(k: TabKey, c: TabControl, where: "bar" | "rail" | "split"): MenuItem[] {
  const pinned = c.pinned.includes(k);
  const chat = c.chats.find((x) => x.id === chatOf(k));
  const items: MenuItem[] = [];
  if (where === "rail" && k !== c.current) items.push({ label: t("Open as a tab"), icon: SquareArrowOutUpRight, onSelect: () => c.open(k) });
  if (where === "split") items.push({ label: t("Open as the page"), icon: Maximize2, onSelect: () => c.open(k) });
  if (c.wide && splittable(k)) {
    items.push(
      k === c.split
        ? { label: t("Close split view"), icon: PanelRightClose, onSelect: c.unsplit }
        : { label: t("Open in split view"), icon: Columns2, onSelect: () => c.toSplit(k) },
    );
  }
  if (pinnable(k)) {
    items.push(
      pinned ? { label: t("Unpin from the tab bar"), icon: PinOff, onSelect: () => c.unpin(k) } : { label: t("Pin to the tab bar"), icon: Pin, onSelect: () => c.pin(k) },
    );
  }
  items.push("sep");
  if (chat?.mine && !chat.main && !chat.fromBot) items.push({ label: t("Rename"), icon: Pencil, onSelect: () => c.rename(chat) });
  if (where === "bar" && !pinned) items.push({ label: t("Close tab"), icon: X, onSelect: () => c.close(k) });
  return items;
}

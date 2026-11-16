// Which of a Bot's chats are tabs. Pure, so node can test it
// (tabOrder_test.ts).

type Row = { id: string; main: boolean; mine: boolean; hidden: boolean; createdAt: string };

const made = (c: Row) => Date.parse(c.createdAt) || 0;

/**
 * The side chats shown as tabs, after Dashboard and the main chat: yours
 * that are not closed, in the order they were made, so a new one opens at the
 * end and a reply never moves a tab. The open chat is a tab for as long as it
 * is open, even a closed one or someone else's (their main chat too); it
 * sits last. Your own main chat has its standing tab.
 */
export function chatTabs<T extends Row>(chats: readonly T[], openId?: string): T[] {
  const tabs = chats
    .filter((c) => c.mine && !c.main && !c.hidden)
    .map((c, i) => ({ c, i }))
    .sort((x, y) => made(x.c) - made(y.c) || y.i - x.i)
    .map((x) => x.c);
  const open = openId ? chats.find((c) => c.id === openId) : undefined;
  if (open && !(open.main && open.mine) && !tabs.includes(open)) tabs.push(open);
  return tabs;
}

/** Your chats behind the arrow: the ones you closed, as listed (newest activity first). */
export function closedChats<T extends Row>(chats: readonly T[]): T[] {
  return chats.filter((c) => c.mine && !c.main && c.hidden);
}

/** Where closing a tab leads: the tab after it, else the one before, else "" (the main chat). */
export function afterClose(tabs: readonly { id: string }[], closing: string): string {
  const i = tabs.findIndex((c) => c.id === closing);
  if (i < 0) return "";
  return tabs[i + 1]?.id ?? tabs[i - 1]?.id ?? "";
}

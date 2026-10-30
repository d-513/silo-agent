// Where a Bot's call is drawn, so it stays with you as you move around the Bot.
// In the chat it is held in, it is the chat page's own. Anywhere else the call
// screen moves beside the page, while there is room for a third column; if not,
// or if you folded it away, the call is the bar under the page.

export type CallPlace = "" | "chat" | "aside" | "bar";

/** Whether the chat page open now is the one the call is held in (or is making). */
export function callIsHere(callChat: string, chatId: string | undefined, fresh: boolean): boolean {
  return callChat === chatId || (callChat === "new" && fresh && !chatId);
}

export function callPlace(a: {
  // A call, even one just ended: it lingers so its last word can be read.
  call: boolean;
  // The chat page of the call's chat is open.
  here: boolean;
  // The call screen rather than the bar.
  expanded: boolean;
  wide: boolean;
  // A machine pane is docked beside the page.
  docked: boolean;
}): CallPlace {
  if (!a.call) return "";
  if (a.here) return "chat";
  return a.expanded && a.wide && !a.docked ? "aside" : "bar";
}

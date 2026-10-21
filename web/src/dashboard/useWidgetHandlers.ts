import { ui } from "../api";
import { UI, type Chat, type DashboardWidget } from "../gen/silo/v1/ui_pb";
import { patch } from "../query";
import type { WidgetHandlers } from "./widgets";

// useWidgetHandlers is what the owner can do to a widget in place: pick it
// for the ask bar, refresh it, check items (its state) and press its actions.
// What the server answers is written back to the board's cache. `guard` runs
// a call, reporting its error, and answers undefined when it failed.
export function useWidgetHandlers({
  botId,
  guard,
  onPick,
  onQuoted,
}: {
  botId: string;
  guard: <T>(fn: () => Promise<T>) => Promise<T | undefined>;
  onPick: (id: string) => void;
  onQuoted: (c: Chat) => void;
}): (w: DashboardWidget) => WidgetHandlers {
  const input = { botId };
  const put = (w: DashboardWidget) => patch(UI.method.listDashboard, input, (r) => ({ ...r, widgets: r.widgets.map((x) => (x.id === w.id ? w : x)) }));
  return (w) => ({
    onSelect: () => onPick(w.id),
    onRefresh: async () => {
      const r = await guard(() => ui.refreshWidget({ botId, id: w.id }));
      if (r) put(r);
    },
    onState: (stateJson) => {
      put({ ...w, stateJson });
      void guard(() => ui.setWidgetState({ botId, id: w.id, stateJson }));
    },
    onAction: async (a, args, apart) => {
      const r = await guard(() => ui.runWidgetAction({ botId, id: w.id, action: a.id, argsJson: JSON.stringify(args), newChat: !!apart }));
      if (r?.widget) put(r.widget);
      if (r?.chat) onQuoted(r.chat);
    },
  });
}

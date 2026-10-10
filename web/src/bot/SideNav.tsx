import { MessageCircle } from "lucide-react";
import { Link } from "@tanstack/react-router";

// The pages a Bot's sidebar opens: the conversation-side ones, the first page
// of Customize and of Settings, and (in the narrow strip) the machine panes.
type SideTo =
  | "/bots/$botId/automations"
  | "/bots/$botId/memories"
  | "/bots/$botId/knowledge"
  | "/bots/$botId/feed"
  | "/bots/$botId/connectors"
  | "/bots/$botId/settings"
  | "/bots/$botId/files"
  | "/bots/$botId/desktop"
  | "/bots/$botId/console"
  | "/bots/$botId/changes"
  | "/bots/$botId/mail";
type SideProps = { to: SideTo; botId: string; on: boolean; icon: typeof MessageCircle; label: string; badge?: number };

// ActiveBar is the cobalt "you are here" mark on the left edge of the open
// row (a page or a chat). It grows in and shrinks out, so moving between rows
// reads as one mark handing over. The row must be `relative`.
export function ActiveBar({ on }: { on: boolean }) {
  return (
    <span
      aria-hidden
      className={`pointer-events-none absolute top-2 bottom-2 left-0 w-0.5 rounded-full bg-cobalt transition-[opacity,transform] duration-200 ease-quiet motion-reduce:transition-opacity ${
        on ? "scale-y-100 opacity-100" : "scale-y-50 opacity-0"
      }`}
    />
  );
}

// SideLink is a one-line row of the sidebar: a page beside the chats.
// Flat like a chat row: the open one is lifted by tone alone.
export function SideLink({ to, botId, on, icon: Icon, label, badge = 0 }: SideProps) {
  return (
    <Link
      to={to}
      params={{ botId }}
      data-tab-on={on || undefined}
      className={`relative flex h-9 items-center gap-2.5 rounded-control px-3 text-[13.5px] font-medium transition-[background-color,color] duration-[160ms] ease-quiet outline-offset-[-2px] ${
        on ? "bg-surface text-ink" : "text-ink-2 hover:bg-pressed hover:text-ink"
      }`}
    >
      <ActiveBar on={on} />
      <Icon size={15} />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {badge > 0 ? <UnreadBadge n={badge} /> : null}
    </Link>
  );
}

// UnreadBadge counts unseen Feed posts: a small cobalt pill.
function UnreadBadge({ n, dot = false }: { n: number; dot?: boolean }) {
  const label = `${n} unread`;
  if (dot) {
    return <span aria-label={label} title={label} className="absolute top-2 right-2 h-2 w-2 rounded-full bg-cobalt ring-2 ring-well" />;
  }
  return (
    <span aria-label={label} className="rise min-w-[18px] shrink-0 rounded-full bg-cobalt px-1.5 text-center font-mono text-[11px] leading-[18px] text-on-accent">
      {n > 99 ? "99+" : n}
    </span>
  );
}

// SideChip is SideLink in the narrow chip strip: icon only unless active.
export function SideChip({ to, botId, on, icon: Icon, label, badge = 0 }: SideProps) {
  return (
    <Link
      to={to}
      params={{ botId }}
      title={label}
      aria-label={label}
      className={`relative flex h-10 shrink-0 items-center gap-2 rounded-control px-3 text-[13px] font-medium ${
        on ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
      }`}
    >
      <Icon size={15} />
      {on && label}
      {badge > 0 && !on ? <UnreadBadge n={badge} dot /> : null}
    </Link>
  );
}

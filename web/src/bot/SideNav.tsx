import { MessageCircle } from "lucide-react";
import { Link } from "@tanstack/react-router";

// The conversation-side pages of a Bot.
type SideTo = "/bots/$botId/automations" | "/bots/$botId/memories" | "/bots/$botId/knowledge" | "/bots/$botId/feed";
type SideProps = { to: SideTo; botId: string; on: boolean; icon: typeof MessageCircle; label: string; badge?: number };

// SideLink is a one-line row above the chats list: the conversation-side pages.
export function SideLink({ to, botId, on, icon: Icon, label, badge = 0 }: SideProps) {
  return (
    <Link
      to={to}
      params={{ botId }}
      className={`flex h-9 items-center gap-2.5 rounded-control px-3 text-[13.5px] font-medium transition-[background-color,box-shadow,color] duration-[160ms] ease-quiet ${
        on ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
      }`}
    >
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
    <span aria-label={label} className="rise min-w-[18px] shrink-0 rounded-full bg-cobalt px-1.5 text-center font-mono text-[11px] leading-[18px] text-white">
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

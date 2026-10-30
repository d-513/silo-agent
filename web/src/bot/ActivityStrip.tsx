import { Link } from "@tanstack/react-router";
import { DoingRow } from "../Doing";
import type { Bot, BotActivity } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { chatLink } from "../links";
import { t } from "../i18n.ts";

const row = "h-7 px-2 text-[12.5px] text-ink-2";

// Work with no row of its own in the sidebar: the main chat and the side chats
// say what they are doing themselves.
const elsewhere = (a: BotActivity) => a.kind === "automation" || a.kind === "channel" || a.kind === "hidden";

// ActivityStrip is the Bot's work that has no row in the sidebar: automations
// and channels with what they are on, a press away, its subagents counted on
// one line. It shows only while there is some.
export function ActivityStrip({ bot }: { bot: Bot }) {
  const runs = bot.activity.filter(elsewhere);
  const agents = bot.activity.filter((a) => a.kind === "subagent");
  if (!runs.length && !agents.length) return null;
  const waiting = agents.some((a) => a.state === "needs_you");
  return (
    <ul className="mx-2 mb-2 space-y-px rounded-control bg-surface/60 px-1 py-1" aria-label={t("Also at work")}>
      {runs.slice(0, 3).map((a, i) => (
        <li key={a.chatId || i}>
          <DoingRow bot={bot} row={a} className={row} />
        </li>
      ))}
      {agents.length ? (
        <li>
          <Link
            {...chatLink(bot.id, agents[0].leadChatId)}
            className={`flex min-w-0 items-center gap-2 rounded-sm transition-colors duration-[160ms] hover:bg-pressed hover:text-ink ${row}`}
          >
            <Lamp status={waiting ? "needs_you" : "working"} />
            <span className="min-w-0 truncate">{t("{{count}} subagents at work", { count: agents.length })}</span>
          </Link>
        </li>
      ) : null}
    </ul>
  );
}

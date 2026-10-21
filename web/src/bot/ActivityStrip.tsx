import { Link } from "@tanstack/react-router";
import type { Bot, BotActivity } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { chatLink } from "../links";
import { sourceLabel } from "../sources";
import { toolMeta } from "../thread/toolInfo";
import { t } from "../i18n.ts";

// What a live run is called on the strip: an untitled chat is a new one.
const where = (a: BotActivity) => sourceLabel(a.kind, a.title, t("New chat"));

// ActivityStrip is what the Bot is doing right now, under its name: each live
// conversation with the tool it is in, a press away; its subagents counted
// on one line. It shows only while something runs.
export function ActivityStrip({ bot }: { bot: Bot }) {
  const runs = bot.activity.filter((a) => a.kind !== "subagent");
  const agents = bot.activity.filter((a) => a.kind === "subagent");
  if (!runs.length && !agents.length) return null;
  return (
    <ul className="mx-2 mb-2 space-y-px rounded-control bg-surface/60 px-1 py-1" aria-label={t("Working now")}>
      {runs.slice(0, 3).map((a) => (
        <li key={a.chatId}>
          <Link
            {...chatLink(bot.id, a.chatId)}
            className="flex h-7 min-w-0 items-center gap-2 rounded-sm px-2 text-[12.5px] text-ink-2 transition-colors duration-[160ms] hover:bg-pressed hover:text-ink"
          >
            <Lamp status="working" />
            <span className="min-w-0 truncate">{where(a)}</span>
            {a.step ? <span className="ml-auto shrink-0 text-ink-3">{toolMeta(a.step).app}</span> : null}
          </Link>
        </li>
      ))}
      {agents.length ? (
        <li>
          <Link
            {...chatLink(bot.id, agents[0].leadChatId)}
            className="flex h-7 min-w-0 items-center gap-2 rounded-sm px-2 text-[12.5px] text-ink-2 transition-colors duration-[160ms] hover:bg-pressed hover:text-ink"
          >
            <Lamp status="working" />
            <span className="min-w-0 truncate">{t("{{count}} subagents at work", { count: agents.length })}</span>
          </Link>
        </li>
      ) : null}
    </ul>
  );
}

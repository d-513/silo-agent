import { Link } from "@tanstack/react-router";
import type { Bot, BotActivity } from "./gen/silo/v1/ui_pb";
import { t } from "./i18n.ts";
import { Lamp, Status } from "./Lamp";
import { chatLink } from "./links";
import { sourceLabel } from "./sources";
import { progress, runLook, type StatusLook } from "./statusLook";

// What a Bot is doing, in the sizes a surface has room for. A crest's corner
// takes the lamp alone (the caller adds the word as a tooltip); these are the
// line and the list, so everywhere says the same thing the same way.

/** The line: lamp, what it is on, and how far along its board. */
export function DoingLine({ look, step, className = "" }: { look: StatusLook; step?: string; className?: string }) {
  return (
    <span className={`flex min-w-0 items-center gap-1.5 ${className}`}>
      <Status look={look} />
      {step ? <span className="shrink-0 font-mono text-[11.5px] leading-4 text-ink-3">{step}</span> : null}
    </span>
  );
}

const rowClass = "flex min-w-0 items-center gap-2 rounded-sm transition-colors duration-[160ms] hover:bg-pressed hover:text-ink";

/** The list's row: where it runs and what it is on, a press away. */
export function DoingRow({ bot, row, className = "" }: { bot: Pick<Bot, "id">; row: BotActivity; className?: string }) {
  const look = runLook(row);
  const where = sourceLabel(row.kind, row.title, t("New chat"));
  const body = (
    <>
      <Lamp status={look.lamp} />
      {where ? <span className="min-w-[3rem] shrink-[2] truncate">{where}</span> : null}
      <span className={`min-w-0 flex-1 truncate ${look.lamp === "working" ? "text-ink-3" : look.tone}`} title={look.word}>
        {look.word}
      </span>
      {progress(row) ? <span className="shrink-0 font-mono text-[11.5px] text-ink-3">{progress(row)}</span> : null}
    </>
  );
  if (!row.chatId) return <div className={`${rowClass} hover:bg-transparent hover:text-ink-2 ${className}`}>{body}</div>;
  return (
    <Link {...chatLink(bot.id, row.leadChatId || row.chatId)} className={`${rowClass} ${className}`}>
      {body}
    </Link>
  );
}

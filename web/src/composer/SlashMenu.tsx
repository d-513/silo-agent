import { msg, t } from "../i18n.ts";
import type { SlashCommand } from "./slash.ts";

// What each command does, in the reader's language; a command the console
// does not know yet shows the server's English.
const summaries: Record<string, string> = {
  clear: msg("Start the context over. Earlier messages stay visible."),
  wipe: msg("Delete messages for good: since /clear, the last exchange, or all."),
  compact: msg("Summarize the conversation to free the context."),
  model: msg("Show or switch this chat's model."),
  thinking: msg("Show or set how hard the model thinks."),
  stop: msg("Stop the reply that is running."),
  new: msg("Open a new chat, optionally starting it with a message."),
  rename: msg("Rename this chat."),
  remember: msg("Collect memories from this chat now."),
  help: msg("List the commands."),
};

// SlashMenu is the list over the composer while a command's name is typed:
// arrows move, Enter runs (or completes one that needs an argument), Tab
// completes, Esc closes.
export function SlashMenu({ items, active, onHover, onPick }: { items: SlashCommand[]; active: number; onHover: (i: number) => void; onPick: (c: SlashCommand) => void }) {
  return (
    <div role="listbox" aria-label={t("Commands")} className="absolute right-0 bottom-full left-0 z-30 mb-2 rounded-control bg-surface p-1 shadow-slip">
      <div className="rise max-h-[280px] overflow-y-auto overscroll-contain">
        {items.map((c, i) => (
          <button
            key={c.name}
            type="button"
            role="option"
            aria-selected={i === active}
            onMouseEnter={() => onHover(i)}
            // Keep the textarea's focus: picking is a keystroke's worth.
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => onPick(c)}
            className={`flex w-full items-baseline gap-2 rounded-sm px-2.5 py-1.5 text-left transition-colors duration-[160ms] ${i === active ? "bg-well text-ink" : "text-ink hover:bg-well"}`}
          >
            <span className="shrink-0 font-mono text-[13px]">/{c.name}</span>
            {c.args ? <span className="shrink-0 font-mono text-[12px] text-ink-3">{c.args}</span> : null}
            <span className="min-w-0 flex-1 truncate text-[12px] text-ink-3">{summaries[c.name] ? t(summaries[c.name]) : c.summary}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

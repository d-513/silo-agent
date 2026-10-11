import { MessageCircle } from "lucide-react";
import { Btn } from "../Btn";
import { Crest } from "../Crest";
import { msg, t } from "../i18n.ts";

// What a new chat offers to start from. The chip is a few words; the prompt it
// puts in the composer is a whole request the Bot can act on as it stands (it
// asks for whatever is missing), and the human can edit it before sending.
// Each one shows something a Bot is for, not that the machine exists: memory,
// the web, a file, a schedule.
const starters = [
  {
    label: msg("Get to know me"),
    prompt: msg("Ask me a few questions about who I am, what I'm working on and how I like things done, one at a time. Remember what matters."),
  },
  {
    label: msg("Research a topic"),
    prompt: msg("Research a topic for me on the web and write a short brief with sources: what is known, what is disputed, and what to read first. Ask me for the topic."),
  },
  {
    label: msg("Read a document"),
    prompt: msg("I'm attaching a file. Read it, tell me what it says in a few bullet points, and list anything I should act on."),
  },
  {
    label: msg("Set up a daily digest"),
    prompt: msg("Set up an automation that posts a short news digest to my Feed every weekday at 8:00. Ask me which topics first."),
  },
  {
    label: msg("What can you do?"),
    prompt: msg("What can you do on this machine? Show me three things worth trying, each with an example I can run right now."),
  },
] as const;

// The thread before anything is said: the Bot's crest, a line, and a few
// prompts to start from.
export function EmptyThread({ botName, botCrest, onSelectPrompt }: { botName?: string; botCrest?: number; onSelectPrompt?: (prompt: string) => void }) {
  return (
    <div className="rise my-auto flex flex-col items-center justify-center px-4 py-14 text-center">
      <div className="blink mb-4">
        {botCrest !== undefined ? <Crest index={botCrest} size={56} /> : <MessageCircle size={30} className="text-ink-2" />}
      </div>
      <h2 className="text-card-title leading-5 text-ink">{botName ? botName : t("Silo Bot")}</h2>
      <p className="mt-1.5 max-w-md text-[12.5px] leading-[18px] text-ink-2">
        {t("Ready for your prompt. Run code in the container, inspect files, or command the browser and desktop.")}
      </p>
      {onSelectPrompt && (
        <div className="mt-6 flex max-w-lg flex-wrap justify-center gap-2">
          {starters.map((s) => (
            <Btn key={s.label} kind="secondary" size="sm" onClick={() => onSelectPrompt(t(s.prompt))}>
              {t(s.label)}
            </Btn>
          ))}
        </div>
      )}
    </div>
  );
}

import { MessageCircle } from "lucide-react";
import { Btn } from "../Btn";
import { Crest } from "../Crest";

const starters = [
  "What files are in /workspace?",
  "Run a Python script to check system info",
  "Open browser and check the desktop",
  "Search the web",
];

// The thread before anything is said: the Bot's crest, a line, and a few
// prompts to start from.
export function EmptyThread({ botName, botCrest, onSelectPrompt }: { botName?: string; botCrest?: number; onSelectPrompt?: (prompt: string) => void }) {
  return (
    <div className="rise my-auto flex flex-col items-center justify-center px-4 py-14 text-center">
      <div className="blink mb-4">
        {botCrest !== undefined ? <Crest index={botCrest} size={56} /> : <MessageCircle size={30} className="text-ink-2" />}
      </div>
      <h2 className="text-[15px] leading-5 font-semibold tracking-[-0.01em] text-ink">{botName ? botName : "Silo Bot"}</h2>
      <p className="mt-1.5 max-w-md text-[12.5px] leading-[18px] text-ink-2">
        Ready for your prompt. Run code in the container, inspect files, or command the browser and desktop.
      </p>
      {onSelectPrompt && (
        <div className="mt-6 flex max-w-lg flex-wrap justify-center gap-2">
          {starters.map((prompt) => (
            <Btn key={prompt} kind="secondary" size="sm" onClick={() => onSelectPrompt(prompt)}>
              {prompt}
            </Btn>
          ))}
        </div>
      )}
    </div>
  );
}

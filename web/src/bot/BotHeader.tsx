import { Power } from "lucide-react";
import { Btn } from "../Btn";
import { Crest } from "../Crest";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { Lamp, StatusWord } from "../Lamp";
import { BotTabs, machineBtnCompact, TabStrip } from "./BotTabs";
import type { Tab } from "./tabs";

// The Bot page's top bar: crest, name, status, the tab strip (twice: wide, and
// the icon strip below 960), and Start/Stop.
export function BotHeader({ id, bot, tab, chatId, onStart, onStop }: { id: string; bot: Bot; tab: Tab; chatId?: string; onStart: () => void; onStop: () => void }) {
  return (
    <header className="@container flex h-12 shrink-0 items-center gap-1 px-2 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14 wide:gap-3 wide:px-4">
      <span className="blink hidden wide:inline-flex">
        <Crest index={bot.crest} size={28} />
      </span>
      <h1 className="sr-only min-w-0 truncate text-card-title leading-5 wide:not-sr-only wide:max-w-[12rem]">{bot.name}</h1>
      <span className="hidden shrink-0 items-center gap-2 wide:inline-flex">
        <Lamp status={bot.status} />
        <StatusWord status={bot.status} />
      </span>
      <TabStrip tab={tab} className="hidden min-h-0 flex-1 self-stretch wide:ml-2 wide:block" innerClass="flex h-full items-stretch gap-0.5">
        <BotTabs id={id} tab={tab} chatId={chatId} />
      </TabStrip>
      <TabStrip tab={tab} className="min-h-0 flex-1 self-stretch wide:hidden" innerClass="flex h-full items-stretch gap-0.5">
        <BotTabs id={id} tab={tab} chatId={chatId} splitMachine compact />
      </TabStrip>
      <div className="shrink-0">
        {bot.workerConnected ? (
          <Btn kind="ghost" title="Stop Bot" aria-label="Stop Bot" onClick={onStop} className={machineBtnCompact} icon={<Power size={13} />}>
            <span className="@max-[1280px]:hidden">Stop Bot</span>
          </Btn>
        ) : (
          <Btn
            kind="ghost"
            title={bot.status === "starting" ? "Starting…" : "Start Bot"}
            aria-label={bot.status === "starting" ? "Starting" : "Start Bot"}
            onClick={onStart}
            disabled={bot.status === "starting"}
            className={machineBtnCompact}
            icon={<Power size={13} />}
          >
            <span className="@max-[1280px]:hidden">{bot.status === "starting" ? "Starting…" : "Start Bot"}</span>
          </Btn>
        )}
      </div>
    </header>
  );
}

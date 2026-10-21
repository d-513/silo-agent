import { useState } from "react";
import { ui } from "./api";
import { fail } from "./errors";
import { Panel } from "./Field";
import type { Bot } from "./gen/silo/v1/ui_pb";
import { setBot } from "./query";
import { msg, t } from "./i18n.ts";

const levels = [
  { id: "off", label: msg("Off"), note: msg("Only when you ask. No Heartbeat.") },
  { id: "quiet", label: msg("Quiet"), note: msg("A morning check-in. News goes to the Feed; questions wait on the Dashboard.") },
  { id: "active", label: msg("Active"), note: msg("Check-ins through the day, never at night. It may also write to you in the main chat.") },
] as const;

// ProactivitySettings is how much the Bot does unasked: one choice that sets
// the Heartbeat's schedule and whether a background run may write into the
// main chat. It saves on its own, apart from the Settings form.
export function ProactivitySettings({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const [busy, setBusy] = useState(false);
  const current = levels.find((l) => l.id === bot.proactivity) ?? levels[0];

  async function choose(level: string) {
    if (busy || (level === bot.proactivity && !bot.heartbeatCustom)) return;
    setBusy(true);
    onError("");
    try {
      // UpdateBot replaces the Bot's other fields: send them back unchanged.
      setBot(
        await ui.updateBot({
          id: bot.id,
          name: bot.name,
          description: bot.description,
          soul: bot.soul,
          autoApprove: bot.autoApprove,
          model: bot.model,
          proactivity: level,
        }),
      );
    } catch (e) {
      onError(fail(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("Proactivity")} note={t("How much this Bot does without being asked.")} className="mt-8">
      <div className="grid gap-3">
        <div role="radiogroup" aria-label={t("Proactivity")} className="flex w-fit select-none items-center gap-0.5 rounded-control bg-well p-[3px]">
          {levels.map((l) => {
            const active = l.id === current.id;
            return (
              <button
                key={l.id}
                type="button"
                role="radio"
                aria-checked={active}
                disabled={busy}
                onClick={() => void choose(l.id)}
                className={`flex h-8 items-center justify-center rounded-sm px-3 text-[12.5px] font-medium transition-[transform,background-color,color,box-shadow] duration-[200ms] ease-quiet active:scale-[.97] active:duration-[70ms] ${
                  active ? "bg-surface text-ink shadow-card" : "text-ink-3 hover:bg-pressed hover:text-ink"
                } ${busy ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}
              >
                {t(l.label)}
              </button>
            );
          })}
        </div>
        <p className="text-[13px] leading-5 text-ink-2">{t(current.note)}</p>
        {bot.heartbeatCustom ? (
          <p className="text-[12.5px] leading-[18px] text-ink-3">
            {t("The Heartbeat runs on a schedule set by hand (Automations). Choosing a level puts it back on that level's schedule.")}
          </p>
        ) : null}
      </div>
    </Panel>
  );
}

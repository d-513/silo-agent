import { useState } from "react";
import { ChipRadio } from "./ChipRadio";
import { fail } from "./errors";
import { SettingRow } from "./Field";
import type { Bot } from "./gen/silo/v1/ui_pb";
import { updateBotWith } from "./query";
import { msg, t } from "./i18n.ts";

const levels = [
  { id: "off", label: msg("Off"), note: msg("Only when you ask. No Heartbeat.") },
  { id: "quiet", label: msg("Quiet"), note: msg("A morning check-in. News goes to the Feed; questions wait on the Dashboard.") },
  { id: "active", label: msg("Active"), note: msg("Check-ins through the day, never at night. It may also write to you in the main chat.") },
] as const;

// ProactivitySettings is how much the Bot does unasked: one choice that sets
// the Heartbeat's schedule and whether a background run may write into the
// main chat. It is one row of the Settings page and saves on its own, apart
// from the page's Save bar.
export function ProactivitySettings({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const [busy, setBusy] = useState(false);
  const current = levels.find((l) => l.id === bot.proactivity) ?? levels[0];

  async function choose(level: string) {
    if (busy || (level === bot.proactivity && !bot.heartbeatCustom)) return;
    setBusy(true);
    onError("");
    try {
      await updateBotWith(bot, { proactivity: level });
    } catch (e) {
      onError(fail(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <SettingRow
      label={t("Proactivity")}
      hint={
        <>
          {t(current.note)}
          {bot.heartbeatCustom ? (
            <span className="mt-1 block text-ink-3">
              {t("The Heartbeat runs on a schedule set by hand (Automations). Choosing a level puts it back on that level's schedule.")}
            </span>
          ) : null}
        </>
      }
    >
      <ChipRadio
        label={t("Proactivity")}
        disabled={busy}
        value={current.id as string}
        onChange={(v) => void choose(v)}
        options={levels.map((l) => ({ value: l.id, label: t(l.label) }))}
      />
    </SettingRow>
  );
}

import { useEffect, useId, useState } from "react";
import { ui } from "./api";
import { fail } from "./errors";
import { SettingRow, inputClass } from "./Field";
import type { Bot } from "./gen/silo/v1/ui_pb";
import { setBot } from "./query";
import { deviceZone, zoneLabel, zoneNames, zoneOffset } from "./zones";
import { msg, t } from "./i18n.ts";
import { Trans } from "./Trans.tsx";

const modes = [
  { pinned: false, label: msg("Follow my device") },
  { pinned: true, label: msg("Fixed") },
] as const;

// TimeZoneSettings is where a Bot's time zone is kept: following the device
// the owner writes from, or fixed to one they pick: a row of the Settings page
// (two while it is fixed) that saves on its own, apart from the page's Save bar.
export function TimeZoneSettings({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const device = deviceZone();
  // What is typed into the Fixed field until it names a zone and is saved.
  const [draft, setDraft] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const listId = useId();
  useEffect(() => setDraft(null), [bot.id]);

  async function save(change: { timeZone?: string; timeZonePinned?: boolean }) {
    setBusy(true);
    onError("");
    try {
      await updateBotWith(bot, change);
      setDraft(null);
    } catch (e) {
      onError(fail(e));
    } finally {
      setBusy(false);
    }
  }

  function setMode(pinned: boolean) {
    if (pinned === bot.timeZonePinned || busy) return;
    // Following starts now, from this device, not at the next message.
    void save(pinned || !device ? { timeZonePinned: pinned } : { timeZonePinned: false, timeZone: device });
  }

  const names = zoneNames(bot.timeZone);
  function type(v: string) {
    setDraft(v);
    if (v !== bot.timeZone && names.includes(v)) void save({ timeZone: v });
  }

  const zoneError = draft !== null && draft !== "" && zoneOffset(draft) === "" ? t("Not a time zone this browser knows.") : undefined;

  return (
    <>
      <SettingRow
        label={t("Time zone")}
        hint={
          <>
            {t("Automation schedules and the times this Bot reads are in it. The Bot's machine itself runs on UTC.")}
            {bot.timeZonePinned ? null : (
              <span className="mt-1 block">
                <Trans
                  i18nKey={
                    device && device !== bot.timeZone
                      ? "<mono>{{zone}}</mono>. This device is in {{device}}; the Bot moves there with your next message."
                      : "<mono>{{zone}}</mono>. It moves with the device you send messages from."
                  }
                  values={{ zone: zoneLabel(bot.timeZone), device }}
                  components={{ mono: <span className="font-mono text-ink" /> }}
                />
              </span>
            )}
          </>
        }
      >
        <ChipRadio
          label={t("How this Bot's time zone is set")}
          disabled={busy}
          value={bot.timeZonePinned}
          onChange={setMode}
          options={modes.map((m) => ({ value: m.pinned, label: t(m.label) }))}
        />
      </SettingRow>
      {bot.timeZonePinned ? (
        <SettingRow
          label={t("Zone")}
          hint={t("An IANA name such as Europe/Warsaw. Now {{zone}}; it stays there wherever you write from.", { zone: zoneLabel(bot.timeZone) })}
          error={zoneError}
        >
          <input
            className={`${inputClass} font-mono text-[13px]`}
            list={listId}
            spellCheck={false}
            autoComplete="off"
            aria-label={t("Zone")}
            aria-invalid={zoneError ? true : undefined}
            value={draft ?? bot.timeZone}
            onChange={(e) => type(e.target.value.trim())}
            onBlur={() => setDraft(null)}
          />
          <datalist id={listId}>
            {names.map((n) => (
              <option key={n} value={n} />
            ))}
          </datalist>
        </SettingRow>
      ) : null}
    </>
  );
}

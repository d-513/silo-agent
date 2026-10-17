import { useEffect, useId, useState } from "react";
import { ui } from "./api";
import { fail } from "./errors";
import { Field, Panel, inputClass } from "./Field";
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
// the owner writes from, or fixed to one they pick. It saves on its own, apart
// from the Settings form.
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
      // UpdateBot replaces the Bot's other fields: send them back unchanged.
      setBot(
        await ui.updateBot({
          id: bot.id,
          name: bot.name,
          description: bot.description,
          soul: bot.soul,
          autoApprove: bot.autoApprove,
          model: bot.model,
          ...change,
        }),
      );
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

  return (
    <Panel
      title={t("Time zone")}
      note={t("Automation schedules and the times this Bot reads are in it. The Bot's machine itself runs on UTC.")}
      className="mt-8"
    >
      <div className="grid gap-4">
        <div role="radiogroup" aria-label={t("How this Bot's time zone is set")} className="flex w-fit select-none items-center gap-0.5 rounded-control bg-well p-[3px]">
          {modes.map((m) => {
            const active = m.pinned === bot.timeZonePinned;
            return (
              <button
                key={m.label}
                type="button"
                role="radio"
                aria-checked={active}
                disabled={busy}
                onClick={() => setMode(m.pinned)}
                className={`flex h-8 items-center justify-center rounded-sm px-3 text-[12.5px] font-medium transition-[transform,background-color,color,box-shadow] duration-[200ms] ease-quiet active:scale-[.97] active:duration-[70ms] ${
                  active ? "bg-surface text-ink shadow-card" : "text-ink-3 hover:bg-pressed hover:text-ink"
                } ${busy ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}
              >
                {t(m.label)}
              </button>
            );
          })}
        </div>
        {bot.timeZonePinned ? (
          <Field
            label={t("Zone")}
            hint={t("An IANA name such as Europe/Warsaw. Now {{zone}}; it stays there wherever you write from.", { zone: zoneLabel(bot.timeZone) })}
            error={draft !== null && draft !== "" && zoneOffset(draft) === "" ? t("Not a time zone this browser knows.") : undefined}
          >
            <input
              className={`${inputClass} max-w-[320px] font-mono text-[13px]`}
              list={listId}
              spellCheck={false}
              autoComplete="off"
              value={draft ?? bot.timeZone}
              onChange={(e) => type(e.target.value.trim())}
              onBlur={() => setDraft(null)}
            />
            <datalist id={listId}>
              {names.map((n) => (
                <option key={n} value={n} />
              ))}
            </datalist>
          </Field>
        ) : (
          <p className="text-[13px] leading-[20px] text-ink-2">
            <Trans
              i18nKey={
                device && device !== bot.timeZone
                  ? "<mono>{{zone}}</mono>. This device is in {{device}}; the Bot moves there with your next message."
                  : "<mono>{{zone}}</mono>. It moves with the device you send messages from."
              }
              values={{ zone: zoneLabel(bot.timeZone), device }}
              components={{ mono: <span className="font-mono text-ink" /> }}
            />
          </p>
        )}
      </div>
    </Panel>
  );
}

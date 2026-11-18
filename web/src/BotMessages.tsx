import { useState } from "react";
import { fail } from "./errors";
import { SettingRow } from "./Field";
import type { Bot } from "./gen/silo/v1/ui_pb";
import { updateBotWith } from "./query";
import { Switch } from "./Switch";
import { t } from "./i18n.ts";

// BotMessagesSetting is whether other Bots of the Workspace may hand this Bot
// work (message_bot): a row of the Settings page that saves on its own, apart
// from the page's Save bar.
export function BotMessagesSetting({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const [busy, setBusy] = useState(false);
  async function set(on: boolean) {
    setBusy(true);
    onError("");
    try {
      await updateBotWith(bot, { acceptBotMessages: on });
    } catch (e) {
      onError(fail(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <SettingRow
      label={t("Messages from other Bots")}
      hint={t(
        "Other Bots of this Workspace may hand {{name}} work. It answers each one in a conversation of its own, with its own rules, and only for people who use {{name}} too.",
        { name: bot.name },
      )}
    >
      <Switch on={bot.acceptBotMessages} disabled={busy} title={t("Messages from other Bots")} onChange={(v) => void set(v)} />
    </SettingRow>
  );
}

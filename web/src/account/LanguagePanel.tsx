import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { ErrorWell, Field, Panel } from "../Field";
import { type User } from "../gen/silo/v1/ui_pb";
import { followUser, t } from "../i18n.ts";
import { nativeLabel, uiLanguages } from "../languages.ts";
import { Select } from "../Select";

// LanguagePanel picks the language Silo is shown in. It is the account's, so
// it follows the person to every browser they sign in on, and it changes at
// once: each language is listed in its own words, since the reader may not
// know the one the page is in now.
export function LanguagePanel({ user, onChange }: { user: User; onChange: (u: User) => void }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function pick(language: string) {
    if (language === user.language || busy) return;
    setBusy(true);
    setErr("");
    try {
      const u = await ui.setLanguage({ language });
      onChange(u);
      // Every page is drawn again in the new language (App keys the routes by it).
      await followUser(u.language);
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Panel title={t("Language")} note={t("The language Silo is shown in, on every device you sign in on.")}>
      <div className="grid gap-4">
        <Field label={t("Display language")} hint={t("A new Bot starts out writing in it. A Bot you already have keeps its own language, set in its Settings.")}>
          <Select className="max-w-[320px]" value={user.language} disabled={busy} onChange={(v) => void pick(v)} options={uiLanguages.map((code) => ({ value: code, label: nativeLabel(code) }))} />
        </Field>
        {err ? <ErrorWell>{err}</ErrorWell> : null}
      </div>
    </Panel>
  );
}

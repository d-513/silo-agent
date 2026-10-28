import { useQuery } from "@connectrpc/connect-query";
import { Square, Volume2 } from "lucide-react";
import { useState } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { ChipRadio } from "./ChipRadio";
import { fail } from "./errors";
import { Spinner } from "./Feedback";
import { SettingGroup, SettingRow, inputClass } from "./Field";
import { UI, type Bot } from "./gen/silo/v1/ui_pb";
import { modelOptions } from "./modelOptions";
import { setBot } from "./query";
import { speak, stopSpeaking } from "./ReadAloud";
import { Select } from "./Select";
import { flagOf } from "./languages.ts";
import { t } from "./i18n.ts";

// The speeds a voice can be set to; 0 is the model's own.
const SPEEDS = [0.85, 0, 1.15, 1.3] as const;

// BotVoice is how the Bot sounds, for Read aloud and calls: a speech model
// from the operator's list, one of its voices, and a speed. It saves on its
// own, apart from the page's Save bar, and Hear it tries a choice before it
// is saved.
export function BotVoice({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const [model, setModel] = useState(bot.speechModel);
  const voicesQ = useQuery(UI.method.listVoices, { botId: bot.id, model }, { staleTime: 5 * 60_000 });
  const v = voicesQ.data;
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<"idle" | "loading" | "playing">("idle");
  const [previewErr, setPreviewErr] = useState("");
  const voices = v?.voices ?? [];
  // A voice typed for a provider that lists none.
  const [typed, setTyped] = useState(bot.voice);

  async function save(patch: { speechModel?: string; voice?: string; speed?: number }) {
    setBusy(true);
    onError("");
    try {
      const next = await ui.setBotVoice({
        botId: bot.id,
        speechModel: patch.speechModel ?? bot.speechModel,
        voice: patch.voice ?? bot.voice,
        speed: patch.speed ?? bot.speechSpeed,
      });
      setBot(next);
    } catch (e) {
      onError(fail(e));
    } finally {
      setBusy(false);
    }
  }

  const effectiveModel = model || v?.defaultModel || "";
  const voice = bot.voice && (voices.length === 0 || voices.some((x) => x.id === bot.voice)) ? bot.voice : "";
  const auto = v?.defaultVoice ? voices.find((x) => x.id === v.defaultVoice)?.label || v.defaultVoice : "";
  // The Bot's own language first, then the rest.
  const lang = bot.language === "mixed" ? "" : bot.language;
  const sorted = [...voices].sort((a, b) => Number(b.language === lang) - Number(a.language === lang));

  function hear() {
    if (preview !== "idle") {
      stopSpeaking();
      return;
    }
    setPreviewErr("");
    void speak(
      { botId: bot.id, text: t("Hi, I'm {{name}}. This is how I sound when I talk with you.", { name: bot.name }), model: effectiveModel, voice: voice || v?.defaultVoice || "", speed: bot.speechSpeed || 1 },
      { state: setPreview, error: setPreviewErr },
      "preview",
    );
  }

  return (
    <SettingGroup title={t("Voice")} note={t("How {{name}} sounds when it reads a reply aloud or talks with you on a call.", { name: bot.name })}>
      <SettingRow label={t("Speech model")} hint={t("From the operator's allowed speech models. Voices differ from one to the next.")}>
        <Select
          value={model}
          disabled={busy}
          onChange={(m) => {
            setModel(m);
            void save({ speechModel: m, voice: "" });
          }}
          emptyLabel={t("No speech models allowed")}
          options={[{ value: "", label: v?.defaultModel ? t("Operator default ({{model}})", { model: v.defaultModel.split("/").slice(1).join("/") }) : t("Operator default") }, ...modelOptions(v?.models ?? [])]}
        />
      </SettingRow>
      <SettingRow
        label={t("Voice", { context: "speaker" })}
        hint={voices.length ? t("Automatic picks one for {{name}}'s language.", { name: bot.name }) : t("This model does not list its voices: type one from its provider's documentation, or leave it empty.")}
        meta={voicesQ.isFetching ? <Spinner size={12} /> : null}
      >
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            {voices.length ? (
              <Select
                value={voice}
                disabled={busy}
                onChange={(id) => void save({ voice: id })}
                options={[
                  { value: "", label: auto ? t("Automatic ({{voice}})", { voice: auto }) : t("Automatic") },
                  ...sorted.map((x) => ({ value: x.id, label: x.label, hint: x.language ? `${flagOf(x.language)} ${x.language}` : undefined })),
                ]}
              />
            ) : (
              <input
                className={`${inputClass} font-mono text-[13px]`}
                value={typed}
                placeholder={t("Provider's default")}
                spellCheck={false}
                autoComplete="off"
                disabled={busy}
                onChange={(e) => setTyped(e.target.value)}
                onBlur={() => typed !== bot.voice && void save({ voice: typed.trim() })}
              />
            )}
          </div>
          <Btn kind="secondary" disabled={!effectiveModel} onClick={hear} aria-pressed={preview !== "idle"}>
            {preview === "loading" ? <Spinner size={13} /> : preview === "playing" ? <Square size={11} fill="currentColor" strokeWidth={0} /> : <Volume2 size={14} />}
            {preview === "playing" ? t("Stop") : t("Hear it")}
          </Btn>
        </div>
        {previewErr ? <p className="mt-1.5 text-[12.5px] text-vermilion">{previewErr}</p> : null}
      </SettingRow>
      <SettingRow label={t("Speed")}>
        <ChipRadio
          label={t("Speed")}
          disabled={busy}
          value={String(SPEEDS.includes(bot.speechSpeed as (typeof SPEEDS)[number]) ? bot.speechSpeed : 0)}
          onChange={(s) => void save({ speed: Number(s) })}
          options={SPEEDS.map((s) => ({ value: String(s), label: s === 0 ? t("Normal", { context: "speed" }) : `${s}×` }))}
        />
      </SettingRow>
    </SettingGroup>
  );
}

import { Field, Panel } from "../Field";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { Select } from "../Select";
import { modelOptions } from "../modelOptions";
import { msg, t } from "../i18n.ts";
import { FieldGroup } from "./FieldGroup";
import { SourceChips } from "./FieldRow";
import { AllowedModels } from "./ModelSettings";
import { VOICE_CALL_NOTE, VOICE_NETWORK_NOTE, groupOf } from "./fields";
import { useSettingsForm } from "./useAdminSettings";

// The models a voice call is made of, each picked from its own kind's list.
const PICKERS: { key: string; kind: "transcription" | "speech" | "text"; label: string; hint: string; off: string; none: string }[] = [
  {
    key: "transcribe_model",
    kind: "transcription",
    label: msg("Speech-to-text"),
    hint: msg("Hears what you say: dictation in the composer, the transcribe tool, and every turn of a call."),
    off: msg("Off: no dictation and no calls"),
    none: msg("No speech-to-text models allowed yet"),
  },
  {
    key: "voice.speech_model",
    kind: "speech",
    label: msg("Speech"),
    hint: msg("The voice a Bot speaks with unless its own Settings pick another allowed speech model."),
    off: msg("None: no Read aloud and no calls"),
    none: msg("No speech models allowed yet"),
  },
  {
    key: "model_voice",
    kind: "text",
    label: msg("Talker"),
    hint: msg("A fast text model that holds the conversation on a call and hands real work to the Bot, so answers start in under a second. Without one, the Bot answers every turn itself: smarter, slower."),
    off: msg("None: the Bot answers every turn"),
    none: msg("No text models allowed yet"),
  },
];

export function VoiceSettings() {
  const { fields, values, setValue, allowedOf, models } = useSettingsForm();
  const field = (key: string) => fields.find((f) => f.key === key);
  const offValue = (key: string) => (key === "transcribe_model" ? "off" : "");

  return (
    <>
      <FieldGroup title={t("Voice")} note={t(VOICE_CALL_NOTE)} rows={fields.filter((f) => groupOf(f.key) === "voice")} />

      <Panel title={t("Voice models")} note={t("What a call hears with, speaks with, and thinks with first. Each picker offers its own list below.")}>
        <div className="grid gap-x-4 gap-y-5 sm:grid-cols-2">
          {PICKERS.map((p) => {
            const f = field(p.key);
            const list = p.kind === "text" ? models : allowedOf(p.kind);
            return (
              <Field key={p.key} label={t(p.label)} hint={t(p.hint)} headerRight={f ? <SourceChips field={f} /> : undefined}>
                <Select
                  value={values[p.key] ?? ""}
                  onChange={(v) => setValue(p.key, v)}
                  disabled={f?.source === ConfigSource.ENV}
                  options={[{ value: offValue(p.key), label: t(p.off) }, ...modelOptions(list)]}
                  placeholder={t(p.off)}
                  emptyLabel={t(p.none)}
                />
              </Field>
            );
          })}
        </div>
      </Panel>

      <AllowedModels kind="transcription" />
      <AllowedModels kind="speech" />

      <FieldGroup title={t("Call network")} note={t(VOICE_NETWORK_NOTE)} rows={fields.filter((f) => groupOf(f.key) === "voice_network")} />
    </>
  );
}

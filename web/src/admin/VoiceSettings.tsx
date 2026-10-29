import { AudioLines } from "lucide-react";
import type { ModelOption } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";
import { AllowedModels } from "./AllowedModels";
import { FieldGroup } from "./FieldGroup";
import { VOICE_CALL_NOTE, VOICE_NETWORK_NOTE, groupOf } from "./fields";
import { ModelsHead, RolePanel, type Role } from "./roles";
import { useSettingsForm } from "./useAdminSettings";

// What a model does on a call: live, or one request at a time.
function how(kind: "transcription" | "speech") {
  return function describe(m: ModelOption | undefined) {
    if (!m) return null;
    const words = m.live
      ? kind === "speech"
        ? t("Live: one connection for the whole call.")
        : t("Live: heard as you speak.")
      : kind === "speech"
        ? t("One request per sentence.")
        : t("One request per turn, after you stop.");
    return <p className={`mt-1.5 text-[12px] leading-[17px] ${m.live ? "text-cobalt" : "text-ink-3"}`}>{words}</p>;
  };
}

// The models a voice call is made of, each picked from its own kind's list.
const ROLES: Role[] = [
  {
    key: "transcribe_model",
    takes: "transcription",
    label: msg("Speech-to-text"),
    hint: msg("Hears what you say: dictation in the composer, the transcribe tool, and calls unless the next picker names another."),
    unset: msg("Off: no dictation and no calls"),
    unsetValue: "off",
    describe: how("transcription"),
  },
  {
    key: "voice.transcribe_model",
    takes: "transcription",
    label: msg("Speech-to-text on calls"),
    hint: msg("Hears every turn of a call. A live model hears you as you speak and has the words ready when you stop; dictation can stay on a cheaper one."),
    unset: msg("Same as dictation"),
    describe: how("transcription"),
  },
  {
    key: "voice.speech_model",
    takes: "speech",
    label: msg("Speech"),
    hint: msg("The voice a Bot speaks with unless its own Settings pick another allowed speech model."),
    unset: msg("None: no Read aloud and no calls"),
    describe: how("speech"),
  },
  {
    key: "model_voice",
    takes: "text",
    label: msg("Talker"),
    hint: msg("A fast text model that holds the conversation on a call and hands real work to the Bot, so answers start in under a second. Without one, the Bot answers every turn itself: smarter, slower."),
    unset: msg("None: the Bot answers every turn"),
  },
];

export function VoiceSettings() {
  const { fields } = useSettingsForm();
  return (
    <>
      <ModelsHead icon={AudioLines} title={t("Voice")} note={t("Dictation, Read aloud and calls: what hears you, what speaks, and the fast model that talks first.")} kinds={["transcription", "speech"]} />
      <FieldGroup title={t("Voice")} note={t(VOICE_CALL_NOTE)} rows={fields.filter((f) => groupOf(f.key) === "voice")} />
      <RolePanel title={t("Voice models")} note={t("What a call hears with, speaks with, and thinks with first. Each picker offers its own list below.")} roles={ROLES} />
      <AllowedModels kind="transcription" />
      <AllowedModels kind="speech" />
      <FieldGroup title={t("Call network")} note={t(VOICE_NETWORK_NOTE)} rows={fields.filter((f) => groupOf(f.key) === "voice_network")} />
    </>
  );
}

import { Radio } from "lucide-react";
import { Tip, TipTitle } from "../Tip";
import { t } from "../i18n.ts";

// LiveChip marks a transcription or speech model that works live, and says
// what that buys on a call. A model without it is used one request at a time.
export function LiveChip({ kind, live, words, endOfTurn }: { kind: string; live: boolean; words?: boolean; endOfTurn?: boolean }) {
  if (!live || (kind !== "transcription" && kind !== "speech")) return null;
  return (
    <Tip
      content={
        <>
          <TipTitle>{t("Live")}</TipTitle>
          <p className="mt-1">{kind === "speech" ? t("Stays connected for a whole call and speaks each sentence on that connection, so the voice starts sooner.") : t("Hears you as you speak, so the words are ready the moment you stop.")}</p>
          {words ? <p className="mt-1">{t("Says when each word is spoken, so a cut-off reply is recorded exactly as far as it was heard.")}</p> : null}
          {endOfTurn ? <p className="mt-1">{t("Judges from your words when you have finished, so a turn can end before the silence timer.")}</p> : null}
        </>
      }
    >
      <span className="inline-flex shrink-0 items-center gap-1 rounded-xs bg-cobalt-pale px-1.5 py-0.5 text-[11px] font-medium text-cobalt">
        <Radio size={11} />
        {t("Live")}
      </span>
    </Tip>
  );
}

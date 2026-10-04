import { Mic, Square } from "lucide-react";
import type { Dictation } from "../voice";
import { micTitle } from "./model";

// The mic toggles a take: Mic to start, a vermilion square to stop and
// transcribe. It is disabled (with the reason as its title) where the browser
// cannot record.
export function MicButton({ dictation, disabled }: { dictation: Dictation; disabled: boolean }) {
  const { state, supported } = dictation;
  const recording = state === "recording";
  const busy = state === "transcribing";
  return (
    <button
      type="button"
      title={micTitle(supported, state)}
      aria-label={recording ? "Stop dictation" : "Dictate"}
      aria-pressed={recording}
      disabled={disabled || !supported || busy}
      className={`flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-control transition-[background-color,color,transform] duration-[160ms] ease-quiet active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent ${
        recording ? "bg-vermilion-pale text-vermilion" : "text-ink-2 hover:bg-well hover:text-ink disabled:text-ink-3"
      }`}
      onClick={() => (recording ? dictation.stop() : void dictation.start())}
    >
      {recording ? <Square size={13} fill="currentColor" strokeWidth={0} /> : <Mic size={17} />}
    </button>
  );
}

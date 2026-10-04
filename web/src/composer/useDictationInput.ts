import { useEffect, useRef, type RefObject } from "react";
import { insertDictation, useDictation } from "../voice";

// useDictationInput wires the mic to the textarea: a finished take is spliced in
// at the caret (or over the selection) and never sent, and Esc drops a take
// wherever focus is.
export function useDictationInput(
  textareaRef: RefObject<HTMLTextAreaElement | null>,
  text: string,
  setText: (val: string) => void,
  onTranscribe?: (audio: Uint8Array, mime: string) => Promise<string>,
) {
  const textRef = useRef(text);
  textRef.current = text;
  const dictation = useDictation(
    (audio, mime) => (onTranscribe ? onTranscribe(audio, mime) : Promise.reject(new Error("Dictation is off."))),
    (spoken) => {
      // Splice at the caret (or over the selection) the textarea last had.
      const el = textareaRef.current;
      const cur = textRef.current;
      const r = insertDictation(cur, el?.selectionStart ?? cur.length, el?.selectionEnd ?? cur.length, spoken);
      setText(r.text);
      requestAnimationFrame(() => {
        el?.focus();
        el?.setSelectionRange(r.caret, r.caret);
      });
    },
  );
  const recording = dictation.state === "recording";
  const cancelTake = dictation.cancel;

  useEffect(() => {
    if (!recording) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        cancelTake();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [recording, cancelTake]);

  return dictation;
}

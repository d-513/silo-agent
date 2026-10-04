import { useLayoutEffect, useState, type ClipboardEvent, type DragEvent, type FormEvent, type KeyboardEvent, type RefObject } from "react";
import { growHeight } from "./model";

const nativeGrow = typeof CSS !== "undefined" && CSS.supports?.("field-sizing", "content");

// useComposerInput is what the composer does with keys, paste, drops and the
// form submit: Enter sends, Esc stops a reply, files can be pasted or dropped
// while the Bot's machine is up, and the textarea grows with its text.
export function useComposerInput({
  textareaRef,
  text,
  attCount,
  chatId,
  workerConnected,
  sending,
  recording,
  onSend,
  onStop,
  onAttach,
}: {
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  text: string;
  attCount: number;
  chatId?: string;
  workerConnected?: boolean;
  sending: boolean;
  recording: boolean;
  onSend: () => void;
  onStop: () => void;
  onAttach: (files: FileList | null) => Promise<void> | void;
}) {
  const [isDragging, setIsDragging] = useState(false);

  // field-sizing: content grows the textarea natively; older engines get a
  // scrollHeight fallback clamped to the same 26–168px.
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el || nativeGrow) return;
    el.style.height = "auto";
    el.style.height = `${growHeight(el.scrollHeight)}px`;
  }, [text, textareaRef]);

  const canSend = (text.trim().length > 0 || attCount > 0) && !!chatId;

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Escape" && recording) return; // the window listener cancels the take
    if (e.key === "Escape" && sending) {
      e.preventDefault();
      onStop();
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      if (canSend) onSend();
    }
  };

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (e.clipboardData.files && e.clipboardData.files.length > 0) {
      e.preventDefault();
      void onAttach(e.clipboardData.files);
    }
  };

  const onDragOver = (e: DragEvent) => {
    e.preventDefault();
    if (workerConnected && chatId) setIsDragging(true);
  };

  const onDragLeave = (e: DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0 && workerConnected && chatId) {
      void onAttach(e.dataTransfer.files);
    }
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (sending && !canSend) {
      onStop();
      return;
    }
    if (canSend) onSend();
  };

  return { canSend, isDragging, onKeyDown, onPaste, onDragOver, onDragLeave, onDrop, onSubmit };
}

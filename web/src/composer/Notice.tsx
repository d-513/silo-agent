import { X } from "lucide-react";

// A one-line red notice above the textarea: an attach failure or a dictation
// error (which can be dismissed).
export function Notice({ text, onDismiss }: { text: string; onDismiss?: () => void }) {
  if (!onDismiss) {
    return (
      <div role="alert" className="flex items-center gap-1.5 px-4 pt-2.5 text-[12px] text-vermilion">
        <span className="h-1.5 w-1.5 rounded-full bg-vermilion" />
        <span>{text}</span>
      </div>
    );
  }
  return (
    <div role="alert" className="flex items-center gap-1.5 px-4 pt-2.5 text-[12px] text-vermilion">
      <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
      <span className="min-w-0 truncate">{text}</span>
      <button
        type="button"
        title="Dismiss"
        aria-label="Dismiss"
        className="ml-auto flex h-5 w-5 shrink-0 items-center justify-center rounded-xs text-ink-3 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
        onClick={onDismiss}
      >
        <X size={12} />
      </button>
    </div>
  );
}

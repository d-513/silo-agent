import { X } from "lucide-react";
import { t } from "../i18n.ts";

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
        title={t("Dismiss")}
        aria-label={t("Dismiss")}
        className="ml-auto flex h-5 w-5 shrink-0 items-center justify-center rounded-xs text-ink-3 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
        onClick={onDismiss}
      >
        <X size={12} />
      </button>
    </div>
  );
}

// Said is what a slash command said back: a line, or a question it asks
// before acting (a /wipe), answered by `confirm`.
export type Said = { text: string; confirm?: { label: string; run: () => void } };

// CommandNotice is that line over the textarea, in ink rather than red.
export function CommandNotice({ said, onDismiss }: { said: Said; onDismiss: () => void }) {
  return (
    <div role="status" className="flex items-start gap-2 px-4 pt-2.5 text-[12.5px] leading-[18px] text-ink-2">
      <span className="min-w-0 flex-1 whitespace-pre-line">{said.text}</span>
      {said.confirm ? (
        <button
          type="button"
          onClick={said.confirm.run}
          className="shrink-0 rounded-control bg-vermilion px-2.5 py-0.5 text-[12px] font-medium text-on-accent transition-opacity duration-[160ms] hover:opacity-90"
        >
          {said.confirm.label}
        </button>
      ) : null}
      <button
        type="button"
        title={said.confirm ? t("Cancel") : t("Dismiss")}
        aria-label={said.confirm ? t("Cancel") : t("Dismiss")}
        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-xs text-ink-3 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
        onClick={onDismiss}
      >
        <X size={12} />
      </button>
    </div>
  );
}

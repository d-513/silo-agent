import { SaveButton, type SaveState } from "../Feedback";
import { Btn } from "../Btn";
import { t } from "../i18n.ts";

const SAVE_KEY = typeof navigator !== "undefined" && /Mac|iP(hone|ad)/.test(navigator.platform) ? "⌘S" : "Ctrl+S";

// SaveBar pins the form's Save to the bottom of the viewport while the form is
// in view, and settles under the last panel once the page scrolls past it. It
// is the only save for the panels above; the model list and connector
// variables save on their own.
export function SaveBar({
  dirty,
  state,
  error,
  onSave,
  onDiscard,
}: {
  dirty: number;
  state: SaveState;
  error: string;
  onSave: () => void;
  onDiscard: () => void;
}) {
  const live = dirty > 0;
  return (
    <div className="sticky bottom-3 z-20 mt-5">
      <div
        className={`flex items-center gap-3 rounded-card bg-surface px-4 py-2.5 transition-shadow duration-[160ms] ease-quiet max-wide:flex-wrap ${live ? "shadow-float-focus" : "shadow-float"}`}
      >
        <span className={`size-2 shrink-0 rounded-full ${error ? "bg-vermilion" : live ? "bg-cobalt" : "bg-line-strong"}`} />
        <p className={`min-w-0 flex-1 text-[13px] ${error ? "text-vermilion" : live ? "text-ink" : "text-ink-3"}`}>
          {error
            ? error
            : live
              ? t("{{count}} unsaved changes", { count: dirty })
              : state === "saved"
                ? t("Saved to silo.yaml")
                : t("No unsaved changes")}
        </p>
        <span className="font-mono text-[11px] text-ink-3 max-wide:hidden">{SAVE_KEY}</span>
        {live ? (
          <Btn kind="ghost" size="sm" onClick={onDiscard} disabled={state === "saving"}>
            {t("Discard")}
          </Btn>
        ) : null}
        <SaveButton size="sm" state={state} disabled={!live && state !== "saved"} onClick={onSave}>
          {t("Save changes")}
        </SaveButton>
      </div>
    </div>
  );
}

import { TextQuote, X } from "lucide-react";
import { t } from "../i18n.ts";
import { quoteTitle, type DraftQuote } from "./model";

// The quotes waiting in the composer, each with a remove button.
export function QuoteChips({ quotes, onRemove }: { quotes: DraftQuote[]; onRemove: (id: string) => void }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 pt-3">
      {quotes.map((q) => (
        <div key={q.id} className="rise flex min-w-0 items-center gap-2 rounded-sm bg-well py-1.5 pr-1 pl-2.5 text-[12.5px] text-ink">
          {q.thumb ? (
            <img src={q.thumb} alt="" className="h-8 w-8 shrink-0 rounded-xs object-cover shadow-card" />
          ) : (
            <TextQuote size={13} className="shrink-0 text-ink-2" />
          )}
          <span className="min-w-0 flex-1">
            <span className="block truncate font-medium">
              {quoteTitle(q)}
              {q.locator && !q.image ? <span className="font-normal text-ink-3"> · {q.locator}</span> : null}
            </span>
            {q.text ? <span className="block truncate text-ink-2">{q.text.replace(/\s+/g, " ")}</span> : null}
          </span>
          <button
            type="button"
            title={t("Remove quote")}
            aria-label={t("Remove quote")}
            className="flex h-6 w-6 shrink-0 items-center justify-center rounded-xs text-ink-2 transition-colors duration-[160ms] hover:bg-pressed hover:text-ink"
            onClick={() => onRemove(q.id)}
          >
            <X size={12} />
          </button>
        </div>
      ))}
    </div>
  );
}

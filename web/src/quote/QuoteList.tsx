import { Image as ImageIcon } from "lucide-react";
import type { Quote } from "./model";
import { quoteTitle } from "./model";
import { t } from "../i18n.ts";

// What a sent message quoted, set off above its words.
export function QuoteList({ quotes }: { quotes: Quote[] }) {
  return (
    <div className="mb-2 flex flex-col gap-1.5">
      {quotes.map((q, i) => (
        <div key={i} className="min-w-0 border-l-2 border-line-strong pl-2.5">
          <div className="truncate text-[12px] leading-4 font-medium text-ink-2">
            {quoteTitle(q)}
            {q.locator && !q.image ? <span className="font-normal text-ink-3"> · {q.locator}</span> : null}
          </div>
          {q.image ? (
            <div className="mt-0.5 flex items-center gap-1.5 text-[12.5px] text-ink-3">
              <ImageIcon size={12} />
              <span className="truncate font-mono text-[11.5px]">{q.image}</span>
              <span>{t("a region of it")}</span>
            </div>
          ) : (
            <div className="mt-0.5 line-clamp-4 text-[13.5px] leading-5 whitespace-pre-wrap text-ink-2">{q.text}</div>
          )}
        </div>
      ))}
    </div>
  );
}

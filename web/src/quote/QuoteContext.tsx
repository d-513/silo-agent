import { createContext, useContext } from "react";
import type { Quote } from "./model";

// Where a quotable surface sends a quote: the Bot page's composer draft, and
// how a cropped region reaches the Bot's machine.
export type QuoteSink = {
  add: (q: Quote & { thumb?: string }) => void;
  // Upload a crop to /workspace/tmp; answers its workspace path.
  upload: (data: Uint8Array, name: string) => Promise<string>;
  // A crop needs the machine up.
  canUpload: boolean;
};

const Ctx = createContext<QuoteSink | null>(null);

export const QuoteProvider = Ctx.Provider;

// useQuoteSink is the page's sink, or null where nothing takes quotes.
export function useQuoteSink(): QuoteSink | null {
  return useContext(Ctx);
}

// QuoteTarget names what a surface shows: a quote of it carries these, plus the
// selected text. text is the whole of it, so a quote can say which lines.
export type QuoteTarget = { source: Quote["source"]; ref: string; label: string; text?: string };

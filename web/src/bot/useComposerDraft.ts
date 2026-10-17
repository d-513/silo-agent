import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { joinPath } from "../fs";
import { t } from "../i18n.ts";
import { addQuote, type DraftQuote, type Quote } from "../quote/model";

export type Att = { name: string; path: string; size: number };

// The composer's unsent state: the text, the files already uploaded to the
// Bot's tmp folder, and what the human quoted. It lives above the tab pages so
// a draft survives leaving the chat and coming back, and so a quote taken in
// the Files pane or an artifact lands in it.
export function useComposerDraft(botId: string, workerConnected: boolean | undefined) {
  const [text, setText] = useState("");
  const [atts, setAtts] = useState<Att[]>([]);
  const [attachErr, setAttachErr] = useState("");
  const [quotes, setQuotes] = useState<DraftQuote[]>([]);

  async function attach(list: FileList | null) {
    if (!list?.length || !workerConnected) return;
    setAttachErr("");
    const added: Att[] = [];
    for (const f of list) {
      if (f.size > 50 << 20) {
        setAttachErr(t("{{name}} is larger than 50 MB", { name: f.name }));
        continue;
      }
      const path = joinPath("tmp", f.name);
      if (!path) continue;
      try {
        const buf = new Uint8Array(await f.arrayBuffer());
        await ui.putFile({ botId, path, data: buf });
        added.push({ name: f.name, path, size: f.size });
      } catch (ex) {
        setAttachErr(fail(ex));
      }
    }
    if (added.length) {
      setAtts((xs) => {
        const seen = new Set(xs.map((x) => x.path));
        return [...xs, ...added.filter((a) => !seen.has(a.path))];
      });
    }
  }

  const quote = (q: Quote & { thumb?: string }) => setQuotes((xs) => addQuote(xs, q));
  const unquote = (id: string) => setQuotes((xs) => xs.filter((x) => x.id !== id));

  // A crop of an image: uploaded like an attachment, and quoted.
  async function upload(data: Uint8Array, name: string) {
    const path = joinPath("tmp", name);
    await ui.putFile({ botId, path, data });
    return path;
  }

  return { text, setText, atts, setAtts, attachErr, attach, quotes, setQuotes, quote, unquote, upload };
}

export type ComposerDraft = ReturnType<typeof useComposerDraft>;

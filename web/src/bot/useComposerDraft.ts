import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { joinPath } from "../fs";

export type Att = { name: string; path: string; size: number };

// The composer's unsent state: the text, and the files already uploaded to the
// Bot's tmp folder. It lives above the tab pages so a draft survives leaving the
// chat and coming back.
export function useComposerDraft(botId: string, workerConnected: boolean | undefined) {
  const [text, setText] = useState("");
  const [atts, setAtts] = useState<Att[]>([]);
  const [attachErr, setAttachErr] = useState("");

  async function attach(list: FileList | null) {
    if (!list?.length || !workerConnected) return;
    setAttachErr("");
    const added: Att[] = [];
    for (const f of list) {
      if (f.size > 50 << 20) {
        setAttachErr(`${f.name} is larger than 50 MB`);
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

  return { text, setText, atts, setAtts, attachErr, attach };
}

export type ComposerDraft = ReturnType<typeof useComposerDraft>;

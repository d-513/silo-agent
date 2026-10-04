import { useEffect, useState } from "react";

type Slot<T> = { value?: T; promise?: Promise<T> };

// useLazyModule gives the module once it has loaded. It starts loading when
// `want` is true; a module already loaded is returned at once, so only the
// first message that needs it ever renders without.
function useLazyModule<T>(slot: Slot<T>, load: () => Promise<T>, want: boolean): T | undefined {
  const [, bump] = useState(0);
  useEffect(() => {
    if (!want || slot.value) return;
    let dead = false;
    void (slot.promise ??= load().then((m) => (slot.value = m))).then(() => {
      if (!dead) bump((n) => n + 1);
    });
    return () => {
      dead = true;
    };
  }, [want]);
  return slot.value;
}

const math: Slot<typeof import("./mdMath")> = {};
const code: Slot<typeof import("./mdCode")> = {};

// Does the text have math ($…$, \(…\), \[…\], \begin{…})?
const hasMath = (text: string) => /\$|\\\(|\\\[|\\begin\{/.test(text);
// Does it have a fenced code block?
const hasCode = (text: string) => text.includes("```") || text.includes("~~~");

// useMdModules loads KaTeX and the code highlighter only for messages that use them.
export function useMdModules(text: string) {
  return {
    math: useLazyModule(math, () => import("./mdMath"), hasMath(text)),
    code: useLazyModule(code, () => import("./mdCode"), hasCode(text)),
  };
}

// preloadMarkdown fetches the code highlighter when the browser is idle: replies
// with code are common, math is not.
export function preloadMarkdown() {
  const go = () => void (code.promise ??= import("./mdCode").then((m) => (code.value = m)));
  if (typeof window !== "undefined" && "requestIdleCallback" in window) window.requestIdleCallback(go, { timeout: 4000 });
  else setTimeout(go, 1500);
}

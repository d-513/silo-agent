import { useEffect, useState } from "react";

export function escapeHtml(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

type Heavy = typeof import("./hljs");
let heavy: Heavy | undefined;
let loading: Promise<Heavy> | undefined;

// loadHighlighter fetches highlight.js (its own chunk) once.
export function loadHighlighter(): Promise<Heavy> {
  return (loading ??= import("./hljs").then((m) => (heavy = m)));
}

// useHighlight returns HTML for code in lang. Until highlight.js has loaded (the
// first time code is shown) it is the escaped text, then it upgrades in place.
export function useHighlight(code: string, lang?: string): string {
  const [, bump] = useState(0);
  useEffect(() => {
    if (heavy || !lang) return;
    let dead = false;
    void loadHighlighter().then(() => {
      if (!dead) bump((n) => n + 1);
    });
    return () => {
      dead = true;
    };
  }, [lang]);
  return heavy ? heavy.highlight(code, lang) : escapeHtml(code);
}

export function langFromPath(path: string) {
  const ext = path.split(".").pop()?.toLowerCase();
  const map: Record<string, string> = {
    py: "python",
    sh: "bash",
    bash: "bash",
    json: "json",
    ts: "typescript",
    tsx: "typescript",
    js: "javascript",
    jsx: "javascript",
    go: "go",
    html: "xml",
    htm: "xml",
    xml: "xml",
    svg: "xml",
    md: "markdown",
    markdown: "markdown",
    diff: "diff",
    patch: "diff",
    yml: "yaml",
    yaml: "yaml",
  };
  return ext ? map[ext] : undefined;
}

export function resultLang(s: string) {
  const t = s.trim();
  if (t.startsWith("{") || t.startsWith("[")) {
    try {
      JSON.parse(t);
      return "json";
    } catch {
      /* not json */
    }
  }
  if (/^(def |class |import |from |Traceback)/m.test(t)) return "python";
  if (/^(\$ |#!\/)/.test(t)) return "bash";
  return langFromPath(t.split("\n")[0] ?? "") || undefined;
}

import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import css from "highlight.js/lib/languages/css";
import diff from "highlight.js/lib/languages/diff";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";

// The languages the app highlights. They are registered once on highlight.js's
// core, and the same map goes to rehype-highlight for fenced code in replies.
export const hlLangs = {
  python,
  bash,
  json,
  typescript,
  javascript,
  go,
  xml,
  html: xml,
  markdown,
  diff,
  yaml,
  css,
};

for (const [name, fn] of Object.entries(hlLangs)) {
  hljs.registerLanguage(name, fn);
}

function escapeHtml(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// highlight returns HTML for code in lang, or the escaped text when the
// language is unknown or highlighting fails.
export function highlight(code: string, lang?: string) {
  if (lang && hljs.getLanguage(lang)) {
    try {
      return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
    } catch {
      /* plain */
    }
  }
  return escapeHtml(code);
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

import { ArrowDown, Check, ChevronRight, Copy, Download, GitBranch, MessageCircle, Paperclip, Pencil, RotateCcw, ShieldCheck, ShieldX, Trash2, X } from "lucide-react";
import hljs from "highlight.js/lib/core";
import "katex/dist/katex.min.css";
import rehypeKatex from "rehype-katex";
import remarkMath from "remark-math";
import bash from "highlight.js/lib/languages/bash";
import diff from "highlight.js/lib/languages/diff";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";
import { useEffect, useRef, useState, type JSX, type ReactNode } from "react";
import Markdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";
import { ui } from "./api";
import { ArtifactCard, downloadArtifact, TypeBadge, type Artifact } from "./Artifact";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
import { CopyButton, Spinner, useArmed } from "./Feedback";
import { kindLabel } from "./fileKind";
import { downloadFile, FilePreview } from "./FilePreview";
import { fmtSize } from "./fs";
import { foldEvents, type Attachment, type Block, type Decision, type Ev, type ReceiptBlock } from "./fold";
import { classNames, isDisplayMath, mathTex, normalizeLatex, rehypeMathCopy, type HastNode } from "./latex";

export type { Ev };

const hlLangs = {
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
};

for (const [name, fn] of Object.entries(hlLangs)) {
  hljs.registerLanguage(name, fn);
}

const mdHighlight: [[typeof rehypeHighlight, { languages: typeof hlLangs }]] = [
  [rehypeHighlight, { languages: hlLangs }],
];

function unescapeJsonTail(s: string): string {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === '"') break;
    if (c === "\\" && i + 1 < s.length) {
      const n = s[++i];
      if (n === "n") out += "\n";
      else if (n === "t") out += "\t";
      else if (n === "r") out += "\r";
      else if (n === '"' || n === "\\" || n === "/") out += n;
      else if (n === "u" && i + 4 < s.length) {
        out += String.fromCharCode(parseInt(s.slice(i + 1, i + 5), 16));
        i += 4;
      } else out += n;
      continue;
    }
    out += c;
  }
  return out;
}

function extractStringField(raw: string, key: string): string | undefined {
  for (const needle of [`"${key}": "`, `"${key}":"`]) {
    const i = raw.indexOf(needle);
    if (i >= 0) return unescapeJsonTail(raw.slice(i + needle.length));
  }
}

function parseToolArgs(raw: string): Record<string, unknown> {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw);
    if (v && typeof v === "object" && !Array.isArray(v)) return v as Record<string, unknown>;
  } catch {
    /* stream */
  }
  const out: Record<string, unknown> = {};
  for (const key of ["code", "command", "content", "path", "pattern", "old_text", "new_text", "include", "append", "text", "query", "channel", "to", "chat", "name"]) {
    const v = extractStringField(raw, key);
    if (v !== undefined) out[key] = v;
  }
  for (const key of ["offset", "limit", "max_hits"]) {
    const m = raw.match(new RegExp(`"${key}":\\s*(-?\\d+)`));
    if (m) out[key] = Number(m[1]);
  }
  return out;
}

function asStr(v: unknown): string {
  return typeof v === "string" ? v : v == null ? "" : String(v);
}

function prettyJson(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

function escapeHtml(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function highlight(code: string, lang?: string) {
  if (lang && hljs.getLanguage(lang)) {
    try {
      return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
    } catch {
      /* plain */
    }
  }
  return escapeHtml(code);
}

function langFromPath(path: string) {
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

function resultLang(s: string) {
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

function CodeBlock({ code, lang }: { code: string; lang?: string }) {
  const displayLang = lang || (resultLang(code) ?? "");
  return (
    <div className="overflow-hidden rounded-sm bg-well">
      <div className="flex h-8 items-center justify-between pr-1 pl-3 shadow-[inset_0_-1px_0_var(--color-line)]">
        <span className="text-[11px] leading-4 font-medium tracking-[0.08em] text-ink-3 uppercase">{displayLang || "code"}</span>
        <CopyButton text={code} title="Copy code" label size={12} />
      </div>
      <pre className="whitespace-pre-wrap break-words p-3 font-mono text-[12.5px] leading-5">
        <code className="hljs whitespace-pre-wrap break-words" dangerouslySetInnerHTML={{ __html: highlight(code, lang) }} />
      </pre>
    </div>
  );
}

// Tool rows read as "Using Python" / "Used Files"; the action is the mono tail.
function toolMeta(name: string): { app: string } {
  switch (name) {
    case "exec_python":
      return { app: "Python" };
    case "terminal":
      return { app: "Terminal" };
    case "read":
    case "write":
    case "patch":
    case "grep":
    case "delete":
    case "present":
      return { app: "Files" };
    case "look":
    case "click":
    case "type":
    case "key":
    case "scroll":
      return { app: "Desktop" };
    case "soul":
      return { app: "SOUL" };
    case "memory":
    case "core_memory":
      return { app: "CORE MEMORY" };
    case "skill":
      return { app: "Skills" };
    case "artifact":
      return { app: "Artifact" };
    case "web_search":
      return { app: "Web search" };
    case "channel":
      return { app: "Channels" };
    case "chats":
      return { app: "Chats" };
    case "list_models":
    case "switch_model":
      return { app: "Model" };
    default:
      return { app: name };
  }
}

function firstLine(s: string) {
  return s.split("\n").find((l) => l.trim())?.trim() ?? "";
}

function toolAction(name: string, raw: string): string {
  const a = parseToolArgs(raw);
  const path = asStr(a.path);
  switch (name) {
    case "exec_python":
      return firstLine(asStr(a.code));
    case "terminal": {
      const c = firstLine(asStr(a.command));
      return c ? `$ ${c}` : "";
    }
    case "read":
    case "write":
    case "patch":
    case "delete":
      return `${name} ${path}`.trim();
    case "grep":
      return `grep ${asStr(a.pattern)}`.trim();
    case "present":
      return path;
    case "click":
      return a.x != null ? `click ${asStr(a.x)},${asStr(a.y)}` : "click";
    case "scroll":
      return a.dy != null ? `scroll ${asStr(a.dy)}` : "scroll";
    case "type":
      return "type";
    case "key":
      return `key ${asStr(a.name)}`.trim();
    case "web_search":
      return asStr(a.query);
    case "skill":
      return asStr(a.name) || path;
    case "channel":
      return asStr(a.channel) || asStr(a.to);
    case "list_models":
      return "list";
    case "switch_model":
      return asStr(a.model);
    default:
      return "";
  }
}

type RowState = "running" | "done" | "waiting" | "stopped";

function rowState(t: { running?: boolean; waiting?: boolean; outcome?: string; result?: string }): RowState {
  if (t.waiting) return "waiting";
  if (t.outcome) return "stopped";
  if (t.running) return "running";
  return "done";
}

function rowVerb(state: RowState, outcome: string | undefined, live: string, past: string) {
  if (outcome === "denied") return "Skipped";
  if (outcome === "stopped") return "Stopped";
  return state === "running" || state === "waiting" ? live : past;
}

// 16px slot that crossfades spinner → check / paused dot / ✕.
function StateSlot({ state }: { state: RowState }) {
  const cell = (on: boolean) =>
    `col-start-1 row-start-1 flex items-center justify-center transition-[opacity,transform] duration-300 ease-settle motion-reduce:transition-opacity ${
      on ? "opacity-100" : "scale-[.6] opacity-0"
    }`;
  return (
    <span className="grid h-4 w-4 shrink-0 place-items-center" aria-hidden>
      <span className={cell(state === "running")}>{state === "running" ? <Spinner size={13} /> : null}</span>
      <span className={cell(state === "done")}>
        <Check size={15} strokeWidth={2.25} className="text-emerald" />
      </span>
      <span className={cell(state === "waiting")}>
        <span className="breathe h-[7px] w-[7px] rounded-full bg-vermilion" />
      </span>
      <span className={cell(state === "stopped")}>
        <X size={14} className="text-ink-3" />
      </span>
    </span>
  );
}

// grid-template-rows 0fr → 1fr fold; contents fade in 60ms behind the height.
// Children mount on first open so closed rows cost nothing to render.
function Fold({ open, children }: { open: boolean; children: ReactNode }) {
  const [mounted, setMounted] = useState(open);
  if (open && !mounted) setMounted(true);
  return (
    <div
      className={`grid transition-[grid-template-rows] duration-[320ms] ease-quiet motion-reduce:transition-none ${
        open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
      }`}
    >
      <div
        className={`min-h-0 overflow-hidden transition-opacity ease-quiet ${
          open ? "opacity-100 delay-[60ms] duration-[260ms]" : "opacity-0 duration-[120ms]"
        }`}
        inert={!open}
      >
        {mounted ? children : null}
      </div>
    </div>
  );
}

function FoldRow({
  lead,
  title,
  tail,
  children,
}: {
  lead?: ReactNode;
  title: ReactNode;
  tail?: ReactNode;
  children?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const has = !!children;
  return (
    <div
      className={`max-w-full rounded-card transition-[background-color,box-shadow] duration-[200ms] ease-quiet ${
        open ? "bg-surface shadow-card" : ""
      }`}
    >
      <button
        type="button"
        aria-expanded={has ? open : undefined}
        disabled={!has}
        onClick={() => setOpen((v) => !v)}
        className={`flex h-9 w-full min-w-0 items-center gap-2.5 rounded-card px-3 text-left text-[13px] transition-colors duration-[160ms] ease-quiet disabled:cursor-default ${
          open ? "" : "enabled:hover:bg-well"
        }`}
      >
        {lead}
        {title}
        {tail}
        <span className="min-w-2 flex-1" />
        {has ? (
          <ChevronRight
            size={14}
            className={`shrink-0 text-ink-3 transition-transform duration-[320ms] ease-quiet ${open ? "rotate-90" : ""}`}
          />
        ) : null}
      </button>
      {has ? (
        <Fold open={open}>
          <div className="space-y-2 px-3 pb-3">{children}</div>
        </Fold>
      ) : null}
    </div>
  );
}

function ToolRow({
  state,
  verb,
  app,
  action,
  children,
}: {
  state: RowState;
  verb: string;
  app: string;
  action?: string;
  children?: ReactNode;
}) {
  return (
    <FoldRow
      lead={<StateSlot state={state} />}
      title={
        <span className="shrink-0 font-medium text-ink">
          {verb} {app}
        </span>
      }
      tail={
        <>
          {state === "waiting" ? <span className="shrink-0 text-[12.5px] text-vermilion">Waiting for you</span> : null}
          {action ? <span className="min-w-0 truncate font-mono text-[12.5px] text-ink-3">{action}</span> : null}
        </>
      }
    >
      {children}
    </FoldRow>
  );
}

const receiptWord: Record<Decision, string> = {
  allow_once: "Allowed once",
  always: "Always allowed",
  deny: "Denied",
  stopped: "Stopped",
};

// One line per decision: shield, verdict, "· action · target", a hairline, "by you".
function Receipt({ b }: { b: ReceiptBlock }) {
  const allowed = b.decision === "allow_once" || b.decision === "always";
  const Icon = allowed ? ShieldCheck : ShieldX;
  return (
    <div className="flex min-w-0 items-center gap-2 px-3 text-[12.5px] leading-[18px]">
      <Icon size={15} className={`shrink-0 ${allowed ? "text-emerald" : "text-ink-3"}`} aria-hidden />
      <span className="shrink-0 font-medium text-ink">{receiptWord[b.decision] ?? b.decision}</span>
      <span className="min-w-0 truncate text-ink-3">
        {b.title ? ` · ${b.title}` : ""}
        {b.target ? (
          <>
            {" · "}
            <span className="font-mono">{b.target}</span>
          </>
        ) : null}
      </span>
      <span aria-hidden className="h-px min-w-6 flex-1 bg-line" />
      <span className="shrink-0 text-ink-3">by you</span>
    </div>
  );
}

function Thinking({ text, streaming, ms }: { text: string; streaming: boolean; ms?: number }) {
  if (streaming) {
    return (
      <div className="flex h-9 items-center px-3 text-[13px] font-medium">
        <span className="shimmer-text">Thinking</span>
      </div>
    );
  }
  const secs = ms && ms >= 1000 ? Math.round(ms / 1000) : 0;
  return (
    <FoldRow title={<span className="shrink-0 font-medium text-ink-2">{secs ? `Thought for ${secs}s` : "Thought"}</span>}>
      {text.trim() ? <div className="whitespace-pre-wrap break-words text-[13.5px] leading-[22px] text-ink-2">{text}</div> : null}
    </FoldRow>
  );
}

function ToolInput({ name, args, running }: { name: string; args: string; running?: boolean }) {
  if (!args) return null;
  const a = parseToolArgs(args);
  const path = asStr(a.path);
  const command = asStr(a.command);
  const content = asStr(a.content);
  const code = asStr(a.code);
  const pattern = asStr(a.pattern);
  const include = asStr(a.include);
  const oldText = asStr(a.old_text);
  const newText = asStr(a.new_text);
  const append = asStr(a.append);
  const offset = a.offset;
  const limit = a.limit;
  const x = a.x;
  const y = a.y;
  const button = asStr(a.button);
  const typeText = asStr(a.text);
  const keyName = asStr(a.name);
  const dy = a.dy;

  let body: JSX.Element | null = null;
  if (name === "exec_python" && code) {
    body = <CodeBlock code={code} lang="python" />;
  } else if (name === "terminal" && command) {
    body = <CodeBlock code={command} lang="bash" />;
  } else if (name === "patch" && (path || oldText || newText)) {
    const lines = [
      ...(oldText ? oldText.split("\n").map((l) => `-${l}`) : []),
      ...(newText ? newText.split("\n").map((l) => `+${l}`) : []),
    ].join("\n");
    body = (
      <div className="space-y-2">
        {path ? <div className="font-mono text-[12.5px]">{path}</div> : null}
        {oldText || newText ? <CodeBlock code={lines} lang="diff" /> : null}
      </div>
    );
  } else if (name === "write" && (path || content)) {
    body = (
      <div className="space-y-2">
        {path ? <div className="font-mono text-[12.5px]">{path}</div> : null}
        {content ? <CodeBlock code={content} lang={langFromPath(path)} /> : null}
      </div>
    );
  } else if (name === "read" && (path || offset != null || limit != null)) {
    body = (
      <div className="space-y-1 rounded-sm bg-well p-3 font-mono text-[12.5px] leading-5">
        {path ? <div>{path}</div> : null}
        {offset != null ? <div className="text-ink-3">offset {asStr(offset)}</div> : null}
        {limit != null ? <div className="text-ink-3">limit {asStr(limit)}</div> : null}
      </div>
    );
  } else if ((name === "soul" || name === "memory" || name === "core_memory") && (content || append || oldText || newText)) {
    if (oldText || newText) {
      const lines = [
        ...(oldText ? oldText.split("\n").map((l) => `-${l}`) : []),
        ...(newText ? newText.split("\n").map((l) => `+${l}`) : []),
      ].join("\n");
      body = <CodeBlock code={lines} lang="diff" />;
    } else {
      body = <div className="whitespace-pre-wrap rounded-sm bg-well p-3 font-mono text-[12.5px] leading-5">{content || append}</div>;
    }
  } else if (name === "skill") {
    const skillName = asStr(a.name);
    body = (
      <div className="font-mono text-[12.5px]">
        {skillName || path}
      </div>
    );
  } else if (name === "artifact" && path) {
    body = <div className="font-mono text-[12.5px]">{path}</div>;
  } else if (name === "present" && path) {
    body = <div className="font-mono text-[12.5px]">{path}</div>;
  } else if (name === "click" && (x != null || y != null)) {
    body = (
      <div className="font-mono text-[12.5px]">
        {asStr(x)},{asStr(y)}
        {button && button !== "left" ? ` ${button}` : ""}
      </div>
    );
  } else if (name === "scroll" && (x != null || y != null || dy != null)) {
    body = (
      <div className="font-mono text-[12.5px]">
        {asStr(x)},{asStr(y)} dy {asStr(dy)}
      </div>
    );
  } else if (name === "type" && typeText) {
    body = <div className="whitespace-pre-wrap rounded-sm bg-well p-3 font-mono text-[12.5px] leading-5">{typeText}</div>;
  } else if (name === "key" && keyName) {
    body = <div className="font-mono text-[12.5px]">{keyName}</div>;
  } else if (name === "grep" && (pattern || path || include)) {
    body = (
      <div className="space-y-1 rounded-sm bg-well p-3 font-mono text-[12.5px] leading-5">
        {pattern ? <div>{pattern}</div> : null}
        {path ? <div className="text-ink-3">{path}</div> : null}
        {include ? <div className="text-ink-3">{include}</div> : null}
      </div>
    );
  } else if (name === "web_search" && asStr(a.query)) {
    body = <div className="font-mono text-[12.5px]">{asStr(a.query)}</div>;
  } else if (path || command) {
    body = (
      <div className="space-y-2">
        {path ? <div className="font-mono text-[12.5px]">{path}</div> : null}
        {command ? <CodeBlock code={command} lang="bash" /> : null}
      </div>
    );
  }

  if (body) return body;
  if (running && !Object.keys(a).length) return null;
  const dump = Object.keys(a).length ? JSON.stringify(a, null, 2) : prettyJson(args);
  if (!dump) return null;
  return <pre className="overflow-x-auto whitespace-pre-wrap rounded-sm bg-well p-3 font-mono text-[12.5px] leading-5">{dump}</pre>;
}

function ToolResult({ text }: { text: string }) {
  const sliced = text.length > 4000 ? `${text.slice(0, 4000)}…` : text;
  const lang = resultLang(sliced);
  const inner = lang ? (
    <pre className="whitespace-pre-wrap break-words font-mono text-[12.5px] leading-5 text-ink-2">
      <code className="hljs" dangerouslySetInnerHTML={{ __html: highlight(sliced, lang) }} />
    </pre>
  ) : (
    <pre className="whitespace-pre-wrap font-mono text-[12.5px] leading-5 text-ink-2">{sliced}</pre>
  );
  const long = sliced.length > 800 || sliced.split("\n").length > 16;
  if (!long) return inner;
  return (
    <details>
      <summary className="cursor-pointer text-[12px] font-medium text-ink-3 hover:text-ink">Result</summary>
      <div className="mt-2">{inner}</div>
    </details>
  );
}

function MathCopy({ node, children }: { node?: HastNode; children?: ReactNode }) {
  const display = isDisplayMath(node);
  const tex = mathTex(node);
  const [copied, setCopied] = useState(false);
  const classes = classNames(node?.properties).filter((c) => c !== "silo-math");
  const copy = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(tex);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard unavailable */
    }
  };
  return (
    <span className={[...classes, "silo-math", "group/math", "relative", display ? "block" : "inline"].join(" ")}>
      {children}
      {tex ? (
        <button
          type="button"
          title={copied ? "Copied LaTeX" : "Copy LaTeX"}
          aria-label={copied ? "Copied LaTeX" : "Copy LaTeX"}
          onClick={copy}
          className={`silo-math-copy absolute z-10 inline-flex items-center justify-center rounded-xs bg-surface text-ink-2 shadow-card transition-[opacity,color,background-color] duration-[160ms] hover:bg-well hover:text-ink ${
            display
              ? "h-5 w-5 top-1 right-1"
              : "h-4 w-4 -top-2.5 -right-1.5 opacity-0 focus-visible:opacity-100 group-hover/math:opacity-100"
          }`}
        >
          {copied ? (
            <Check size={display ? 11 : 10} className="text-emerald" />
          ) : (
            <Copy size={display ? 11 : 10} />
          )}
        </button>
      ) : null}
    </span>
  );
}

type PosNode = HastNode & { position?: { start?: { offset?: number }; end?: { offset?: number } } };

const SKIP_CHUNKS = new Set(["pre", "code", "math", "svg", "annotation"]);

// While a reply streams, wrap each delta (by its source offset) in a
// <span class="chunk"> so only the new words fade in. Existing spans keep their
// place in the tree, so React leaves them — and their finished animation — alone.
function rehypeChunks(bounds: number[]) {
  return () => (tree: PosNode) => {
    const chunkAt = (off: number) => {
      let i = 0;
      while (i + 1 < bounds.length && bounds[i + 1] <= off) i++;
      return i;
    };
    const walk = (node: PosNode) => {
      const kids = node.children as PosNode[] | undefined;
      if (!kids) return;
      const out: PosNode[] = [];
      for (const c of kids) {
        if (c.type === "element") {
          const cls = classNames(c.properties);
          if (!SKIP_CHUNKS.has(c.tagName ?? "") && !cls.includes("katex") && !cls.includes("silo-math")) walk(c);
          out.push(c);
          continue;
        }
        const s = c.position?.start?.offset;
        const e = c.position?.end?.offset;
        const v = c.value ?? "";
        if (c.type !== "text" || s == null || e == null || e - s !== v.length || !v) {
          out.push(c);
          continue;
        }
        let from = s;
        let i = chunkAt(s);
        while (from < e) {
          const to = Math.min(e, i + 1 < bounds.length ? bounds[i + 1] : e);
          const piece = v.slice(from - s, to - s);
          if (piece) {
            out.push({ type: "element", tagName: "span", properties: { className: ["chunk"] }, children: [{ type: "text", value: piece }] });
          }
          from = to;
          i++;
        }
      }
      node.children = out;
    };
    walk(tree);
  };
}

function Md({ text, bounds }: { text: string; bounds?: number[] }) {
  if (!text) return null;
  const plugins = bounds && bounds.length > 1 ? [...mdRehype, rehypeChunks(bounds)] : mdRehype;
  return (
    <div className="silo-md">
      <Markdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={plugins}
        components={{
          table: ({ children }) => (
            <div className="silo-md-table">
              <table>{children}</table>
            </div>
          ),
          span: ({ node, children, ...rest }) => {
            if (classNames(node?.properties).includes("silo-math")) {
              return <MathCopy node={node}>{children}</MathCopy>;
            }
            return <span {...rest}>{children}</span>;
          },
        }}
      >
        {normalizeLatex(text)}
      </Markdown>
    </div>
  );
}

const mdRehype = [rehypeKatex, rehypeMathCopy, ...mdHighlight] as NonNullable<Parameters<typeof Markdown>[0]["rehypePlugins"]>;

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function isBotScratch(path: string): boolean {
  const p = path.replace(/^\/+/, "").replace(/^(workspace\/)+/, "");
  return p === "bot" || p.startsWith("bot/");
}

type LoadedFile = { name: string; content: string; data?: Uint8Array; binary: boolean; truncated: boolean };

function useWorkspaceFile(botId: string, path: string) {
  const [file, setFile] = useState<LoadedFile | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    ui.readFile({ botId, path })
      .then((r) => {
        if (!dead) setFile({ name: r.name, content: r.content, data: r.data, binary: r.binary, truncated: r.truncated });
      })
      .catch((ex) => {
        if (!dead) setErr(fail(ex));
      });
    return () => {
      dead = true;
    };
  }, [botId, path]);
  return { file, err };
}

function fileSize(f: LoadedFile) {
  return f.data ? f.data.length : new TextEncoder().encode(f.content).length;
}

// `present` of a bot/… path or a `look` screenshot: the preview inside a folded row.
function QuietPreview({ botId, path }: { botId: string; path: string }) {
  const { file, err } = useWorkspaceFile(botId, path);
  return (
    <div className="space-y-2">
      {file?.truncated ? <p className="text-[12.5px] text-ink-3">Showing the first 2 MB.</p> : null}
      {err ? <p className="text-[13px] text-vermilion">{err}</p> : null}
      {!file && !err ? <div className="skeleton h-40 rounded-sm" /> : null}
      {file ? <FilePreview name={file.name} content={file.content} data={file.data} binary={file.binary} /> : null}
    </div>
  );
}

// `present` of a user-facing path shows the file itself in a card.
function PresentFile({ botId, path }: { botId: string; path: string }) {
  const { file, err } = useWorkspaceFile(botId, path);
  const name = file?.name || path.split("/").filter(Boolean).pop() || path;
  return (
    <div className="max-w-full overflow-hidden rounded-card bg-surface shadow-card">
      <div className="flex items-center gap-3 px-3 py-2.5">
        <TypeBadge name={name} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13.5px] leading-5 font-medium text-ink">{name}</div>
          <div className="truncate text-[12.5px] leading-[18px] text-ink-3">
            {kindLabel(name)}
            {file ? ` · ${fmtSize(fileSize(file))}` : ""} · in Files
          </div>
        </div>
        {file ? (
          <Btn
            kind="ghost"
            size="sm"
            iconOnly
            title="Download"
            aria-label="Download"
            icon={<Download size={15} />}
            onClick={() => downloadFile(file.name, file.content, file.data)}
          />
        ) : null}
      </div>
      <div className="space-y-2 px-3 pb-3">
        {file?.truncated ? <p className="text-[12.5px] text-ink-3">Showing the first 2 MB.</p> : null}
        {err ? <p className="text-[13px] text-vermilion">{err}</p> : null}
        {!file && !err ? <div className="skeleton h-40 rounded-sm" /> : null}
        {file ? <FilePreview name={file.name} content={file.content} data={file.data} binary={file.binary} /> : null}
      </div>
    </div>
  );
}

const miniBtn =
  "flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:opacity-40";

function UserBubble({
  text,
  attachments,
  isLast,
  busy,
  fresh,
  onEdit,
  onDelete,
  onDiverge,
}: {
  text: string;
  attachments?: Attachment[];
  isLast?: boolean;
  busy?: boolean;
  fresh?: boolean;
  onEdit?: (text: string, attachments?: Attachment[]) => void;
  onDelete?: () => void;
  onDiverge?: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(text);
  const del = useArmed();

  const startEdit = () => {
    setDraft(text);
    setEditing(true);
  };

  const saveEdit = () => {
    const next = draft.trim();
    if ((!next && !attachments?.length) || next === text) {
      setEditing(false);
      return;
    }
    setEditing(false);
    onEdit?.(next, attachments);
  };

  if (editing) {
    return (
      <div className="flex w-full justify-end">
        <div className="w-full max-w-[88%] sm:max-w-[80%]">
          <textarea
            autoFocus
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                saveEdit();
              }
              if (e.key === "Escape") setEditing(false);
            }}
            rows={Math.min(12, Math.max(2, draft.split("\n").length))}
            className="w-full resize-none rounded-bubble rounded-br-[6px] bg-surface px-4 py-2.5 text-[15px] leading-6 text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)] outline-none"
          />
          <div className="mt-1.5 flex items-center justify-end gap-1.5">
            <Btn kind="secondary" size="sm" onClick={() => setEditing(false)}>
              Cancel
            </Btn>
            <Btn kind="primary" size="sm" onClick={saveEdit}>
              Save & resend
            </Btn>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={`group flex w-full flex-col items-end gap-1 ${fresh ? "glide" : ""}`}>
      <div className="min-w-0 max-w-[88%] rounded-bubble rounded-br-[6px] bg-well px-4 py-2.5 text-ink sm:max-w-[80%]">
        <div className="whitespace-pre-wrap break-words text-[15px] leading-6 select-text">{text}</div>
        {attachments?.length ? (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {attachments.map((a) => (
              <span
                key={a.path}
                title={a.path}
                className="inline-flex max-w-full items-center gap-1.5 rounded-sm bg-surface px-2.5 py-1 text-[12px] text-ink shadow-card"
              >
                <Paperclip size={12} className="shrink-0 text-ink-2" />
                <span className="max-w-[180px] truncate font-medium">{a.name}</span>
                <span className="shrink-0 font-mono text-[11px] text-ink-3">{fmtSize(a.size)}</span>
              </span>
            ))}
          </div>
        ) : null}
      </div>
      <div className="flex translate-y-0.5 items-center gap-0.5 opacity-0 transition-[opacity,transform] duration-[180ms] ease-quiet group-focus-within:translate-y-0 group-focus-within:opacity-100 group-hover:translate-y-0 group-hover:opacity-100">
        <CopyButton text={text} title="Copy prompt" size={13} />
        {onEdit && isLast ? (
          <button type="button" onClick={startEdit} disabled={busy} title={busy ? "Stop the Bot to edit" : "Edit and resend"} className={miniBtn}>
            <Pencil size={13} />
          </button>
        ) : null}
        {onDelete && isLast ? (
          <button
            type="button"
            onClick={() => del.fire(() => onDelete())}
            disabled={busy}
            title={busy ? "Stop the Bot to delete" : del.armed ? "Click again to delete" : "Delete"}
            className={`${miniBtn} relative overflow-hidden ${del.armed ? "!bg-vermilion !text-white" : "hover:!text-vermilion"}`}
          >
            <Trash2 size={13} />
            {del.armed ? <span aria-hidden className="drain absolute inset-x-0 bottom-0 h-[2px] bg-white" /> : null}
          </button>
        ) : null}
        {onDiverge ? (
          <button type="button" onClick={onDiverge} disabled={busy} title="Diverge into a new chat" className={miniBtn}>
            <GitBranch size={13} />
          </button>
        ) : null}
      </div>
    </div>
  );
}

function Reply({ text, bounds, streaming, onRetry }: { text: string; bounds?: number[]; streaming?: boolean; onRetry?: () => void }) {
  return (
    <div className="group/reply min-w-0">
      <div className={`silo-reply ${streaming ? "silo-live" : ""}`}>
        <Md text={text} bounds={streaming ? bounds : undefined} />
        {streaming && !text ? <span className="breathe inline-block h-[7px] w-[7px] rounded-full bg-ink" /> : null}
      </div>
      {!streaming ? (
        <div className="mt-1 -ml-1.5 flex translate-y-0.5 items-center gap-0.5 opacity-0 transition-[opacity,transform] duration-[180ms] ease-quiet group-focus-within/reply:translate-y-0 group-focus-within/reply:opacity-100 group-hover/reply:translate-y-0 group-hover/reply:opacity-100">
          <CopyButton text={text} title="Copy reply" />
          {onRetry ? (
            <button type="button" onClick={onRetry} title="Retry" aria-label="Retry" className={miniBtn}>
              <RotateCcw size={14} />
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

const reduceMotion = () => typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

export function Thread({
  botId,
  botName,
  botCrest,
  chatId,
  events,
  sending,
  fresh,
  onInspectArtifact,
  onSaveSkill,
  onSelectPrompt,
  onEditMessage,
  onDeleteMessage,
  onDivergeChat,
}: {
  botId: string;
  botName?: string;
  botCrest?: number;
  chatId?: string;
  events: Ev[];
  sending: boolean;
  // User message ids sent from this tab this session; they glide in.
  fresh?: ReadonlySet<string>;
  onInspectArtifact?: (a: Artifact) => void;
  onSaveSkill?: (a: Artifact) => void;
  onSelectPrompt?: (prompt: string) => void;
  onEditMessage?: (eventId: string, text: string, attachments?: Attachment[]) => void;
  onDeleteMessage?: (eventId: string) => void;
  onDivergeChat?: (eventId: string) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  const autoAt = useRef(0);
  const conversation = useRef(chatId);
  const lastKey = useRef<string | undefined>(undefined);
  const openedAt = useRef(Date.now());
  const arrived = useRef(new Map<string, boolean>());
  const [showScrollBottom, setShowScrollBottom] = useState(false);
  const blocks = foldEvents(events);
  let lastUser: Extract<Block, { type: "user" }> | undefined;
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i];
    if (b.type === "user") {
      lastUser = b;
      break;
    }
  }
  let lastAssistantKey: string | undefined;
  for (let i = blocks.length - 1; i >= 0 && blocks[i].type !== "user"; i--) {
    if (blocks[i].type === "assistant") {
      lastAssistantKey = blocks[i].key;
      break;
    }
  }

  if (conversation.current !== chatId) {
    openedAt.current = Date.now();
    arrived.current = new Map();
  }
  // History replays in a burst when a chat opens; only items that show up
  // after that rise in. Each key decides once.
  const rises = (key: string) => {
    let v = arrived.current.get(key);
    if (v === undefined) {
      v = Date.now() - openedAt.current > 900;
      arrived.current.set(key, v);
    }
    return v ? "rise" : "";
  };

  const scrollToBottom = (smooth: boolean) => {
    const el = containerRef.current;
    if (!el) return;
    pinned.current = true;
    autoAt.current = Date.now();
    el.scrollTo({ top: el.scrollHeight, behavior: smooth && !reduceMotion() ? "smooth" : "auto" });
  };

  useEffect(() => {
    const switched = chatId !== conversation.current;
    const last = blocks[blocks.length - 1];
    const sent = last?.type === "user" && last.key !== lastKey.current && !!last.id && !!fresh?.has(last.id);
    conversation.current = chatId;
    lastKey.current = last?.key;
    if (switched) {
      scrollToBottom(false);
      return;
    }
    if (sent) pinned.current = true;
    // Follow new items only when the reader was already near the bottom.
    if (pinned.current) scrollToBottom(Date.now() - openedAt.current > 900);
  }, [events, sending, chatId]);

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    // Our own smooth scroll passes through intermediate offsets; ignore those.
    if (Date.now() - autoAt.current > 700 || distanceToBottom <= 120) pinned.current = distanceToBottom <= 120;
    setShowScrollBottom(distanceToBottom > 160);
  };

  const working =
    sending &&
    !blocks.some(
      (b) => (b.type === "assistant" && b.streaming) || (b.type === "thinking" && b.streaming) || (b.type === "tool" && b.running),
    );

  const retry =
    onEditMessage && lastUser?.id && !sending
      ? () => onEditMessage(lastUser!.id!, lastUser!.text, lastUser!.attachments)
      : undefined;

  function item(b: Block): ReactNode {
    if (b.type === "user") {
      const id = b.id;
      return (
        <UserBubble
          text={b.text}
          attachments={b.attachments}
          isLast={b.key === lastUser?.key}
          busy={sending}
          fresh={!!id && !!fresh?.has(id)}
          onEdit={onEditMessage && id ? (t, a) => onEditMessage(id, t, a) : undefined}
          onDelete={onDeleteMessage && id ? () => onDeleteMessage(id) : undefined}
          onDiverge={onDivergeChat && id ? () => onDivergeChat(id) : undefined}
        />
      );
    }
    if (b.type === "thinking") {
      return <Thinking text={b.text} streaming={!!b.streaming && sending} ms={b.ms} />;
    }
    if (b.type === "receipt") return <Receipt b={b} />;
    if (b.type === "tool") {
      if (b.name === "artifact" && blocks.some((x) => x.type === "artifact" && (!x.runId || !b.runId || x.runId === b.runId))) {
        return null;
      }
      const state = rowState(b);
      const path = asStr(parseToolArgs(b.args).path);
      const failed = !b.running && b.result?.startsWith("error:");
      if (b.name === "look" || (b.name === "present" && path && isBotScratch(path))) {
        const what = b.name === "look" ? "screen" : path.split("/").filter(Boolean).pop() || path;
        return (
          <ToolRow state={state} verb={rowVerb(state, b.outcome, "Looking at", "Looked at")} app={what}>
            {!b.running && b.result && !failed ? (
              <QuietPreview botId={botId} path={b.name === "look" ? "bot/screen.jpg" : path} />
            ) : b.result ? (
              <ToolResult text={b.result} />
            ) : null}
          </ToolRow>
        );
      }
      if (b.name === "present" && !b.running && b.result && !failed && path) {
        return <PresentFile botId={botId} path={path} />;
      }
      const { app } = toolMeta(b.name);
      const body =
        b.args || b.result ? (
          <>
            <ToolInput name={b.name} args={b.args} running={b.running} />
            {b.result ? <ToolResult text={b.result} /> : null}
          </>
        ) : undefined;
      const python =
        b.name === "call" ? null : (
          <ToolRow state={state} verb={rowVerb(state, b.outcome, "Using", "Used")} app={app} action={toolAction(b.name, b.args)}>
            {body}
          </ToolRow>
        );
      if (!b.calls?.length) return python;
      // Connector calls made from Python sit above that Python row.
      return (
        <div className="space-y-1">
          {b.calls.map((c) => {
            const cs = rowState(c);
            return (
              <ToolRow key={c.key} state={cs} verb={rowVerb(cs, c.outcome, "Using", "Used")} app={c.title} action={c.name && c.name !== c.title ? c.name : undefined}>
                {c.result ? <ToolResult text={c.result} /> : undefined}
              </ToolRow>
            );
          })}
          {python}
        </div>
      );
    }
    if (b.type === "artifact") {
      const a: Artifact = {
        type: b.artifactType === "file" ? "file" : "skill",
        name: b.name,
        title: b.title,
        path: b.path,
        scope: b.scope,
        approvalId: b.approvalId,
        status: b.status,
        size: b.size,
        runId: b.runId,
      };
      return (
        <ArtifactCard
          artifact={a}
          onOpen={() => onInspectArtifact?.(a)}
          onSave={a.status === "pending" ? () => onSaveSkill?.(a) : undefined}
          onDownload={() => void downloadArtifact(botId, a)}
        />
      );
    }
    if (b.type === "assistant") {
      return <Reply text={b.text} bounds={b.bounds} streaming={b.streaming} onRetry={b.key === lastAssistantKey ? retry : undefined} />;
    }
    return (
      <div role="alert" className="flex items-start gap-2 rounded-control bg-vermilion-pale px-3 py-2.5 text-[13px] text-vermilion">
        <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
        <span className="min-w-0 break-words">{b.text}</span>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="relative min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-6"
    >
      <div className="mx-auto flex max-w-[720px] flex-col gap-[18px]">
        {blocks.length === 0 && !sending && (
          <div className="rise my-auto flex flex-col items-center justify-center px-4 py-14 text-center">
            <div className="blink mb-4">
              {botCrest !== undefined ? <Crest index={botCrest} size={56} /> : <MessageCircle size={30} className="text-ink-2" />}
            </div>
            <h2 className="text-[15px] leading-5 font-semibold tracking-[-0.01em] text-ink">{botName ? botName : "Silo Bot"}</h2>
            <p className="mt-1.5 max-w-md text-[12.5px] leading-[18px] text-ink-2">
              Ready for your prompt. Run code in the container, inspect files, or command the browser and desktop.
            </p>
            {onSelectPrompt && (
              <div className="mt-6 flex max-w-lg flex-wrap justify-center gap-2">
                {[
                  "What files are in /workspace?",
                  "Run a Python script to check system info",
                  "Open browser and check the desktop",
                  "Search the web",
                ].map((prompt) => (
                  <Btn key={prompt} kind="secondary" size="sm" onClick={() => onSelectPrompt(prompt)}>
                    {prompt}
                  </Btn>
                ))}
              </div>
            )}
          </div>
        )}
        {blocks.map((b) => {
          const node = item(b);
          if (!node) return null;
          return (
            <div key={b.key} className={`min-w-0 ${b.type === "user" ? "" : rises(b.key)}`}>
              {node}
            </div>
          );
        })}
        {working ? (
          <div className="rise flex h-9 items-center px-3 text-[13px] font-medium">
            <span className="shimmer-text">Working…</span>
          </div>
        ) : null}
      </div>

      {showScrollBottom && (
        <div className="pointer-events-none sticky bottom-0 z-20 mx-auto flex max-w-[720px] justify-end">
          <button
            type="button"
            onClick={() => scrollToBottom(true)}
            className="rise pointer-events-auto flex h-8 items-center gap-1.5 rounded-control bg-surface px-3 text-[12.5px] font-medium text-ink shadow-float transition-[background-color,transform] duration-[160ms] ease-quiet hover:bg-well active:scale-[.97] active:duration-[70ms]"
          >
            <ArrowDown size={13} />
            <span>Latest</span>
          </button>
        </div>
      )}
    </div>
  );
}

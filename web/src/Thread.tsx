import {
  ArrowDown,
  CaretRight,
  ChatCircle,
  Check,
  CircleNotch,
  Code,
  Copy,
  DownloadSimple,
  File,
  Eye,
  FrameCorners,
  GitDiff,
  Keyboard,
  MagnifyingGlass,
  Mouse,
  Notebook,
  Paperclip,
  PencilSimple,
  Plugs,
  Sparkle,
  Terminal,
  User,
} from "@phosphor-icons/react";
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
import { ArtifactCard, downloadArtifact, type Artifact } from "./Artifact";
import { Crest } from "./Crest";
import { downloadFile, FilePreview } from "./FilePreview";
import { fmtSize } from "./fs";
import { foldEvents, type Ev } from "./fold";
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

function CopyButton({ text, className = "" }: { text: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* ignore */
    }
  };
  return (
    <button
      type="button"
      onClick={copy}
      title={copied ? "Copied to clipboard!" : "Copy code"}
      className={`inline-flex items-center gap-1 rounded-[6px] px-2 py-0.5 text-[11px] font-medium text-stone transition-colors hover:bg-linen hover:text-iron active:scale-95 ${className}`}
    >
      {copied ? <Check size={12} className="text-pine" /> : <Copy size={12} />}
      <span>{copied ? "Copied" : "Copy"}</span>
    </button>
  );
}

function CodeBlock({ code, lang }: { code: string; lang?: string }) {
  const displayLang = lang || (resultLang(code) ?? "");
  return (
    <div className="group/code overflow-hidden rounded-xl border border-thread-2 bg-cloth">
      <div className="flex items-center justify-between border-b border-thread-2/60 bg-linen/30 px-3 py-1 text-[11px] font-mono text-stone">
        <span className="uppercase tracking-wider">{displayLang || "code"}</span>
        <CopyButton text={code} />
      </div>
      <pre className="whitespace-pre-wrap break-words p-3 font-mono text-[13px] leading-5">
        <code className="hljs whitespace-pre-wrap break-words" dangerouslySetInnerHTML={{ __html: highlight(code, lang) }} />
      </pre>
    </div>
  );
}

function toolMeta(name: string) {
  switch (name) {
    case "exec_python":
      return { label: "Python", Icon: Code };
    case "patch":
      return { label: "patch", Icon: GitDiff };
    case "write":
      return { label: "write", Icon: PencilSimple };
    case "read":
      return { label: "read", Icon: File };
    case "grep":
      return { label: "grep", Icon: MagnifyingGlass };
    case "terminal":
      return { label: "terminal", Icon: Terminal };
    case "soul":
      return { label: "SOUL", Icon: User };
    case "memory":
      return { label: "MEMORY", Icon: Notebook };
    case "skill":
      return { label: "skill", Icon: Notebook };
    case "artifact":
      return { label: "artifact", Icon: Notebook };
    case "present":
      return { label: "present", Icon: FrameCorners };
    case "look":
      return { label: "look", Icon: Eye };
    case "click":
      return { label: "click", Icon: Mouse };
    case "type":
      return { label: "type", Icon: Keyboard };
    case "key":
      return { label: "key", Icon: Keyboard };
      case "scroll":
        return { label: "scroll", Icon: Mouse };
      case "web_search":
        return { label: "web search", Icon: MagnifyingGlass };
      case "channel":
        return { label: "channel", Icon: Plugs };
      case "chats":
        return { label: "chats", Icon: ChatCircle };
      default:
      return { label: name, Icon: Code };
  }
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
        {path ? <div className="font-mono text-[13px]">{path}</div> : null}
        {oldText || newText ? <CodeBlock code={lines} lang="diff" /> : null}
      </div>
    );
  } else if (name === "write" && (path || content)) {
    body = (
      <div className="space-y-2">
        {path ? <div className="font-mono text-[13px]">{path}</div> : null}
        {content ? <CodeBlock code={content} lang={langFromPath(path)} /> : null}
      </div>
    );
  } else if (name === "read" && (path || offset != null || limit != null)) {
    body = (
      <div className="space-y-1 rounded-[6px] bg-cloth p-3 font-mono text-[13px]">
        {path ? <div>{path}</div> : null}
        {offset != null ? <div className="text-stone">offset {asStr(offset)}</div> : null}
        {limit != null ? <div className="text-stone">limit {asStr(limit)}</div> : null}
      </div>
    );
  } else if ((name === "soul" || name === "memory") && (content || append || oldText || newText)) {
    if (oldText || newText) {
      const lines = [
        ...(oldText ? oldText.split("\n").map((l) => `-${l}`) : []),
        ...(newText ? newText.split("\n").map((l) => `+${l}`) : []),
      ].join("\n");
      body = <CodeBlock code={lines} lang="diff" />;
    } else {
      body = <div className="whitespace-pre-wrap rounded-[6px] bg-cloth p-3 font-mono text-[13px]">{content || append}</div>;
    }
  } else if (name === "skill") {
    const skillName = asStr(a.name);
    body = (
      <div className="font-mono text-[13px]">
        {skillName || path}
      </div>
    );
  } else if (name === "artifact" && path) {
    body = <div className="font-mono text-[13px]">{path}</div>;
  } else if (name === "present" && path) {
    body = <div className="font-mono text-[13px]">{path}</div>;
  } else if (name === "click" && (x != null || y != null)) {
    body = (
      <div className="font-mono text-[13px]">
        {asStr(x)},{asStr(y)}
        {button && button !== "left" ? ` ${button}` : ""}
      </div>
    );
  } else if (name === "scroll" && (x != null || y != null || dy != null)) {
    body = (
      <div className="font-mono text-[13px]">
        {asStr(x)},{asStr(y)} dy {asStr(dy)}
      </div>
    );
  } else if (name === "type" && typeText) {
    body = <div className="whitespace-pre-wrap rounded-[6px] bg-cloth p-3 font-mono text-[13px]">{typeText}</div>;
  } else if (name === "key" && keyName) {
    body = <div className="font-mono text-[13px]">{keyName}</div>;
  } else if (name === "grep" && (pattern || path || include)) {
    body = (
      <div className="space-y-1 rounded-[6px] bg-cloth p-3 font-mono text-[13px]">
        {pattern ? <div>{pattern}</div> : null}
        {path ? <div className="text-stone">{path}</div> : null}
        {include ? <div className="text-stone">{include}</div> : null}
      </div>
    );
  } else if (name === "web_search" && asStr(a.query)) {
    body = <div className="font-mono text-[13px]">{asStr(a.query)}</div>;
  } else if (path || command) {
    body = (
      <div className="space-y-2">
        {path ? <div className="font-mono text-[13px]">{path}</div> : null}
        {command ? <CodeBlock code={command} lang="bash" /> : null}
      </div>
    );
  }

  if (body) return body;
  if (running && !Object.keys(a).length) return null;
  const dump = Object.keys(a).length ? JSON.stringify(a, null, 2) : prettyJson(args);
  if (!dump) return null;
  return <pre className="overflow-x-auto whitespace-pre-wrap rounded-[6px] bg-cloth p-3 font-mono text-[13px]">{dump}</pre>;
}

function ToolResult({ text }: { text: string }) {
  const sliced = text.length > 4000 ? `${text.slice(0, 4000)}…` : text;
  const lang = resultLang(sliced);
  const inner = lang ? (
    <pre className="whitespace-pre-wrap break-words font-mono text-[13px] leading-5 text-stone">
      <code className="hljs" dangerouslySetInnerHTML={{ __html: highlight(sliced, lang) }} />
    </pre>
  ) : (
    <pre className="whitespace-pre-wrap font-mono text-[13px] text-stone">{sliced}</pre>
  );
  const long = sliced.length > 800 || sliced.split("\n").length > 16;
  if (!long) return inner;
  return (
    <details>
      <summary className="cursor-pointer text-[12px] font-medium tracking-wide text-stone">Result</summary>
      <div className="mt-2">{inner}</div>
    </details>
  );
}

function ToolFold({ summary, children }: { summary: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <details
      className="group max-w-full rounded-[10px] border border-thread-2 bg-folio transition-colors duration-150 ease-quiet hover:border-hover"
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
    >
      <summary className="flex cursor-pointer select-none items-center gap-2 px-3 py-2 text-[12px] font-medium tracking-wide text-stone transition-colors duration-150 hover:text-iron">
        <CaretRight size={12} className="shrink-0 transition-transform duration-150 group-open:rotate-90" />
        {summary}
      </summary>
      <div className="space-y-2 border-t border-thread-2 bg-cloth/30 p-3">{children}</div>
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
          className={`silo-math-copy absolute z-10 inline-flex items-center justify-center rounded-[4px] border border-thread-2 bg-folio text-stone shadow-sm transition-[opacity,color,background-color] duration-150 hover:bg-linen hover:text-iron ${
            display
              ? "h-5 w-5 top-1 right-1"
              : "h-4 w-4 -top-2.5 -right-1.5 opacity-0 focus-visible:opacity-100 group-hover/math:opacity-100"
          }`}
        >
          {copied ? (
            <Check size={display ? 11 : 10} weight="bold" className="text-pine" />
          ) : (
            <Copy size={display ? 11 : 10} />
          )}
        </button>
      ) : null}
    </span>
  );
}

function Md({ text }: { text: string }) {
  if (!text) return null;
  return (
    <div className="silo-md">
      <Markdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex, rehypeMathCopy, ...mdHighlight]}
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

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function isBotScratch(path: string): boolean {
  const p = path.replace(/^\/+/, "").replace(/^(workspace\/)+/, "");
  return p === "bot" || p.startsWith("bot/");
}

function PresentFile({ botId, path, quiet }: { botId: string; path: string; quiet?: boolean }) {
  const [file, setFile] = useState<{ name: string; content: string; data?: Uint8Array; binary: boolean; truncated: boolean } | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    ui.readFile({ botId, path })
      .then((r) => {
        if (!dead) {
          setFile({ name: r.name, content: r.content, data: r.data, binary: r.binary, truncated: r.truncated });
        }
      })
      .catch((ex) => {
        if (!dead) setErr(fail(ex));
      });
    return () => {
      dead = true;
    };
  }, [botId, path]);
  const name = file?.name || path.split("/").filter(Boolean).pop() || path;
  if (quiet) {
    return (
      <div className="space-y-2">
        {file?.truncated ? <p className="text-[12px] text-stone">Showing the first 2 MB.</p> : null}
        {err ? <p className="text-carmine">{err}</p> : null}
        {!file && !err ? (
          <div className="flex items-center gap-2 text-stone">
            <CircleNotch size={14} className="animate-spin" />
            Opening…
          </div>
        ) : null}
        {file ? <FilePreview name={file.name} content={file.content} data={file.data} binary={file.binary} /> : null}
      </div>
    );
  }
  return (
    <div className="max-w-full space-y-2 rounded-[10px] border border-thread bg-folio p-3">
      <div className="flex items-center gap-2 text-[12px] font-medium tracking-wide text-stone">
        <FrameCorners size={14} />
        <span className="min-w-0 flex-1 truncate">{name}</span>
        {file && (
          <button
            type="button"
            className="text-stone hover:text-iron transition-colors"
            title="Download"
            onClick={() => downloadFile(file.name, file.content, file.data)}
          >
            <DownloadSimple size={14} />
          </button>
        )}
      </div>
      {file?.truncated ? <p className="text-[12px] text-stone">Showing the first 2 MB.</p> : null}
      {err ? <p className="text-carmine">{err}</p> : null}
      {!file && !err ? (
        <div className="flex items-center gap-2 text-stone">
          <CircleNotch size={14} className="animate-spin" />
          Opening…
        </div>
      ) : null}
      {file ? <FilePreview name={file.name} content={file.content} data={file.data} binary={file.binary} /> : null}
    </div>
  );
}

export function Thread({
  botId,
  botName,
  botCrest,
  events,
  sending,
  onInspectArtifact,
  onSaveSkill,
  onSelectPrompt,
}: {
  botId: string;
  botName?: string;
  botCrest?: number;
  events: Ev[];
  sending: boolean;
  onInspectArtifact?: (a: Artifact) => void;
  onSaveSkill?: (a: Artifact) => void;
  onSelectPrompt?: (prompt: string) => void;
}) {
  const end = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [showScrollBottom, setShowScrollBottom] = useState(false);
  const blocks = foldEvents(events);

  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [events, sending]);

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    setShowScrollBottom(distanceToBottom > 160);
  };

  const scrollToBottom = () => {
    end.current?.scrollIntoView({ behavior: "smooth" });
  };

  const working =
    sending &&
    !blocks.some(
      (b) => (b.type === "assistant" && b.streaming) || (b.type === "thinking" && b.streaming) || (b.type === "tool" && b.running),
    );

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="relative min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-5"
    >
      <div className="mx-auto max-w-2xl space-y-4">
        {blocks.length === 0 && !sending && (
          <div className="silo-enter my-auto flex flex-col items-center justify-center px-4 py-12 text-center">
            <div className="mb-5 flex h-16 w-16 items-center justify-center rounded-[14px] border border-thread-2 bg-folio">
              {botCrest !== undefined ? (
                <Crest index={botCrest} size={40} />
              ) : (
                <ChatCircle size={30} className="text-bindery" />
              )}
            </div>
            <h2 className="text-[18px] font-medium text-iron">
              {botName ? botName : "Silo Bot"}
            </h2>
            <p className="mt-1.5 max-w-md text-[13px] leading-6 text-stone">
              Ready for your prompt. Run code in the container, inspect files, or command the browser and desktop.
            </p>
            {onSelectPrompt && (
              <div className="mt-7 flex max-w-lg flex-wrap justify-center gap-2">
                {[
                  "What files are in /workspace?",
                  "Run a Python script to check system info",
                  "Open browser and check the desktop",
                  "Search the web",
                ].map((prompt) => (
                  <button
                    key={prompt}
                    type="button"
                    onClick={() => onSelectPrompt(prompt)}
                    className="inline-flex items-center gap-1.5 rounded-[8px] border border-thread-2 bg-folio px-3 py-1.5 text-[12px] text-iron transition-[border-color,background-color,transform] duration-200 ease-quiet hover:border-bindery hover:bg-linen active:scale-[0.97]"
                  >
                    <Sparkle size={12} className="text-bindery" />
                    <span>{prompt}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
        {blocks.map((b) => {
          if (b.type === "user") {
            return (
              <div
                key={b.key}
                className="silo-enter w-fit max-w-full rounded-[10px] border border-thread-2 border-l-[4px] border-l-bindery bg-folio px-4 py-3"
              >
                <div className="whitespace-pre-wrap break-words text-[14px] leading-relaxed text-iron">{b.text}</div>
                {b.attachments?.length ? (
                  <div className="mt-3 flex flex-wrap gap-1.5 border-t border-thread-2 pt-3">
                    {b.attachments.map((a) => (
                      <span
                        key={a.path}
                        title={a.path}
                        className="inline-flex max-w-full items-center gap-1.5 rounded-[6px] border border-thread-2 bg-cloth px-2 py-1 text-[12px] text-iron"
                      >
                        <Paperclip size={12} className="text-bindery" />
                        <span className="truncate">{a.name}</span>
                        <span className="shrink-0 font-mono text-[11px] text-stone">{fmtSize(a.size)}</span>
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          }
          if (b.type === "thinking") {
            if (b.streaming && sending) {
              return (
                <div key={b.key} className="silo-enter space-y-2 rounded-[10px] border border-thread-2 bg-cloth/40 px-3 py-2.5 text-stone">
                  <div className="flex items-center gap-2 text-[12px] font-medium tracking-wide text-bindery">
                    <CircleNotch size={14} className="animate-spin" />
                    Thinking…
                  </div>
                  {b.text ? <div className="whitespace-pre-wrap font-mono text-[13px] leading-relaxed text-stone/90">{b.text}</div> : null}
                </div>
              );
            }
            return (
              <details key={b.key} className="group max-w-full rounded-[10px] border border-thread-2 bg-cloth/30 text-stone">
                <summary className="flex cursor-pointer items-center gap-1.5 px-3 py-1.5 text-[12px] font-medium tracking-wide transition-colors duration-150 hover:text-iron">
                  <CaretRight size={12} className="shrink-0 transition-transform duration-150 group-open:rotate-90" />
                  <span>Thought</span>
                </summary>
                <div className="whitespace-pre-wrap break-words border-t border-thread-2 p-3 font-mono text-[13px] leading-relaxed text-stone/90">{b.text}</div>
              </details>
            );
          }
          if (b.type === "tool") {
            if (b.name === "artifact" && blocks.some((x) => x.type === "artifact" && (!x.runId || !b.runId || x.runId === b.runId))) {
              return null;
            }
            const path = asStr(parseToolArgs(b.args).path);
            if (b.name === "look") {
              return (
                <ToolFold
                  key={b.key}
                  summary={
                    <>
                      <Eye size={14} className="text-bindery" />
                      {b.running ? <CircleNotch size={14} className="animate-spin" /> : null}
                      {b.running ? "Looking at" : "Looked at"} screen
                    </>
                  }
                >
                  {!b.running && b.result && !b.result.startsWith("error:") ? (
                    <PresentFile botId={botId} path="bot/screen.png" quiet />
                  ) : b.result ? (
                    <ToolResult text={b.result} />
                  ) : null}
                </ToolFold>
              );
            }
            if (b.name === "present" && path && isBotScratch(path)) {
              const name = path.split("/").filter(Boolean).pop() || path;
              return (
                <ToolFold
                  key={b.key}
                  summary={
                    <>
                      <FrameCorners size={14} className="text-bindery" />
                      {b.running ? <CircleNotch size={14} className="animate-spin" /> : null}
                      {b.running ? "Looking at" : "Looked at"} {name}
                    </>
                  }
                >
                  {!b.running && b.result && !b.result.startsWith("error:") ? (
                    <PresentFile botId={botId} path={path} quiet />
                  ) : b.result ? (
                    <ToolResult text={b.result} />
                  ) : null}
                </ToolFold>
              );
            }
            if (b.name === "present" && !b.running && b.result && !b.result.startsWith("error:") && path) {
              return <PresentFile key={b.key} botId={botId} path={path} />;
            }
            const { label, Icon } = toolMeta(b.name);
            const python =
              b.name === "call" ? null : (
              <ToolFold
                key={b.calls?.length ? `${b.key}-py` : b.key}
                summary={
                  <>
                    <Icon size={14} className={b.name === "exec_python" ? "text-pine" : ""} />
                    {b.running ? <CircleNotch size={14} className="animate-spin" /> : null}
                    {b.running ? "Using" : "Used"} {label}
                  </>
                }
              >
                <ToolInput name={b.name} args={b.args} running={b.running} />
                {b.result ? <ToolResult text={b.result} /> : null}
              </ToolFold>
            );
            if (!b.calls?.length) return python;
            return (
              <div key={b.key} className="space-y-2">
                {b.calls.map((c) => (
                  <ToolFold
                    key={c.key}
                    summary={
                      <>
                        <Plugs size={14} className="text-slate" />
                        {c.running ? <CircleNotch size={14} className="animate-spin" /> : null}
                        {c.running ? "Using" : "Used"} {c.title}
                      </>
                    }
                  >
                    {c.result ? <ToolResult text={c.result} /> : null}
                  </ToolFold>
                ))}
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
                key={b.key}
                artifact={a}
                onOpen={() => onInspectArtifact?.(a)}
                onSave={a.status === "pending" ? () => onSaveSkill?.(a) : undefined}
                onDownload={() => void downloadArtifact(botId, a)}
              />
            );
          }
          if (b.type === "assistant") {
            return (
              <div key={b.key} className="silo-enter min-w-0 space-y-1">
                <Md text={b.text} />
                {b.streaming ? (
                  <span className="ml-1 inline-block h-[15px] w-[2px] translate-y-[3px] animate-pulse bg-bindery align-middle" />
                ) : null}
              </div>
            );
          }
          return (
            <div key={b.key} className="silo-enter flex items-start gap-2 rounded-[10px] border border-carmine/30 bg-carmine/10 px-3 py-2.5 text-[13px] text-carmine">
              <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-carmine" />
              <span className="min-w-0 break-words">{b.text}</span>
            </div>
          );
        })}
        {working ? (
          <div className="flex items-center gap-2 pl-1 text-[13px] text-stone">
            <CircleNotch size={14} className="animate-spin text-bindery" />
            <span>Working…</span>
          </div>
        ) : null}
        <div ref={end} />
      </div>

      {showScrollBottom && (
        <button
          type="button"
          onClick={scrollToBottom}
          className="fixed bottom-24 right-8 z-20 flex items-center gap-1.5 rounded-full border border-thread bg-folio px-3 py-1.5 text-[12px] font-medium text-iron shadow-[0_6px_20px_-8px_rgba(30,33,38,0.35)] transition-all duration-200 ease-quiet hover:bg-linen active:scale-95"
        >
          <ArrowDown size={13} weight="bold" />
          <span>Latest</span>
        </button>
      )}
    </div>
  );
}

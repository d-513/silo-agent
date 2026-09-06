import {
  CaretRight,
  ChatCircle,
  CircleNotch,
  Code,
  DownloadSimple,
  File,
  Eye,
  FrameCorners,
  GitDiff,
  Keyboard,
  MagnifyingGlass,
  Mouse,
  Notebook,
  PencilSimple,
  Plugs,
  Terminal,
  User,
} from "@phosphor-icons/react";
import hljs from "highlight.js/lib/core";
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
import { downloadFile, FilePreview } from "./FilePreview";
import { foldEvents, type Ev } from "./fold";

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
  for (const key of ["code", "command", "content", "path", "pattern", "old_text", "new_text", "include", "append"]) {
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
  return (
    <pre className="whitespace-pre-wrap break-words rounded bg-cloth p-3 font-mono text-[13px] leading-5">
      <code className="hljs whitespace-pre-wrap break-words" dangerouslySetInnerHTML={{ __html: highlight(code, lang) }} />
    </pre>
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
      <div className="space-y-1 rounded bg-cloth p-3 font-mono text-[13px]">
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
      body = <div className="whitespace-pre-wrap rounded bg-cloth p-3 font-mono text-[13px]">{content || append}</div>;
    }
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
    body = <div className="whitespace-pre-wrap rounded bg-cloth p-3 font-mono text-[13px]">{typeText}</div>;
  } else if (name === "key" && keyName) {
    body = <div className="font-mono text-[13px]">{keyName}</div>;
  } else if (name === "grep" && (pattern || path || include)) {
    body = (
      <div className="space-y-1 rounded bg-cloth p-3 font-mono text-[13px]">
        {pattern ? <div>{pattern}</div> : null}
        {path ? <div className="text-stone">{path}</div> : null}
        {include ? <div className="text-stone">{include}</div> : null}
      </div>
    );
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
  return <pre className="overflow-x-auto whitespace-pre-wrap rounded bg-cloth p-3 font-mono text-[13px]">{dump}</pre>;
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
      className="group max-w-full"
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
    >
      <summary className="flex cursor-pointer items-center gap-2 rounded bg-cloth px-3 py-2 text-[12px] font-medium tracking-wide text-stone">
        <CaretRight size={12} className="shrink-0 transition-transform group-open:rotate-90" />
        {summary}
      </summary>
      <div className="mt-2 space-y-2">{children}</div>
    </details>
  );
}

function Md({ text }: { text: string }) {
  if (!text) return null;
  return (
    <div className="silo-md">
      <Markdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={mdHighlight}
        components={{
          table: ({ children }) => (
            <div className="silo-md-table">
              <table>{children}</table>
            </div>
          ),
        }}
      >
        {text}
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
            className="text-stone hover:text-iron"
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

export function Thread({ botId, events, sending }: { botId: string; events: Ev[]; sending: boolean }) {
  const end = useRef<HTMLDivElement>(null);
  const blocks = foldEvents(events);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [events, sending]);
  const working =
    sending &&
    !blocks.some(
      (b) => (b.type === "assistant" && b.streaming) || (b.type === "thinking" && b.streaming) || (b.type === "tool" && b.running),
    );
  return (
    <div className="min-w-0 flex-1 space-y-4 overflow-x-hidden overflow-y-auto p-4">
      {blocks.length === 0 && !sending && (
        <p className="flex items-center gap-2 text-stone">
          <ChatCircle size={16} />
          Ask this Bot…
        </p>
      )}
      {blocks.map((b) => {
        if (b.type === "user") {
          return (
            <div key={b.key} className="max-w-full break-words whitespace-pre-wrap rounded-[10px] border-l-4 border-bindery bg-folio px-3 py-2">
              {b.text}
            </div>
          );
        }
        if (b.type === "thinking") {
          if (b.streaming && sending) {
            return (
              <div key={b.key} className="space-y-2 text-stone">
                <div className="flex items-center gap-2 text-[12px] font-medium tracking-wide">
                  <CircleNotch size={14} className="animate-spin" />
                  Thinking
                </div>
                {b.text ? <div className="whitespace-pre-wrap font-mono text-[13px]">{b.text}</div> : null}
              </div>
            );
          }
          return (
            <details key={b.key} className="max-w-full text-stone">
              <summary className="cursor-pointer text-[12px] font-medium tracking-wide">Thought</summary>
              <div className="mt-2 break-words whitespace-pre-wrap font-mono text-[13px]">{b.text}</div>
            </details>
          );
        }
        if (b.type === "tool") {
          const path = asStr(parseToolArgs(b.args).path);
          if (b.name === "look") {
            return (
              <ToolFold
                key={b.key}
                summary={
                  <>
                    <Eye size={14} />
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
                    <FrameCorners size={14} />
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
                  <Icon size={14} />
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
                      <Plugs size={14} />
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
        if (b.type === "assistant") {
          return (
            <div key={b.key} className="min-w-0">
              <Md text={b.text} />
              {b.streaming ? <span className="ml-0.5 inline-block h-4 w-px translate-y-0.5 bg-iron align-middle" /> : null}
            </div>
          );
        }
        return (
          <div key={b.key} className="text-carmine">
            {b.text}
          </div>
        );
      })}
      {working ? (
        <div className="flex items-center gap-2 text-stone">
          <CircleNotch size={14} className="animate-spin" />
          Working…
        </div>
      ) : null}
      <div ref={end} />
    </div>
  );
}

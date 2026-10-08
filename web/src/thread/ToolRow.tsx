import type { JSX, ReactNode } from "react";
import { CopyButton } from "../Feedback";
import { Highlighted } from "../Highlighted";
import { codeLang } from "../fileKind";
import { resultLang } from "../highlight";
import { FoldRow, type RowState } from "./FoldRow";
import { ToolIcon, type ConnectorMarks } from "./toolIcons";
import { asStr, parseToolArgs, prettyJson } from "./toolInfo";

function CodeBlock({ code, lang }: { code: string; lang?: string }) {
  const displayLang = lang || (resultLang(code) ?? "");
  return (
    <div className="overflow-hidden rounded-sm bg-well">
      <div className="flex h-8 items-center justify-between pr-1 pl-3 shadow-[inset_0_-1px_0_var(--color-line)]">
        <span className="text-label-caps leading-4 text-ink-3 uppercase">{displayLang || "code"}</span>
        <CopyButton text={code} title="Copy code" label size={12} />
      </div>
      <Highlighted code={code} lang={lang} preClass="whitespace-pre-wrap break-words p-3 font-mono text-[12.5px] leading-5" codeClass="hljs whitespace-pre-wrap break-words" />
    </div>
  );
}

export function ToolRow({
  state,
  icon,
  marks,
  verb,
  app,
  action,
  live,
  children,
}: {
  state: RowState;
  icon: string;
  marks?: ConnectorMarks;
  verb: string;
  app: string;
  action?: string;
  live?: boolean;
  children?: ReactNode;
}) {
  return (
    <FoldRow
      live={live}
      lead={<ToolIcon name={icon} marks={marks} />}
      title={
        state === "running" ? (
          // One element, so the shimmer's clipped gradient reaches every word.
          <span className="shimmer-text shrink-0 font-medium">
            {verb} {app}
          </span>
        ) : (
          <span className="shrink-0 font-medium">
            <span className="text-ink-3">{verb}</span>{" "}
            <span className={state === "stopped" ? "text-ink-3" : "text-ink-2 transition-colors duration-[160ms] group-hover/row:text-ink"}>{app}</span>
          </span>
        )
      }
      tail={
        <>
          {state === "waiting" ? (
            <span className="flex shrink-0 items-center gap-1.5 text-[12.5px] text-vermilion">
              <span className="breathe h-[7px] w-[7px] rounded-full bg-vermilion" aria-hidden />
              Waiting for you
            </span>
          ) : null}
          {action ? <span className="min-w-0 truncate font-mono text-[12.5px] text-ink-3">{action}</span> : null}
        </>
      }
    >
      {children}
    </FoldRow>
  );
}

export function ToolInput({ name, args, running }: { name: string; args: string; running?: boolean }) {
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
        {content ? <CodeBlock code={content} lang={codeLang(path)} /> : null}
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

// Live output shows its tail (that is what is changing); finished output its head.
export function ToolResult({ text, live }: { text: string; live?: boolean }) {
  const sliced = text.length > 4000 ? (live ? `…${text.slice(-4000)}` : `${text.slice(0, 4000)}…`) : text;
  const lang = resultLang(sliced);
  const inner = lang ? (
    <Highlighted code={sliced} lang={lang} preClass="whitespace-pre-wrap break-words font-mono text-[12.5px] leading-5 text-ink-2" />
  ) : (
    <pre className="whitespace-pre-wrap font-mono text-[12.5px] leading-5 text-ink-2">{sliced}</pre>
  );
  const long = sliced.length > 800 || sliced.split("\n").length > 16;
  if (!long || live) return inner;
  return (
    <details>
      <summary className="cursor-pointer text-[12px] font-medium text-ink-3 hover:text-ink">Result</summary>
      <div className="mt-2">{inner}</div>
    </details>
  );
}

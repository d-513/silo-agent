import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import css from "highlight.js/lib/languages/css";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";
import { useEffect, useMemo, useState } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { codeLang, kindOf, mimeOf } from "./fileKind";

export function downloadFile(name: string, content: string, data?: Uint8Array) {
  const blob = new Blob([data && data.length ? data.slice() : content], { type: mimeOf(name) });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}

const langs: Record<string, typeof python> = {
  python,
  bash,
  json,
  typescript,
  javascript,
  go,
  xml,
  html: xml,
  markdown,
  yaml,
  css,
};
for (const [name, fn] of Object.entries(langs)) {
  hljs.registerLanguage(name, fn);
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

function useBlob(data: Uint8Array | undefined, mime: string) {
  const url = useMemo(() => {
    if (!data?.length) return "";
    return URL.createObjectURL(new Blob([data.slice()], { type: mime }));
  }, [data, mime]);
  useEffect(() => () => {
    if (url) URL.revokeObjectURL(url);
  }, [url]);
  return url;
}

function textOf(content: string, data?: Uint8Array) {
  if (content) return content;
  if (!data?.length) return "";
  return new TextDecoder().decode(data);
}

function CsvTable({ text }: { text: string }) {
  const rows = text.split(/\r?\n/).filter((l) => l.length).slice(0, 200);
  const delim = text.includes("\t") && !text.includes(",") ? "\t" : ",";
  return (
    <div className="max-w-full overflow-x-auto">
      <table className="w-max min-w-full border-collapse text-left text-[13px]">
        <tbody>
          {rows.map((line, i) => (
            <tr key={i} className="border-b border-thread-2">
              {line.split(delim).map((cell, j) => (
                <td key={j} className={`px-2 py-1 break-words ${i === 0 ? "font-medium" : ""}`}>
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function DocxView({ data }: { data: Uint8Array }) {
  const [html, setHtml] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    import("mammoth")
      .then((m) =>
        m.convertToHtml({
          arrayBuffer: data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength) as ArrayBuffer,
        }),
      )
      .then((r) => {
        if (!dead) setHtml(r.value);
      })
      .catch((e) => {
        if (!dead) setErr(e instanceof Error ? e.message : "could not read document");
      });
    return () => {
      dead = true;
    };
  }, [data]);
  if (err) return <p className="text-carmine">{err}</p>;
  if (!html) return <p className="text-stone">Opening…</p>;
  return <div className="silo-md" dangerouslySetInnerHTML={{ __html: html }} />;
}

export function FilePreview({
  name,
  content,
  data,
  binary,
}: {
  name: string;
  content: string;
  data?: Uint8Array;
  binary: boolean;
}) {
  const kind = kindOf(name);
  const mime = mimeOf(name);
  const url = useBlob(data, mime);
  const text = textOf(content, binary ? undefined : data);

  if (kind === "image" && url) {
    return <img src={url} alt={name} className="max-h-[32rem] max-w-full object-contain" />;
  }
  if (kind === "pdf" && url) {
    return <iframe title={name} src={url} className="h-full min-h-[24rem] w-full border-0 bg-folio" />;
  }
  if (kind === "video" && url) {
    return <video src={url} controls className="max-h-full max-w-full" />;
  }
  if (kind === "audio" && url) {
    return <audio src={url} controls className="w-full" />;
  }
  if (kind === "docx" && data?.length) {
    return <DocxView data={data} />;
  }
  if (kind === "markdown") {
    return (
      <div className="silo-md">
        <Markdown
          remarkPlugins={[remarkGfm]}
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
  if (kind === "csv") {
    return <CsvTable text={text} />;
  }
  if (kind === "json") {
    let pretty = text;
    try {
      pretty = JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      /* raw */
    }
    return (
      <pre className="whitespace-pre-wrap break-words font-mono text-[13px] leading-5">
        <code className="hljs" dangerouslySetInnerHTML={{ __html: highlight(pretty, "json") }} />
      </pre>
    );
  }
  if (kind === "code" || (kind === "text" && !binary)) {
    const lang = codeLang(name);
    return (
      <pre className="whitespace-pre-wrap break-words font-mono text-[13px] leading-5">
        <code className="hljs" dangerouslySetInnerHTML={{ __html: highlight(text || " ", lang) }} />
      </pre>
    );
  }
  if (!binary && text) {
    return <pre className="whitespace-pre-wrap font-mono text-[13px]">{text}</pre>;
  }
  return <p className="text-stone">No preview for this type. Download it instead.</p>;
}

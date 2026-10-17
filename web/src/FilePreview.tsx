import { lazy, Suspense, useEffect, useMemo, useState, type ReactNode } from "react";
import { filterRows, parseCsv, sortRows } from "./csv";
import { codeLang, extOf, kindOf, mimeOf } from "./fileKind";
import { Md } from "./Md";
import { Croppable } from "./quote/Croppable";
import { Quotable } from "./quote/Quotable";
import type { QuoteTarget } from "./quote/QuoteContext";
import { Highlighted } from "./Highlighted";
import { safeHtml } from "./safeHtml";
import { t } from "./i18n.ts";

export function downloadFile(name: string, content: string, data?: Uint8Array) {
  const blob = new Blob([data && data.length ? data.slice() : content], { type: mimeOf(name) });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
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
  const all = useMemo(() => parseCsv(text), [text]);
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<{ col: number; dir: 1 | -1 } | null>(null);
  if (all.length === 0) return <p className="text-ink-2">{t("This table is empty.")}</p>;
  const [head, ...body] = all;
  const shown = filterRows(sort ? sortRows(body, sort.col, sort.dir) : body, q);
  const width = Math.max(head.length, ...body.slice(0, 50).map((r) => r.length));
  return (
    <div className="space-y-2">
      {body.length > 8 ? (
        <div className="flex items-center gap-2">
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("Filter rows")}
            className="h-8 w-56 rounded-control bg-well px-2.5 text-[13px] text-ink outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--color-cobalt)]"
          />
          <span className="font-mono text-[11.5px] text-ink-3">
            {shown.length === body.length ? t("{{count}} rows", { count: body.length }) : t("{{n}} of {{total}}", { n: shown.length, total: body.length })}
          </span>
        </div>
      ) : null}
      <div className="max-w-full overflow-x-auto">
        <table className="w-max min-w-full border-collapse text-left text-[13px]">
          <thead>
            <tr className="border-b border-line-strong">
              {Array.from({ length: width }, (_, j) => {
                const on = sort?.col === j;
                return (
                  <th key={j} className="px-2 py-1 font-medium">
                    <button
                      type="button"
                      className="flex items-center gap-1 text-left hover:text-ink"
                      onClick={() => setSort(on ? (sort!.dir === 1 ? { col: j, dir: -1 } : null) : { col: j, dir: 1 })}
                    >
                      {head[j] ?? ""}
                      {on ? <span className="text-ink-3">{sort!.dir === 1 ? "\u2191" : "\u2193"}</span> : null}
                    </button>
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {shown.slice(0, 500).map((r, i) => (
              <tr key={i} className="border-b border-line">
                {Array.from({ length: width }, (_, j) => (
                  <td key={j} className="px-2 py-1 break-words">
                    {r[j] ?? ""}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {shown.length > 500 ? <p className="text-[12.5px] text-ink-3">{t("Showing the first {{n}} rows.", { n: 500 })}</p> : null}
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
        // The converter does not clean what the document's author put in it.
        if (!dead) setHtml(safeHtml(r.value));
      })
      .catch((e) => {
        if (!dead) setErr(e instanceof Error ? e.message : t("could not read document"));
      });
    return () => {
      dead = true;
    };
  }, [data]);
  if (err) return <p className="text-vermilion">{err}</p>;
  if (!html) return <p className="text-ink-2">{t("Opening\u2026")}</p>;
  return <div className="silo-md" dangerouslySetInnerHTML={{ __html: html }} />;
}

const Sheet = lazy(() => import("./previewers/Sheet"));
const Slides = lazy(() => import("./previewers/Slides"));
const Chart = lazy(() => import("./previewers/Chart"));
const Mermaid = lazy(() => import("./previewers/Mermaid"));
const Notebook = lazy(() => import("./previewers/Notebook"));
const GeoMap = lazy(() => import("./previewers/GeoMap"));
const Model3D = lazy(() => import("./previewers/Model3D"));

function Lazy({ children }: { children: ReactNode }) {
  return <Suspense fallback={<div className="skeleton h-40 rounded-sm" />}>{children}</Suspense>;
}

// FilePreview shows a file as what it is. Given a quote target, its text can be
// selected and quoted into the message being written, and an image cropped.
export function FilePreview({
  name,
  content,
  data,
  binary,
  full,
  quote,
}: {
  name: string;
  content: string;
  data?: Uint8Array;
  binary: boolean;
  full?: boolean;
  quote?: QuoteTarget;
}) {
  const kind = kindOf(name);
  const mime = mimeOf(name);
  const url = useBlob(data, mime);
  const text = textOf(content, binary ? undefined : data);
  const quotable = (node: ReactNode) => (quote ? <Quotable target={{ ...quote, text }}>{node}</Quotable> : node);

  if (kind === "image" && url) {
    const cls = full ? "max-h-[calc(100vh-6rem)] max-w-full object-contain" : "max-h-[32rem] max-w-full object-contain";
    // An SVG is shown, never cropped: it would have to be drawn into a canvas.
    if (quote && extOf(name) !== "svg") return <Croppable src={url} alt={name} target={quote} className={cls} />;
    return <img src={url} alt={name} className={cls} />;
  }
  if (kind === "pdf" && url) {
    return (
      <iframe
        title={name}
        src={url}
        className={full ? "block h-full min-h-0 w-full flex-1 border-0 bg-surface" : "block h-[70vh] min-h-[26rem] w-full border-0 bg-surface"}
      />
    );
  }
  if (kind === "video" && url) {
    return <video src={url} controls className="max-h-full max-w-full" />;
  }
  if (kind === "audio" && url) {
    return <audio src={url} controls className="w-full" />;
  }
  if (kind === "docx" && data?.length) {
    return quotable(<DocxView data={data} />);
  }
  if (kind === "sheet" && data?.length) {
    return quotable(<Lazy><Sheet data={data} /></Lazy>);
  }
  if (kind === "slides" && data?.length) {
    return quotable(<Lazy><Slides data={data} /></Lazy>);
  }
  if (kind === "model" && data?.length) {
    return <Lazy><Model3D data={data} /></Lazy>;
  }
  if (kind === "chart" && text) {
    return <Lazy><Chart text={text} /></Lazy>;
  }
  if (kind === "mermaid" && text) {
    return <Lazy><Mermaid source={text} /></Lazy>;
  }
  if (kind === "notebook" && text) {
    return quotable(<Lazy><Notebook text={text} /></Lazy>);
  }
  if (kind === "geojson" && text) {
    return <Lazy><GeoMap text={text} /></Lazy>;
  }
  if (kind === "markdown") {
    return quotable(<Md text={text} />);
  }
  if (kind === "csv") {
    return quotable(<CsvTable text={text} />);
  }
  if (kind === "json") {
    let pretty = text;
    try {
      pretty = JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      /* raw */
    }
    return quotable(<Highlighted code={pretty} lang="json" preClass="whitespace-pre-wrap break-words font-mono text-[13px] leading-5" />);
  }
  if (kind === "code" || kind === "html" || (kind === "text" && !binary)) {
    const lang = kind === "html" ? "xml" : codeLang(name);
    return quotable(<Highlighted code={text || " "} lang={lang} preClass="whitespace-pre-wrap break-words font-mono text-[13px] leading-5" />);
  }
  if (!binary && text) {
    return quotable(<pre className="whitespace-pre-wrap font-mono text-[13px]">{text}</pre>);
  }
  return <p className="text-ink-2">{t("No preview for this type. Download it instead.")}</p>;
}

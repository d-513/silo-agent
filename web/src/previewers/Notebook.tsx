import { Highlighted } from "../Highlighted";
import { Md } from "../Md";
import { safeHtml, safeImage } from "../safeHtml";
import { t } from "../i18n.ts";

type Output = {
  output_type: string;
  text?: string | string[];
  name?: string;
  data?: Record<string, string | string[]>;
  ename?: string;
  evalue?: string;
  traceback?: string[];
};
type Cell = { cell_type: string; source: string | string[]; outputs?: Output[]; execution_count?: number | null };

const joined = (v: string | string[] | undefined) => (Array.isArray(v) ? v.join("") : (v ?? ""));
// Tracebacks carry terminal colours.
const plain = (s: string) => s.replace(/\u001b\[[0-9;]*m/g, "");

// A Jupyter notebook as it was saved: markdown, code, and the outputs each
// cell left. HTML output goes through the same cleaning as a document's.
export default function Notebook({ text }: { text: string }) {
  let cells: Cell[] = [];
  let lang = "python";
  try {
    const nb = JSON.parse(text) as { cells?: Cell[]; metadata?: { language_info?: { name?: string } } };
    cells = nb.cells ?? [];
    lang = nb.metadata?.language_info?.name || lang;
  } catch {
    return <p className="text-ink-2">{t("This notebook could not be read.")}</p>;
  }
  return (
    <div className="space-y-4">
      {cells.map((c, i) => (
        <div key={i} className="min-w-0">
          {c.cell_type === "markdown" ? (
            <Md text={joined(c.source)} />
          ) : c.cell_type === "code" ? (
            <div className="space-y-1.5">
              <div className="flex gap-2">
                <span className="w-9 shrink-0 pt-2 text-right font-mono text-[11px] text-ink-3">[{c.execution_count ?? " "}]</span>
                <div className="min-w-0 flex-1 rounded-sm bg-well p-2.5">
                  <Highlighted code={joined(c.source) || " "} lang={lang} preClass="whitespace-pre-wrap break-words font-mono text-[12.5px] leading-5" />
                </div>
              </div>
              {(c.outputs ?? []).map((o, j) => (
                <div key={j} className="ml-11 min-w-0">
                  <CellOutput o={o} />
                </div>
              ))}
            </div>
          ) : (
            <pre className="whitespace-pre-wrap font-mono text-[12.5px] text-ink-2">{joined(c.source)}</pre>
          )}
        </div>
      ))}
    </div>
  );
}

function CellOutput({ o }: { o: Output }) {
  const pre = "max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[12.5px] leading-5";
  if (o.output_type === "stream") {
    return <pre className={`${pre} ${o.name === "stderr" ? "text-vermilion" : "text-ink-2"}`}>{joined(o.text)}</pre>;
  }
  if (o.output_type === "error") {
    return <pre className={`${pre} text-vermilion`}>{plain([`${o.ename}: ${o.evalue}`, ...(o.traceback ?? [])].join("\n"))}</pre>;
  }
  const d = o.data ?? {};
  for (const type of ["image/png", "image/jpeg", "image/gif"]) {
    if (d[type]) {
      const src = safeImage(`data:${type};base64,${joined(d[type]).replace(/\s+/g, "")}`);
      if (src) return <img src={src} alt="" className="max-h-[28rem] max-w-full rounded-sm bg-surface" />;
    }
  }
  if (d["text/html"]) return <div className="silo-md max-h-96 overflow-auto" dangerouslySetInnerHTML={{ __html: safeHtml(joined(d["text/html"])) }} />;
  if (d["text/markdown"]) return <Md text={joined(d["text/markdown"])} />;
  if (d["text/plain"]) return <pre className={`${pre} text-ink-2`}>{joined(d["text/plain"])}</pre>;
  return null;
}

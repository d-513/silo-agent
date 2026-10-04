import { Check, Copy } from "lucide-react";
import remarkMath from "remark-math";
import { useState, type ReactNode } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useMdModules } from "./mdPlugins";
import { classNames, isDisplayMath, mathTex, normalizeLatex, rehypeMathCopy, type HastNode } from "./latex";

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

// Module-level on purpose: a fresh object each render gives React new component
// types, which remounts every span in the reply (and replays its fade) per token.
const mdComponents: NonNullable<Parameters<typeof Markdown>[0]["components"]> = {
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
};

type Rehype = NonNullable<Parameters<typeof Markdown>[0]["rehypePlugins"]>;

export function Md({ text, bounds }: { text: string; bounds?: number[] }) {
  const mods = useMdModules(text);
  if (!text) return null;
  // KaTeX first, then the math tagging that reads its output, then highlighting.
  const base = [...(mods.math ? [mods.math.rehypeKatex] : []), rehypeMathCopy, ...(mods.code ? mods.code.rehypeCode : [])] as Rehype;
  const plugins = bounds && bounds.length > 1 ? [...base, rehypeChunks(bounds)] : base;
  return (
    <div className="silo-md">
      <Markdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={plugins}
        components={mdComponents}
      >
        {normalizeLatex(text)}
      </Markdown>
    </div>
  );
}


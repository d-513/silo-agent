// Models routinely emit LaTeX with \(...\) and \[...\] delimiters. remark-math
// only understands $...$ / $$...$$, and remark-parse silently eats \( as a
// markdown escape before any plugin can see it. So rewrite the delimiters on
// the raw string first, leaving fenced and inline code untouched.
const CODE = /(```[\s\S]*?```|~~~[\s\S]*?~~~|`[^`\n]*`)/g;

function convert(s: string): string {
  s = s.replace(/\\\[([\s\S]+?)\\\]/g, (_m, tex: string) => `\n\n$$\n${tex.trim()}\n$$\n\n`);
  s = s.replace(/\\\(([\s\S]+?)\\\)/g, (_m, tex: string) => `$${tex.trim()}$`);
  return s;
}

export function normalizeLatex(src: string): string {
  if (!src.includes("\\(") && !src.includes("\\[")) return src;
  const parts: string[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  CODE.lastIndex = 0;
  while ((m = CODE.exec(src))) {
    parts.push(convert(src.slice(last, m.index)));
    parts.push(m[0]);
    last = m.index + m[0].length;
  }
  parts.push(convert(src.slice(last)));
  return parts.join("");
}

// --- copy button support ---------------------------------------------------

// Minimal HAST shape; enough for the rehype pass and the renderer without
// pulling in @types/hast.
export type HastNode = {
  type?: string;
  tagName?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
  value?: string;
};

export function classNames(props?: Record<string, unknown>): string[] {
  const c = props?.className;
  if (Array.isArray(c)) return c.map(String);
  if (typeof c === "string") return c.split(/\s+/).filter(Boolean);
  return [];
}

// rehype-katex keeps the original TeX in the MathML annotation.
function firstTex(node: HastNode): string {
  if (node.type === "element" && node.tagName === "annotation") {
    for (const c of node.children ?? []) {
      if (c.type === "text" && typeof c.value === "string") return c.value;
    }
  }
  for (const c of node.children ?? []) {
    const v = firstTex(c);
    if (v) return v;
  }
  return "";
}

// markMath tags the outermost KaTeX element so the renderer can wrap it once.
// Descending stops there, so the inner .katex of a display block is not tagged
// twice.
function markMath(node: HastNode): void {
  const props = (node.properties ??= {});
  const cls = classNames(props);
  if (!cls.includes("silo-math")) cls.push("silo-math");
  props.className = cls;
  props.tex = firstTex(node);
}

function walkHast(node: HastNode): void {
  for (const child of node.children ?? []) {
    if (child.type === "element" && child.tagName === "span") {
      const cls = classNames(child.properties);
      if (cls.includes("katex-display") || cls.includes("katex")) {
        markMath(child);
        continue;
      }
    }
    walkHast(child);
  }
}

// rehypeMathCopy marks KaTeX output. It must run after rehype-katex.
export function rehypeMathCopy() {
  return (tree: HastNode) => walkHast(tree);
}

export function mathTex(node?: HastNode): string {
  const v = node?.properties?.tex;
  return typeof v === "string" ? v : "";
}

export function isDisplayMath(node?: HastNode): boolean {
  return classNames(node?.properties).includes("katex-display");
}

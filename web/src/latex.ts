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

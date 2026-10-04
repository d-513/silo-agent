import { useHighlight } from "./highlight";

// A <pre> of code in lang, highlighted once highlight.js has loaded.
export function Highlighted({ code, lang, preClass, codeClass = "hljs" }: { code: string; lang?: string; preClass: string; codeClass?: string }) {
  const html = useHighlight(code, lang);
  return (
    <pre className={preClass}>
      <code className={codeClass} dangerouslySetInnerHTML={{ __html: html }} />
    </pre>
  );
}

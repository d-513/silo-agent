import { useEffect, useId, useState } from "react";
import { t } from "../i18n.ts";

type MermaidAPI = typeof import("mermaid").default;
let loading: Promise<MermaidAPI> | null = null;
const load = () => (loading ??= import("mermaid").then((m) => m.default));

// A Mermaid diagram from its source. Strict mode: no click handlers, no HTML
// labels, nothing in it acts on the page.
export default function Mermaid({ source }: { source: string }) {
  const id = useId().replace(/[^a-zA-Z0-9]/g, "");
  const [svg, setSvg] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    setErr("");
    load()
      .then(async (mermaid) => {
        const dark = document.documentElement.dataset.theme === "dark";
        mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: dark ? "dark" : "neutral", fontFamily: "Geist Variable, ui-sans-serif, system-ui" });
        const out = await mermaid.render(`mmd${id}`, source);
        if (!dead) setSvg(out.svg);
      })
      .catch((e: unknown) => {
        if (!dead) setErr(e instanceof Error ? e.message : t("This diagram could not be drawn."));
      });
    return () => {
      dead = true;
    };
  }, [source, id]);
  if (err) {
    return (
      <div className="space-y-1.5">
        <p className="text-[12.5px] text-vermilion">{err}</p>
        <pre className="overflow-auto rounded-sm bg-well p-3 font-mono text-[12.5px] leading-5">{source}</pre>
      </div>
    );
  }
  if (!svg) return <div className="skeleton h-32 rounded-sm" />;
  return <div className="silo-mermaid max-w-full overflow-auto [&_svg]:max-w-full" dangerouslySetInnerHTML={{ __html: svg }} />;
}

import { useEffect, useRef, useState } from "react";
import { t } from "../i18n.ts";

// A Vega or Vega-Lite spec drawn as the chart it describes. It may use only
// the data written in it: a spec that names a URL would have the console fetch
// it, signed in, so every load is refused.
export default function Chart({ text }: { text: string }) {
  const box = useRef<HTMLDivElement>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    let finalize: (() => void) | undefined;
    setErr("");
    (async () => {
      const spec = JSON.parse(text);
      const [{ default: embed }, vega] = await Promise.all([import("vega-embed"), import("vega")]);
      const loader = vega.loader();
      const refuse = async () => {
        throw new Error(t("A chart in a file can use only the data written in it."));
      };
      loader.load = refuse;
      loader.sanitize = refuse;
      if (dead || !box.current) return;
      const dark = document.documentElement.dataset.theme === "dark";
      const res = await embed(box.current, spec, { actions: false, loader, renderer: "svg", theme: dark ? "dark" : undefined });
      finalize = () => res.finalize();
      if (dead) finalize();
    })().catch((e: unknown) => {
      if (!dead) setErr(e instanceof Error ? e.message : String(e));
    });
    return () => {
      dead = true;
      finalize?.();
    };
  }, [text]);
  return (
    <div className="space-y-2">
      {err ? <p className="text-[13px] text-vermilion">{err}</p> : null}
      <div ref={box} className="max-w-full overflow-auto" />
    </div>
  );
}

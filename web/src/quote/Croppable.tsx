import { Check, Crop, LoaderCircle, X } from "lucide-react";
import { useRef, useState, type PointerEvent as RPointerEvent } from "react";
import { fail } from "../errors";
import { t } from "../i18n.ts";
import { regionLocator } from "./model";
import { useQuoteSink, type QuoteTarget } from "./QuoteContext";

type Rect = { x: number; y: number; w: number; h: number };

// Croppable is an image the human can quote a region of: Crop, drag a box,
// Quote region. The crop is uploaded to /workspace/tmp, and the quote carries
// its path and where it sat in the picture.
export function Croppable({ src, alt, target, className }: { src: string; alt: string; target: QuoteTarget; className: string }) {
  const sink = useQuoteSink();
  const img = useRef<HTMLImageElement>(null);
  const [mode, setMode] = useState<"off" | "drag" | "busy">("off");
  const [rect, setRect] = useState<Rect | null>(null);
  const [err, setErr] = useState("");
  const start = useRef<{ x: number; y: number } | null>(null);

  if (!sink) return <img src={src} alt={alt} className={className} />;

  const at = (e: RPointerEvent) => {
    const r = img.current!.getBoundingClientRect();
    return { x: Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)), y: Math.min(1, Math.max(0, (e.clientY - r.top) / r.height)) };
  };
  const down = (e: RPointerEvent<HTMLDivElement>) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    start.current = at(e);
    setRect({ ...start.current, w: 0, h: 0 });
  };
  const move = (e: RPointerEvent) => {
    if (!start.current) return;
    const p = at(e);
    const s = start.current;
    setRect({ x: Math.min(s.x, p.x), y: Math.min(s.y, p.y), w: Math.abs(p.x - s.x), h: Math.abs(p.y - s.y) });
  };
  const up = () => {
    start.current = null;
  };
  const cancel = () => {
    setMode("off");
    setRect(null);
    setErr("");
  };

  async function take() {
    const el = img.current;
    if (!el || !rect || rect.w < 0.01 || rect.h < 0.01) return;
    setMode("busy");
    setErr("");
    try {
      const sx = rect.x * el.naturalWidth;
      const sy = rect.y * el.naturalHeight;
      const sw = Math.max(1, rect.w * el.naturalWidth);
      const sh = Math.max(1, rect.h * el.naturalHeight);
      const canvas = document.createElement("canvas");
      canvas.width = Math.round(sw);
      canvas.height = Math.round(sh);
      canvas.getContext("2d")!.drawImage(el, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height);
      const blob = await new Promise<Blob>((res, rej) => canvas.toBlob((b) => (b ? res(b) : rej(new Error("could not crop"))), "image/png"));
      const thumbCanvas = document.createElement("canvas");
      const scale = Math.min(1, 96 / Math.max(canvas.width, canvas.height));
      thumbCanvas.width = Math.max(1, Math.round(canvas.width * scale));
      thumbCanvas.height = Math.max(1, Math.round(canvas.height * scale));
      thumbCanvas.getContext("2d")!.drawImage(canvas, 0, 0, thumbCanvas.width, thumbCanvas.height);
      const path = await sink!.upload(new Uint8Array(await blob.arrayBuffer()), `crop-${Date.now()}.png`);
      sink!.add({ source: target.source, ref: target.ref, label: target.label, text: "", locator: regionLocator(rect), image: path, thumb: thumbCanvas.toDataURL("image/png") });
      cancel();
    } catch (ex) {
      setMode("drag");
      setErr(fail(ex));
    }
  }

  const tool = "flex h-7 items-center gap-1.5 rounded-control bg-surface px-2.5 text-[12px] font-medium text-ink shadow-float transition-[background-color,transform] duration-[160ms] ease-quiet hover:bg-well active:scale-[.97] active:duration-[70ms]";
  return (
    <div className="group/crop relative inline-block max-w-full">
      <img ref={img} src={src} alt={alt} className={className} draggable={false} />
      {mode === "off" ? (
        sink.canUpload ? (
          <button type="button" onClick={() => setMode("drag")} className={`${tool} absolute top-2 right-2 opacity-0 group-hover/crop:opacity-100 focus-visible:opacity-100`}>
            <Crop size={13} />
            {t("Quote a region")}
          </button>
        ) : null
      ) : (
        <div className="absolute inset-0 cursor-crosshair touch-none" onPointerDown={down} onPointerMove={move} onPointerUp={up}>
          {rect ? (
            <div
              className="pointer-events-none absolute ring-2 ring-cobalt"
              style={{ left: `${rect.x * 100}%`, top: `${rect.y * 100}%`, width: `${rect.w * 100}%`, height: `${rect.h * 100}%`, boxShadow: "0 0 0 9999px var(--shade-deep)" }}
            />
          ) : null}
          <div className="absolute top-2 right-2 flex gap-1.5" onPointerDown={(e) => e.stopPropagation()}>
            <button type="button" onClick={cancel} className={tool}>
              <X size={13} />
              {t("Cancel")}
            </button>
            <button type="button" onClick={() => void take()} disabled={!rect || rect.w < 0.01 || mode === "busy"} className={`${tool} disabled:opacity-50`}>
              {mode === "busy" ? <LoaderCircle size={13} className="spin" /> : <Check size={13} />}
              {t("Quote region")}
            </button>
          </div>
          {err ? <p className="absolute bottom-2 left-2 rounded-sm bg-surface px-2 py-1 text-[12px] text-vermilion shadow-card">{err}</p> : null}
        </div>
      )}
    </div>
  );
}

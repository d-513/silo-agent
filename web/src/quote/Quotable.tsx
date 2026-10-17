import { Check, TextQuote } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n.ts";
import { clip, lineLocator } from "./model";
import { useQuoteSink, type QuoteTarget } from "./QuoteContext";

type Spot = { x: number; top: number; bottom: number; text: string };

// Quotable lets the human select text inside it and quote it into the message
// they are writing: a small Quote button rises over the selection, and turns
// into a check once it is taken. Without a quote sink it is a plain wrapper.
export function Quotable({ target, children, className = "" }: { target: QuoteTarget; children: ReactNode; className?: string }) {
  const sink = useQuoteSink();
  const ref = useRef<HTMLDivElement>(null);
  const [spot, setSpot] = useState<Spot | null>(null);
  const [taken, setTaken] = useState(false);
  const targetRef = useRef(target);
  targetRef.current = target;

  useEffect(() => {
    if (!sink) return;
    let timer = 0;
    const read = () => {
      const sel = document.getSelection();
      const box = ref.current;
      if (!sel || sel.isCollapsed || sel.rangeCount === 0 || !box) return setSpot(null);
      const range = sel.getRangeAt(0);
      if (!box.contains(range.startContainer) || !box.contains(range.endContainer)) return setSpot(null);
      const text = sel.toString();
      if (!text.trim()) return setSpot(null);
      const r = range.getBoundingClientRect();
      setTaken(false);
      setSpot({ x: r.left + r.width / 2, top: r.top, bottom: r.bottom, text });
    };
    const soon = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(read, 180);
    };
    const hide = () => setSpot(null);
    document.addEventListener("selectionchange", soon);
    document.addEventListener("pointerup", soon);
    window.addEventListener("scroll", hide, true);
    window.addEventListener("resize", hide);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("selectionchange", soon);
      document.removeEventListener("pointerup", soon);
      window.removeEventListener("scroll", hide, true);
      window.removeEventListener("resize", hide);
    };
  }, [sink]);

  const take = () => {
    if (!sink || !spot) return;
    const tg = targetRef.current;
    sink.add({ source: tg.source, ref: tg.ref, label: tg.label, text: clip(spot.text), locator: tg.text ? lineLocator(tg.text, spot.text) : "" });
    setTaken(true);
    window.setTimeout(() => {
      setSpot(null);
      document.getSelection()?.removeAllRanges();
    }, 700);
  };

  const above = spot && spot.top > 52;
  return (
    <div ref={ref} className={className}>
      {children}
      {sink && spot
        ? createPortal(
            <button
              type="button"
              // Keep the selection: a press must not collapse it.
              onPointerDown={(e) => e.preventDefault()}
              onClick={take}
              style={{ left: spot.x, top: above ? spot.top - 8 : spot.bottom + 8 }}
              className={`rise fixed z-[60] flex h-8 -translate-x-1/2 items-center gap-1.5 rounded-control bg-ink px-3 text-[12.5px] font-medium text-on-ink shadow-float transition-[transform] duration-[160ms] ease-quiet active:scale-[.97] active:duration-[70ms] ${above ? "-translate-y-full" : ""}`}
            >
              {taken ? <Check size={13} className="pop" /> : <TextQuote size={13} />}
              <span>{taken ? t("Quoted") : t("Quote")}</span>
            </button>,
            document.body,
          )
        : null}
    </div>
  );
}

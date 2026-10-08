import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

export function FadeScroll({
  className = "",
  innerClass = "",
  fade = "from-canvas",
  children,
}: {
  className?: string;
  innerClass?: string;
  fade?: "from-canvas" | "from-well";
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [edge, setEdge] = useState({ start: false, end: false });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const tick = () => {
      setEdge({
        start: el.scrollLeft > 1,
        end: el.scrollLeft + el.clientWidth < el.scrollWidth - 1,
      });
    };
    tick();
    el.addEventListener("scroll", tick, { passive: true });
    const ro = new ResizeObserver(tick);
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", tick);
      ro.disconnect();
    };
  }, []);
  function nudge(dir: -1 | 1) {
    const el = ref.current;
    if (!el) return;
    el.scrollBy({ left: dir * Math.max(160, el.clientWidth * 0.7), behavior: "smooth" });
  }
  return (
    <div className={`relative min-w-0 ${className}`}>
      <div ref={ref} className={`silo-scroll-x relative h-full ${innerClass}`}>
        {children}
      </div>
      {edge.start ? (
        <div className={`absolute inset-y-0 left-0 z-10 flex w-8 items-stretch bg-gradient-to-r ${fade} to-transparent`}>
          <button
            type="button"
            title="Previous"
            className="flex w-8 items-center justify-center text-ink-2 hover:text-ink"
            onClick={() => nudge(-1)}
          >
            <ChevronLeft size={14} />
          </button>
        </div>
      ) : null}
      {edge.end ? (
        <div className={`absolute inset-y-0 right-0 z-10 flex w-8 items-stretch justify-end bg-gradient-to-l ${fade} to-transparent`}>
          <button
            type="button"
            title="Next"
            className="flex w-8 items-center justify-center text-ink-2 hover:text-ink"
            onClick={() => nudge(1)}
          >
            <ChevronRight size={14} />
          </button>
        </div>
      ) : null}
    </div>
  );
}

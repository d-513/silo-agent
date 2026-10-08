import { HardDrive } from "lucide-react";
import { useMemo } from "react";

export const CATEGORY_LABEL: Record<string, string> = {
  consumer: "Personal cloud",
  "self-hosted": "Self-hosted",
  "object-storage": "Object storage",
  protocol: "File servers",
};

// Provider marks are the brands' own SVGs (thesvg.org), drawn as an <img> from
// a data URI: several inline SVGs would share ids (gradients, masks) and break
// each other, and an <img> never runs markup from the file.
export function DriveMark({ svg, size = 40, muted = false, className = "" }: { svg?: string; size?: number; muted?: boolean; className?: string }) {
  const src = useMemo(() => (svg ? `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}` : ""), [svg]);
  return (
    <div
      aria-hidden
      className={`flex shrink-0 items-center justify-center rounded-sm ${src ? "bg-mark" : "bg-well"} ${className}`}
      style={{ width: size, height: size, padding: Math.round(size * 0.16) }}
    >
      {src ? (
        <img src={src} alt="" draggable={false} className={`h-full w-full object-contain ${muted ? "opacity-45 grayscale" : ""}`} />
      ) : (
        <HardDrive size={Math.round(size * 0.5)} className="text-ink-2" />
      )}
    </div>
  );
}

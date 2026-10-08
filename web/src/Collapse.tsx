import { useState, type ReactNode } from "react";

// grid-template-rows 0fr → 1fr fold; contents fade in 60ms behind the height.
// Children mount on first open so closed rows cost nothing to render.
export function Collapse({ open, children }: { open: boolean; children: ReactNode }) {
  const [mounted, setMounted] = useState(open);
  if (open && !mounted) setMounted(true);
  return (
    <div
      className={`grid transition-[grid-template-rows] duration-[320ms] ease-quiet motion-reduce:transition-none ${
        open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
      }`}
    >
      <div
        className={`min-h-0 overflow-hidden transition-opacity ease-quiet ${
          open ? "opacity-100 delay-[60ms] duration-[260ms]" : "opacity-0 duration-[120ms]"
        }`}
        inert={!open}
      >
        {mounted ? children : null}
      </div>
    </div>
  );
}

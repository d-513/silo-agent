import { Timer } from "lucide-react";
import type { Automation } from "./gen/silo/v1/ui_pb";

// AutomationMark is an automation's glyph, drawn on lucide's 24px grid and
// stroke so it sits with the other icons. The pinned ones say what they are:
// the Heartbeat is a pulse trace, a flat line while it never runs on its own
// (no schedule, or paused), and the trace redraws when the row is hovered;
// the Dashboard's curator is the board itself, a wide widget over two small
// ones, whose last tile fills on hover. Everything else is the timer.
// Hover is the `group/auto` around it.
export function AutomationMark({ a, size = 15 }: { a: Automation; size?: number }) {
  if (a.kind === "heartbeat") return <Heartbeat alive={a.enabled && !!a.schedule} size={size} />;
  if (a.kind === "dashboard") return <Board size={size} />;
  return <Timer size={size} />;
}

const svg = {
  xmlns: "http://www.w3.org/2000/svg",
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  "aria-hidden": true,
};

function Heartbeat({ alive, size }: { alive: boolean; size: number }) {
  return (
    <svg {...svg} width={size} height={size}>
      {alive ? (
        <path pathLength={1} className="beat-trace" d="M2 12h4.5l2-4.5 3.5 11 2.5-8.5 1.5 2H22" />
      ) : (
        <path d="M2 12h20" />
      )}
    </svg>
  );
}

function Board({ size }: { size: number }) {
  return (
    <svg {...svg} width={size} height={size}>
      <rect x="3" y="3" width="18" height="7.5" rx="2" />
      <rect x="3" y="13.5" width="7.5" height="7.5" rx="2" />
      <rect x="13.5" y="13.5" width="7.5" height="7.5" rx="2" className="board-tile" />
    </svg>
  );
}

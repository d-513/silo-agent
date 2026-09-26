import { useEffect, useRef, useState } from "react";

export type LampStatus = "online" | "idle" | "working" | "needs_you" | "starting" | "stopped";

type Ground = "canvas" | "surface" | "well" | "pressed";
type Norm = Exclude<LampStatus, "idle">;

function norm(status: string): Norm {
  if (status === "idle") return "online";
  if (status === "online" || status === "working" || status === "needs_you" || status === "starting") return status;
  return "stopped";
}

// Online/Working are solid lamp green (Working breathes), Needs you is
// vermilion, Starting and Stopped are hollow 1.5px rings.
function look(s: Norm): { bg: string; ring: string } {
  switch (s) {
    case "online":
    case "working":
      return { bg: "var(--color-lamp)", ring: "transparent" };
    case "needs_you":
      return { bg: "var(--color-vermilion)", ring: "transparent" };
    case "starting":
      return { bg: "transparent", ring: "var(--color-cobalt)" };
    default:
      return { bg: "transparent", ring: "var(--color-ink-3)" };
  }
}

/**
 * A status lamp. `onCrest` names the ground it sits on when pinned to a crest
 * corner: the lamp grows to 9px with a 2px ring in that color.
 */
export function Lamp({
  status,
  onCrest,
  className = "",
}: {
  status: string;
  onCrest?: Ground;
  className?: string;
}) {
  const s = norm(status);
  const prev = useRef<Norm | null>(null);
  const [ping, setPing] = useState(0);
  useEffect(() => {
    // One ping when the state becomes Needs you — never on first mount.
    if (prev.current !== null && prev.current !== "needs_you" && s === "needs_you") setPing((n) => n + 1);
    prev.current = s;
  }, [s]);
  const { bg, ring } = look(s);
  const size = onCrest ? 9 : 7;
  const ground = onCrest ? `var(--color-${onCrest})` : "";
  const hollow = bg === "transparent";
  const shadow = [`inset 0 0 0 1.5px ${ring}`, onCrest ? `0 0 0 2px ${ground}` : ""].filter(Boolean).join(", ");
  return (
    <span
      aria-hidden
      className={`${/\b(absolute|fixed)\b/.test(className) ? "" : "relative"} inline-block shrink-0 rounded-full ${className}`}
      style={{ width: size, height: size }}
    >
      <span
        className={`absolute inset-0 rounded-full transition-[background-color,box-shadow] duration-[320ms] ease-quiet ${
          s === "working" ? "breathe" : ""
        }`}
        style={{ background: hollow && onCrest ? ground : bg, boxShadow: shadow }}
      />
      {ping > 0 ? (
        <span
          key={ping}
          className="ping pointer-events-none absolute inset-0 rounded-full bg-vermilion"
          onAnimationEnd={() => setPing(0)}
        />
      ) : null}
    </span>
  );
}

const WORDS: Record<Norm, string> = {
  online: "Online",
  working: "Working",
  needs_you: "Needs you",
  starting: "Starting",
  stopped: "Stopped",
};

const TONE: Record<Norm, string> = {
  online: "text-emerald",
  working: "text-emerald",
  needs_you: "text-vermilion",
  starting: "text-ink-2",
  stopped: "text-ink-3",
};

export function statusText(status: string) {
  return WORDS[norm(status)];
}

/** The status word beside a lamp: the old word fades out while the new one rises 4px. */
export function StatusWord({ status, className = "" }: { status: string; className?: string }) {
  const s = norm(status);
  const [shown, setShown] = useState<{ cur: Norm; old: Norm | null; n: number }>({ cur: s, old: null, n: 0 });
  if (shown.cur !== s) setShown({ cur: s, old: shown.cur, n: shown.n + 1 });
  return (
    <span className={`inline-grid text-[12px] leading-4 font-medium ${className}`} aria-live="polite">
      {shown.old ? (
        <span
          key={`o${shown.n}`}
          aria-hidden
          className={`word-out col-start-1 row-start-1 whitespace-nowrap ${TONE[shown.old]}`}
          onAnimationEnd={() => setShown((x) => ({ ...x, old: null }))}
        >
          {WORDS[shown.old]}
        </span>
      ) : null}
      <span key={`n${shown.n}`} className={`col-start-1 row-start-1 whitespace-nowrap ${shown.n ? "word-in" : ""} ${TONE[s]}`}>
        {WORDS[s]}
      </span>
    </span>
  );
}

/** Lamp + word, the pair every status uses (a lamp is never the only signal). */
export function Status({ status, className = "" }: { status: string; className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <Lamp status={status} />
      <StatusWord status={status} />
    </span>
  );
}

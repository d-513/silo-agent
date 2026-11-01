import { useEffect, useRef, useState } from "react";
import type { LampStatus, StatusLook } from "./statusLook";

type Ground = "canvas" | "surface" | "well" | "pressed";

// Online/Working are solid lamp green (Working breathes), Needs you is
// vermilion, Starting and Stopped are hollow 1.5px rings.
function look(s: LampStatus): { bg: string; ring: string } {
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
  status: LampStatus;
  onCrest?: Ground;
  className?: string;
}) {
  const s = status;
  const prev = useRef<LampStatus | null>(null);
  const [ping, setPing] = useState(0);
  useEffect(() => {
    // One ping when the state becomes Needs you — never on first mount.
    if (prev.current !== null && prev.current !== "needs_you" && s === "needs_you") setPing((n) => n + 1);
    prev.current = s;
  }, [s]);
  if (s === "none") return null;
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

/** The status word beside a lamp: the old word fades out while the new one rises 4px. */
export function StatusWord({ look: l, className = "" }: { look: StatusLook; className?: string }) {
  const [shown, setShown] = useState<{ cur: StatusLook; old: StatusLook | null; n: number }>({ cur: l, old: null, n: 0 });
  if (shown.cur.word !== l.word || shown.cur.tone !== l.tone) setShown({ cur: l, old: shown.cur, n: shown.n + 1 });
  return (
    <span className={`inline-grid min-w-0 grid-cols-[minmax(0,1fr)] text-label leading-4 font-medium ${className}`} aria-live="polite" title={l.word}>
      {shown.old ? (
        <span
          key={`o${shown.n}`}
          aria-hidden
          className={`word-out col-start-1 row-start-1 truncate ${shown.old.tone}`}
          onAnimationEnd={() => setShown((x) => ({ ...x, old: null }))}
        >
          {shown.old.word}
        </span>
      ) : null}
      <span key={`n${shown.n}`} className={`col-start-1 row-start-1 truncate ${shown.n ? "word-in" : ""} ${l.tone}`}>
        {l.word}
      </span>
    </span>
  );
}

/** Lamp + word, the pair every status uses (a lamp is never alone). */
export function Status({ look: l, className = "" }: { look: StatusLook; className?: string }) {
  return (
    <span className={`inline-flex min-w-0 items-center gap-2 ${className}`}>
      <Lamp status={l.lamp} />
      <StatusWord look={l} />
    </span>
  );
}

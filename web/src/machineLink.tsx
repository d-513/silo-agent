import { useCallback, useState, type MouseEventHandler, type RefObject } from "react";

// The state of a live link to a Bot's machine (the Desktop hatch and the Console).
export type LinkPhase = "off" | "connecting" | "connected" | "lost";

// useLinkPhase holds the phase and the retry counter; bumping the counter
// re-runs the connect effect that reads it.
export function useLinkPhase() {
  const [phase, setPhase] = useState<LinkPhase>("off");
  const [attempt, setAttempt] = useState(0);
  const nextAttempt = useCallback(() => setAttempt((n) => n + 1), []);
  return { phase, setPhase, attempt, nextAttempt };
}

// retryDelay is how long a lost link waits before dialing again: 2s, 4s, 8s, then 15s.
export const retryDelay = (attempt: number) => Math.min(15000, 2000 * 2 ** Math.min(attempt, 3));

// LinkSurface is the dark well a remote screen draws into, with the status
// words over it until the link is up. `noun` is "Desktop" or "Console".
export function LinkSurface({
  noun,
  phase,
  surfaceRef,
  surfaceClass,
  onMouseDown,
}: {
  noun: string;
  phase: LinkPhase;
  surfaceRef: RefObject<HTMLDivElement | null>;
  surfaceClass: string;
  onMouseDown?: MouseEventHandler<HTMLDivElement>;
}) {
  return (
    <div className="relative h-full min-h-0 w-full bg-matte">
      <div ref={surfaceRef} className={`${surfaceClass} h-full min-h-[320px] w-full`} onMouseDown={onMouseDown} />
      {phase !== "connected" && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center font-mono text-[12.5px] text-white/80">
          {phase === "off" ? `${noun} not connected` : phase === "connecting" ? `Opening ${noun.toLowerCase()}…` : `${noun} lost`}
        </div>
      )}
    </div>
  );
}

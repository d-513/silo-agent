import { ArrowUp } from "lucide-react";

// Send and Stop are one 34px button. The arrow lifts out while the square
// rotates in (320ms settle); the fill crossfades over 240ms.
export function SendStop({ sending, canSend, onStop }: { sending: boolean; canSend: boolean; onStop: () => void }) {
  const fill = sending ? "bg-vermilion hover:bg-vermilion-deep" : canSend ? "bg-ink hover:bg-ink-deep" : "bg-pressed";
  const glyph = "col-start-1 row-start-1 transition-[transform,opacity] duration-[320ms] ease-settle motion-reduce:transition-opacity";
  return (
    <button
      type={sending ? "button" : "submit"}
      onClick={sending ? onStop : undefined}
      disabled={!sending && !canSend}
      title={sending ? "Stop this reply" : "Send message"}
      aria-label={sending ? "Stop this reply" : "Send message"}
      className={`grid h-[34px] w-[34px] shrink-0 place-items-center rounded-[11px] transition-[background-color,transform] duration-[240ms] ease-quiet active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:active:scale-100 ${fill}`}
    >
      <ArrowUp
        size={17}
        strokeWidth={2.25}
        className={`${glyph} ${canSend && !sending ? "text-on-ink" : "text-ink-3"} ${
          sending ? "-translate-y-2.5 scale-[.6] opacity-0" : "opacity-100"
        }`}
      />
      <span
        className={`${glyph} h-2.5 w-2.5 rounded-[3px] bg-on-accent ${sending ? "opacity-100" : "-rotate-90 scale-[.6] opacity-0"}`}
      />
    </button>
  );
}

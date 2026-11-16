import { useState } from "react";
import type { CallSnapshot } from "./session";
import { t } from "../i18n.ts";

// Timings is a debug readout of the last turn's latency: a small mono chip
// with the time to the Bot's first sound, which opens the stages behind it.
// The stage names are left as they are in the code, untranslated.

const fmt = (ms: number | undefined) => (ms ? `${ms} ms` : "–");

export function Timings({ s, up }: { s: CallSnapshot; up?: boolean }) {
  const [open, setOpen] = useState(false);
  const l = s.lap;
  // What the ear waits for: the browser's end of turn, the server's part, and
  // the browser's playout buffer.
  const heard = l?.first_ms ? l.eou_ms + l.first_ms + (s.playoutMs ?? 0) : 0;
  const rows: [string, string][] = l
    ? [
        ["end of turn", fmt(l.eou_ms)],
        ["speech-to-text", fmt(l.stt_ms)],
        ...(l.talker || l.talker_ms ? ([["talker", `${fmt(l.talker_ms)}${l.talker ? ` · ${l.talker}` : ""}`]] as [string, string][]) : []),
        ["bot", fmt(l.bot_ms)],
        ["tts", fmt(l.tts_ms)],
        ["first sound", fmt(l.first_ms)],
        ["playout", fmt(s.playoutMs)],
        ["≈ heard after", fmt(heard)],
      ]
    : [["playout", fmt(s.playoutMs)]];
  return (
    <span className="relative shrink-0">
      <button
        type="button"
        aria-expanded={open}
        aria-label={t("Latency")}
        onClick={() => setOpen((o) => !o)}
        className={`rounded-xs px-1 font-mono text-micro tabular-nums transition-colors duration-[160ms] ease-quiet hover:bg-well hover:text-ink ${open ? "bg-well text-ink" : "text-ink-3"}`}
      >
        {heard ? `${(heard / 1000).toFixed(1)}s` : "ms"}
      </button>
      {open ? (
        <span className={`absolute left-0 z-20 grid w-max grid-cols-[auto_auto] gap-x-3 rounded-sm bg-surface px-2.5 py-2 text-left font-mono text-micro leading-[16px] text-ink-2 shadow-card ${up ? "bottom-full mb-1" : "top-full mt-1"}`}>
          {rows.map(([k, v]) => (
            <span key={k} className="contents">
              <span className="text-ink-3">{k}</span>
              <span className="text-right tabular-nums">{v}</span>
            </span>
          ))}
        </span>
      ) : null}
    </span>
  );
}

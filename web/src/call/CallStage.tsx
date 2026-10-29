import { Hand, Keyboard, MessageSquareText, Mic, MicOff, PhoneOff } from "lucide-react";
import { useEffect, useState } from "react";
import { Crest } from "../Crest";
import { Spinner } from "../Feedback";
import { Lamp } from "../Lamp";
import { Tip, TipTitle } from "../Tip";
import { toolMeta } from "../thread/toolInfo";
import { Clock, stateWords, useHoldKeys } from "./CallBar";
import type { Call } from "./useCall";
import type { CallSnapshot } from "./session";
import { t } from "../i18n.ts";

// CallStage is a call with the chat column to itself: the Bot's face with
// rings that carry the voice, what it is saying in the reply serif, what it
// heard from you, and a dock of controls. The thread is still written
// underneath; Show chat folds the call back into the bar over the composer.

const dk =
  "flex h-12 w-12 shrink-0 items-center justify-center rounded-card bg-surface text-ink-2 shadow-card transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms] motion-reduce:active:scale-100";

export function CallStage({
  call,
  botName,
  crest,
  where,
  waiting,
  typing,
  onType,
  onMinimize,
}: {
  call: Call;
  botName: string;
  crest: number;
  // Which chat the call is held in, for the top line.
  where: string;
  // A run of this chat is paused on an approval: the slip is on screen.
  waiting: boolean;
  typing: boolean;
  onType: () => void;
  onMinimize: () => void;
}) {
  const s = call.snap;
  const session = call.session;
  const ended = s?.state === "ended";
  useHoldKeys(s ?? idle, session);
  if (!s) return null;
  return (
    <section className="flex min-h-0 flex-1 flex-col" aria-label={t("Call with {{name}}", { name: botName })}>
      <header className="flex h-12 shrink-0 items-center gap-2 px-4">
        <span className="flex min-w-0 items-center gap-2 text-[12.5px] text-ink-3">
          {ended ? null : <Lamp status="working" />}
          <span className="truncate">{ended ? t("Call ended") : t("On a call · {{where}}", { where })}</span>
          {ended ? null : <Clock since={s.startedAt} />}
        </span>
        <span className="flex-1" />
        <button
          type="button"
          onClick={onMinimize}
          className="flex h-8 shrink-0 items-center gap-1.5 rounded-control px-2.5 text-[13px] font-medium text-ink-2 shadow-[inset_0_0_0_1px_var(--color-line-strong)] transition-[background-color,color] duration-[160ms] ease-quiet hover:bg-well hover:text-ink"
        >
          <MessageSquareText size={15} />
          {t("Show chat")}
        </button>
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-6 py-4 text-center">
        <div className="rise flex flex-col items-center">
          <Face s={s} crest={crest} botName={botName} onInterrupt={() => session?.interrupt()} />
          <h2 className="mt-1 text-title text-ink">{botName}</h2>
          <p className={`mt-1 flex min-h-5 items-center gap-2 text-[13px] leading-5 ${s.error && ended ? "text-vermilion" : "text-ink-3"}`} aria-live="polite">
            {s.state === "working" ? <Lamp status="working" /> : null}
            {stateWords(s, botName)}
          </p>
        </div>
        <Captions s={s} botName={botName} />
        <div className="mt-4 flex min-h-8 flex-wrap items-center justify-center gap-2">
          {waiting && !ended ? (
            <span className="flex h-8 items-center gap-2 rounded-sm px-3 text-[12.5px] font-medium text-vermilion shadow-[inset_0_0_0_1px_var(--color-vermilion)]">
              <span aria-hidden className="breathe h-1.5 w-1.5 rounded-full bg-vermilion" />
              {t("Needs you on screen")}
            </span>
          ) : null}
          {s.state === "working" && s.tool && !waiting ? (
            <button
              type="button"
              onClick={onMinimize}
              className="flex h-8 items-center gap-2 rounded-sm bg-well py-0 pr-1 pl-3 text-[12.5px] text-ink-2 transition-colors duration-[160ms] ease-quiet hover:text-ink"
            >
              <Lamp status="working" />
              {t("Working · {{what}}", { what: toolMeta(s.tool).app || s.tool })}
              <span className="rounded-xs bg-surface px-2 py-0.5 font-medium text-ink shadow-card">{t("Show")}</span>
            </button>
          ) : null}
        </div>
      </div>
      <Dock s={s} call={call} typing={typing} onType={onType} />
    </section>
  );
}

const idle: CallSnapshot = { state: "ended", tool: "", heard: "", saying: "", muted: false, ptt: false, holding: false, level: 0, chatId: "", callId: "", error: "", startedAt: 0 };

// Face is the Bot's crest inside three rings: cobalt while you speak, ink
// while the Bot does, swelling with the voice; one quiet ring otherwise,
// breathing while it thinks. A press while the Bot speaks stops it.
function Face({ s, crest, botName, onInterrupt }: { s: CallSnapshot; crest: number; botName: string; onInterrupt: () => void }) {
  const loud = s.state === "hearing" || s.state === "speaking";
  const tone = s.muted ? "text-vermilion" : s.state === "hearing" ? "text-cobalt" : s.state === "working" ? "text-emerald" : s.state === "ended" ? "text-line-strong" : s.state === "listening" ? "text-line-strong" : "text-ink";
  const rings = [0, 1, 2].map((i) => {
    const size = 128 + i * 34;
    const scale = loud ? 1 + s.level * (0.32 - i * 0.06) : 1;
    const opacity = loud ? [0.9, 0.45, 0.2][i] : i === 0 ? (s.state === "ended" ? 0 : s.state === "listening" ? 1 : 0.35) : 0;
    return (
      <span
        key={i}
        aria-hidden
        className={`absolute rounded-full transition-[transform,opacity] duration-[90ms] ease-quiet motion-reduce:transition-none ${i === 0 && s.state === "thinking" ? "breathe" : ""}`}
        style={{ width: size, height: size, boxShadow: `inset 0 0 0 ${i === 0 ? 2 : 1.25}px currentColor`, transform: `scale(${scale})`, opacity }}
      />
    );
  });
  const body = (
    <span className={`relative flex h-[210px] w-[250px] items-center justify-center ${tone}`}>
      {rings}
      {s.state === "connecting" ? (
        <span className="absolute">
          <Spinner size={20} />
        </span>
      ) : null}
      <span className={s.state === "connecting" ? "opacity-40" : ""}>
        <Crest index={crest} size={104} />
      </span>
      {s.muted ? (
        <span className="absolute top-1/2 left-1/2 flex h-8 w-8 translate-x-[30px] translate-y-[26px] items-center justify-center rounded-full bg-vermilion text-on-accent shadow-[0_0_0_3px_var(--color-canvas)]">
          <MicOff size={15} />
        </span>
      ) : null}
    </span>
  );
  if (s.state !== "speaking") return body;
  return (
    <Tip content={t("Stop {{name}} talking", { name: botName })}>
      <button type="button" aria-label={t("Stop {{name}} talking", { name: botName })} className="rounded-full" onClick={onInterrupt}>
        {body}
      </button>
    </Tip>
  );
}

// Captions are the sentence the Bot is on, in the reply serif, with the one
// before it fading above; your last turn as heard sits under them.
function Captions({ s, botName }: { s: CallSnapshot; botName: string }) {
  const [lines, setLines] = useState({ prev: "", cur: "" });
  useEffect(() => {
    if (s.saying) setLines((l) => (l.cur === s.saying ? l : { prev: l.cur, cur: s.saying }));
  }, [s.saying]);
  const hint =
    s.state === "listening" && !s.heard && !lines.cur
      ? s.muted
        ? t("{{name}} can't hear you. Unmute to talk.", { name: botName })
        : s.ptt
          ? t("Hold the button or Space while you talk.")
          : t("Talk to {{name}}. Speak over it any time to cut in.", { name: botName })
      : "";
  return (
    <div className="mt-6 flex w-full max-w-[600px] flex-col items-center">
      <div className="flex min-h-[124px] flex-col justify-end">
        {s.error && s.state !== "ended" ? (
          <p className="text-[14px] leading-6 text-vermilion">{s.error}</p>
        ) : hint ? (
          <p className="text-[14px] leading-6 text-ink-3">{hint}</p>
        ) : (
          <>
            {lines.prev ? <p className="font-serif text-[18px] leading-[26px] text-ink-3">{lines.prev}</p> : null}
            {lines.cur ? <p className="font-serif text-[24px] leading-[34px] tracking-[-0.005em] text-ink max-sm:text-[21px] max-sm:leading-[30px]">{lines.cur}</p> : null}
          </>
        )}
      </div>
      <p className="mt-3 min-h-[22px] text-[14px] leading-[22px] text-ink-3">{s.heard ? `“${s.heard}”` : ""}</p>
    </div>
  );
}

function Dock({ s, call, typing, onType }: { s: CallSnapshot; call: Call; typing: boolean; onType: () => void }) {
  const session = call.session;
  if (s.state === "ended") {
    return (
      <div className="flex shrink-0 justify-center pt-2 pb-6">
        <button
          type="button"
          onClick={call.dismiss}
          className="flex h-10 items-center gap-2 rounded-control px-4 text-[13.5px] font-medium text-ink shadow-[inset_0_0_0_1px_var(--color-line-strong)] transition-colors duration-[160ms] ease-quiet hover:bg-well"
        >
          <MessageSquareText size={15} />
          {t("Back to the chat")}
        </button>
      </div>
    );
  }
  return (
    <div className="flex shrink-0 flex-col items-center gap-2 pt-2 pb-5">
      {s.ptt ? (
        <button
          type="button"
          aria-pressed={s.holding}
          className={`flex h-11 touch-none items-center gap-2 rounded-card px-5 text-[13.5px] font-medium transition-[background-color,color,transform] duration-[120ms] ease-quiet select-none ${s.holding ? "scale-[.97] bg-cobalt text-on-accent" : "bg-surface text-ink shadow-card hover:bg-well"}`}
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId);
            session?.hold(true);
          }}
          onPointerUp={() => session?.hold(false)}
          onPointerCancel={() => session?.hold(false)}
        >
          {s.holding ? t("Talking…") : t("Hold to talk")}
          <kbd className={`rounded-xs px-1.5 font-mono text-[11px] ${s.holding ? "text-on-accent" : "bg-well text-ink-3"}`}>{t("Space")}</kbd>
        </button>
      ) : null}
      <div className="flex items-center gap-2.5">
        <Tip
          content={
            <>
              <TipTitle>{s.ptt ? t("Hands-free") : t("Hold to talk")}</TipTitle>
              <p className="mt-1 text-[12px] leading-[17px] text-ink-2">{s.ptt ? t("Talk any time; a pause ends your turn.") : t("Only what you say while holding the button (or Space) is heard. For a noisy room, or speakers the Bot hears itself through.")}</p>
            </>
          }
        >
          <button type="button" className={`${dk} ${s.ptt ? "bg-well text-ink" : ""}`} aria-pressed={s.ptt} aria-label={t("Hold to talk")} onClick={() => session?.setPtt(!s.ptt)}>
            <Hand size={19} />
          </button>
        </Tip>
        <Tip content={s.muted ? t("Unmute") : t("Mute")}>
          <button
            type="button"
            className={`${dk} ${s.muted ? "bg-vermilion-pale text-vermilion shadow-none hover:bg-vermilion-pale hover:text-vermilion" : ""}`}
            aria-pressed={s.muted}
            aria-label={s.muted ? t("Unmute") : t("Mute")}
            onClick={() => session?.setMuted(!s.muted)}
          >
            {s.muted ? <MicOff size={19} /> : <Mic size={19} />}
          </button>
        </Tip>
        <Tip content={t("Type a message")}>
          <button type="button" className={`${dk} ${typing ? "bg-well text-ink" : ""}`} aria-pressed={typing} aria-label={t("Type a message")} onClick={onType}>
            <Keyboard size={19} />
          </button>
        </Tip>
        <button
          type="button"
          onClick={call.end}
          className="ml-1 flex h-12 shrink-0 items-center gap-2 rounded-card bg-vermilion px-5 text-[14px] font-medium text-on-accent transition-[filter,transform] duration-[160ms] ease-quiet hover:brightness-95 active:scale-[.96] active:duration-[70ms] motion-reduce:active:scale-100"
        >
          <PhoneOff size={17} />
          {t("End", { context: "call" })}
        </button>
      </div>
    </div>
  );
}

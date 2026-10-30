import { AudioLines, Hand, Maximize2, Mic, MicOff, PhoneOff, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Spinner } from "../Feedback";
import { Tip, TipTitle } from "../Tip";
import type { Bot as BotRow } from "../gen/silo/v1/ui_pb";
import { chatDoing, type Doing } from "../statusLook";
import { callClock } from "./audio";
import { callTone, type CallTone, stateWords } from "./look";
import type { Call } from "./useCall";
import type { CallSession, CallSnapshot } from "./session";
import { t } from "../i18n.ts";

// CallBar is a live call, docked over the composer of the chat it is held in,
// or under any other page of the Bot (place.ts): the orb (your voice, or the
// Bot's), what is going on in words, what was last heard or said, and the
// controls. The call screen (CallStage) is the same call with the room to
// itself; the bar's maximise button opens it.

const ctl =
  "flex h-9 w-9 shrink-0 items-center justify-center rounded-control text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms]";

export function CallBar({ call, bot, onExpand }: { call: Call; bot: BotRow; onExpand?: () => void }) {
  const s = call.snap;
  if (!s) return null;
  return <Bar s={s} call={call} botName={bot.name} doing={chatDoing(bot, s.chatId)} onExpand={onExpand} />;
}

// useHoldKeys makes Space hold-to-talk while that mode is on and no field
// has the keys. The bar and the call screen share it.
export function useHoldKeys(s: CallSnapshot, session: CallSession | null) {
  const ended = s.state === "ended";
  useEffect(() => {
    if (!s.ptt || ended || !session) return;
    const typing = (e: KeyboardEvent) => e.target instanceof HTMLElement && (e.target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName));
    const down = (e: KeyboardEvent) => {
      if (e.code !== "Space" || e.repeat || typing(e)) return;
      e.preventDefault();
      session.hold(true);
    };
    const up = (e: KeyboardEvent) => {
      if (e.code !== "Space" || typing(e)) return;
      e.preventDefault();
      session.hold(false);
    };
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
    };
  }, [s.ptt, ended, session]);
}

function Bar({ s, call, botName, doing, onExpand }: { s: CallSnapshot; call: Call; botName: string; doing?: Doing; onExpand?: () => void }) {
  const session = call.session;
  const ended = s.state === "ended";

  useHoldKeys(s, session);

  return (
    <div className="mx-auto w-full max-w-[760px] px-4 pb-2" role="region" aria-label={t("Call with {{name}}", { name: botName })}>
      <div className={`rise flex items-center gap-3 rounded-card bg-surface py-2.5 pr-2.5 pl-3 shadow-card ${ended ? "opacity-80" : ""}`}>
        <Orb s={s} onInterrupt={() => session?.interrupt()} botName={botName} />
        <div className="min-w-0 flex-1" aria-live="polite">
          <div className="flex items-baseline gap-2">
            <span className={`truncate text-[13.5px] leading-5 font-medium ${(s.error && ended) || s.state === "needs_you" ? "text-vermilion" : "text-ink"}`}>{stateWords(s, botName, doing)}</span>
            {!ended ? <Clock since={s.startedAt} /> : null}
          </div>
          <Caption s={s} botName={botName} />
        </div>
        {ended ? (
          <button type="button" className={ctl} aria-label={t("Dismiss")} onClick={call.dismiss}>
            <X size={16} />
          </button>
        ) : (
          <div className="flex shrink-0 items-center gap-0.5">
            {s.ptt ? <HoldToTalk s={s} onHold={(d) => session?.hold(d)} /> : null}
            <Tip
              content={
                <>
                  <TipTitle>{s.ptt ? t("Hands-free") : t("Hold to talk")}</TipTitle>
                  <p className="mt-1 text-[12px] leading-[17px] text-ink-2">{s.ptt ? t("Talk any time; a pause ends your turn.") : t("Only what you say while holding the button (or Space) is heard. For a noisy room, or speakers the Bot hears itself through.")}</p>
                </>
              }
            >
              <button type="button" className={`${ctl} ${s.ptt ? "bg-well text-ink" : ""}`} aria-pressed={s.ptt} aria-label={t("Hold to talk")} onClick={() => session?.setPtt(!s.ptt)}>
                <Hand size={16} />
              </button>
            </Tip>
            <Tip content={s.muted ? t("Unmute") : t("Mute")}>
              <button type="button" className={`${ctl} ${s.muted ? "bg-vermilion-pale text-vermilion hover:bg-vermilion-pale hover:text-vermilion" : ""}`} aria-pressed={s.muted} aria-label={s.muted ? t("Unmute") : t("Mute")} onClick={() => session?.setMuted(!s.muted)}>
                {s.muted ? <MicOff size={16} /> : <Mic size={16} />}
              </button>
            </Tip>
            {onExpand ? (
              <Tip content={t("Open the call screen")}>
                <button type="button" className={ctl} aria-label={t("Open the call screen")} onClick={onExpand}>
                  <Maximize2 size={16} />
                </button>
              </Tip>
            ) : null}
            <Tip content={t("End call")}>
              <button
                type="button"
                className="ml-1 flex h-9 shrink-0 items-center gap-1.5 rounded-control bg-vermilion px-3 text-[13px] font-medium text-on-accent transition-[filter,transform] duration-[160ms] ease-quiet hover:brightness-95 active:scale-[.96] active:duration-[70ms]"
                aria-label={t("End call")}
                onClick={call.end}
              >
                <PhoneOff size={15} />
                <span className="max-sm:hidden">{t("End", { context: "call" })}</span>
              </button>
            </Tip>
          </div>
        )}
      </div>
    </div>
  );
}

// Orb shows whose voice it is and how loud: cobalt while you speak, ink while
// the Bot does, breathing while it thinks. A press while the Bot speaks stops it.
const orbTone: Record<CallTone, string> = {
  you: "bg-cobalt",
  bot: "bg-ink",
  needs_you: "bg-vermilion",
  muted: "bg-vermilion-pale",
  working: "bg-emerald",
  thinking: "breathe bg-ink-3",
  ended: "bg-line-strong",
  quiet: "bg-well shadow-[inset_0_0_0_1.5px_var(--color-line-strong)]",
};

function Orb({ s, onInterrupt, botName }: { s: CallSnapshot; onInterrupt: () => void; botName: string }) {
  const speaking = s.state === "speaking";
  const scale = s.state === "hearing" || speaking ? 1 + s.level * 0.45 : 1;
  const tone = orbTone[callTone(s)];
  const body =
    s.state === "connecting" ? (
      <Spinner size={16} />
    ) : (
      <span className={`block h-5 w-5 rounded-full transition-[transform,background-color] duration-[90ms] ease-quiet ${tone}`} style={{ transform: `scale(${scale})` }} />
    );
  if (!speaking) return <span className="flex h-10 w-10 shrink-0 items-center justify-center">{body}</span>;
  return (
    <Tip content={t("Stop {{name}} talking", { name: botName })}>
      <button type="button" aria-label={t("Stop {{name}} talking", { name: botName })} className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full hover:bg-well" onClick={onInterrupt}>
        {body}
      </button>
    </Tip>
  );
}

// Caption is the last thing said: the sentence the Bot is on while it
// speaks, your words once they were heard.
function Caption({ s, botName }: { s: CallSnapshot; botName: string }) {
  const [shown, setShown] = useState("");
  // While a turn streams live, your words show as they are heard.
  const line = s.state === "speaking" ? s.saying : s.hearing ? `“${s.hearing}…”` : s.heard ? `“${s.heard}”` : "";
  useEffect(() => setShown(line), [line]);
  if (s.error && s.state !== "ended") return <p className="truncate text-[12.5px] leading-[18px] text-vermilion">{s.error}</p>;
  if (s.state === "listening" && !s.heard) {
    return <p className="truncate text-[12.5px] leading-[18px] text-ink-3">{s.ptt ? t("Hold the button or Space while you talk.") : t("Talk to {{name}}. Speak over it any time to cut in.", { name: botName })}</p>;
  }
  if (s.state === "thinking" && s.heard === "" && !shown) return <p className="truncate text-[12.5px] leading-[18px] text-ink-3">{t("Listening back…")}</p>;
  return <p className={`truncate text-[12.5px] leading-[18px] ${s.state === "speaking" ? "text-ink-2" : "text-ink-3"}`}>{shown}</p>;
}

function HoldToTalk({ s, onHold }: { s: CallSnapshot; onHold: (down: boolean) => void }) {
  return (
    <button
      type="button"
      aria-pressed={s.holding}
      className={`mr-1 flex h-9 shrink-0 touch-none items-center gap-1.5 rounded-control px-3 text-[13px] font-medium transition-[background-color,color,transform] duration-[120ms] ease-quiet select-none ${s.holding ? "scale-[.97] bg-cobalt text-on-accent" : "bg-well text-ink hover:bg-line"}`}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        onHold(true);
      }}
      onPointerUp={() => onHold(false)}
      onPointerCancel={() => onHold(false)}
    >
      <AudioLines size={15} />
      {s.holding ? t("Talking…") : t("Hold to talk")}
    </button>
  );
}

export function Clock({ since }: { since: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return <span className="shrink-0 font-mono text-[12px] text-ink-3 tabular-nums">{callClock(now - since)}</span>;
}

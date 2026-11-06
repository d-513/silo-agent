import { Square, Volume2 } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { fail } from "./errors";
import { Spinner } from "./Feedback";
import { Tip } from "./Tip";
import { t } from "./i18n.ts";

// Read aloud: a Bot's words in its own voice. One voice at a time on the
// page: starting another stops this one. The slot is taken when the voice is
// asked for, not when it starts playing, so a reply still loading is the one
// that stops (and never plays after another started).
let playing: { owner: string; stop: () => void } | null = null;

type State = "idle" | "loading" | "playing";

/** Speaks text in a Bot's voice (or a voice being tried) and reports how it goes. */
export async function speak(req: { botId: string; text: string; model?: string; voice?: string; speed?: number }, on: { state: (s: State) => void; error: (m: string) => void }, owner = req.text) {
  playing?.stop();
  let stopped = false;
  let audio: HTMLAudioElement | null = null;
  const me = {
    owner,
    stop: () => {
      if (stopped) return;
      stopped = true;
      if (audio) {
        audio.pause();
        audio.removeAttribute("src");
      }
      on.state("idle");
      if (playing === me) playing = null;
    },
  };
  playing = me;
  on.state("loading");
  try {
    const { url } = await ui.prepareSpeech({ botId: req.botId, text: req.text, model: req.model ?? "", voice: req.voice ?? "", speed: req.speed ?? 0 });
    if (stopped) return;
    const a = new Audio(url);
    audio = a;
    a.onplaying = () => {
      if (!stopped) on.state("playing");
    };
    a.onended = me.stop;
    a.onerror = () => {
      if (stopped) return;
      me.stop();
      on.error(t("The voice did not answer. Try again in a moment."));
    };
    await a.play();
  } catch (e) {
    // Stopped while loading or starting: that is not a failure.
    if (stopped) return;
    me.stop();
    on.error(fail(e));
  }
}

/** Stops whatever is being read aloud. */
export function stopSpeaking() {
  playing?.stop();
}

export function ReadAloudButton({ botId, text, className }: { botId: string; text: string; className: string }) {
  const [state, setState] = useState<State>("idle");
  const [err, setErr] = useState("");

  // A reply that leaves the screen stops talking.
  useEffect(() => () => (playing?.owner === text ? playing.stop() : undefined), [text]);

  function start() {
    setErr("");
    void speak({ botId, text }, { state: setState, error: setErr });
  }

  const label = state === "idle" ? t("Read aloud") : t("Stop reading");
  return (
    <Tip content={err || label}>
      <button
        type="button"
        aria-label={label}
        aria-pressed={state !== "idle"}
        className={`${className} ${err ? "text-vermilion" : state === "playing" ? "text-cobalt" : ""}`}
        onClick={() => (state === "idle" ? start() : stopSpeaking())}
      >
        {state === "loading" ? <Spinner size={13} /> : state === "playing" ? <Square size={11} fill="currentColor" strokeWidth={0} /> : <Volume2 size={14} />}
      </button>
    </Tip>
  );
}

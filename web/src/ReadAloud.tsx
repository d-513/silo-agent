import { Square, Volume2 } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { fail } from "./errors";
import { Spinner } from "./Feedback";
import { Tip } from "./Tip";
import { t } from "./i18n.ts";

// Read aloud: a Bot's words in its own voice. One voice at a time on the
// page: starting another stops this one.
let playing: { audio: HTMLAudioElement; stop: () => void } | null = null;

type State = "idle" | "loading" | "playing";

/** Speaks text in a Bot's voice (or a voice being tried) and reports how it goes. */
export async function speak(req: { botId: string; text: string; model?: string; voice?: string; speed?: number }, on: { state: (s: State) => void; error: (m: string) => void }, owner = req.text) {
  playing?.stop();
  on.state("loading");
  try {
    const { url } = await ui.prepareSpeech({ botId: req.botId, text: req.text, model: req.model ?? "", voice: req.voice ?? "", speed: req.speed ?? 0 });
    const audio = new Audio(url);
    audio.dataset.owner = owner;
    const stop = () => {
      audio.pause();
      audio.removeAttribute("src");
      on.state("idle");
      if (playing?.audio === audio) playing = null;
    };
    playing = { audio, stop };
    audio.onplaying = () => on.state("playing");
    audio.onended = stop;
    audio.onerror = () => {
      stop();
      on.error(t("The voice did not answer. Try again in a moment."));
    };
    await audio.play();
  } catch (e) {
    on.state("idle");
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
  useEffect(() => () => (playing?.audio.dataset.owner === text ? playing.stop() : undefined), [text]);

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

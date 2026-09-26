import { useCallback, useEffect, useRef, useState } from "react";

// Composer dictation: record with MediaRecorder, hand the blob to the CP
// (`Transcribe`), and put the text back in the draft. Never auto-sends.

// Containers every transcription upstream accepts, best first. Chrome and
// Firefox record webm/opus; Safari only mp4/aac.
export const MIME_CANDIDATES = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/ogg;codecs=opus"];

// OpenAI/OpenRouter cap one upload at 25 MB; opus at ~32 kbps stays far under
// that for 10 minutes.
export const MAX_RECORDING_MS = 10 * 60 * 1000;

// pickMime returns the first supported container, or "" to let the browser
// choose its default.
export function pickMime(isSupported: (mime: string) => boolean): string {
  for (const m of MIME_CANDIDATES) {
    try {
      if (isSupported(m)) return m;
    } catch {
      /* some engines throw on unknown types */
    }
  }
  return "";
}

// insertDictation splices spoken text into the draft at [start, end), adding a
// space on either side when it would otherwise glue onto a word. It returns
// the new draft and where the caret lands.
export function insertDictation(draft: string, start: number, end: number, spoken: string): { text: string; caret: number } {
  const said = spoken.trim();
  if (!said) return { text: draft, caret: end };
  const s = Math.max(0, Math.min(start, draft.length));
  const e = Math.max(s, Math.min(end, draft.length));
  const before = draft.slice(0, s);
  const after = draft.slice(e);
  const lead = before && !/\s$/.test(before) ? " " : "";
  const trail = after && !/^\s/.test(after) ? " " : "";
  const text = before + lead + said + trail + after;
  return { text, caret: before.length + lead.length + said.length };
}

// fmtClock renders elapsed recording time as m:ss.
export function fmtClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

// micError turns a getUserMedia / MediaRecorder failure into one plain line.
export function micError(err: unknown): string {
  const name = err instanceof DOMException ? err.name : "";
  if (name === "NotAllowedError" || name === "SecurityError") return "Microphone access is blocked for this site.";
  if (name === "NotFoundError" || name === "OverconstrainedError") return "No microphone found.";
  if (name === "NotReadableError") return "The microphone is in use by another app.";
  return err instanceof Error && err.message ? err.message : "Could not start the microphone.";
}

export type DictationState = "idle" | "recording" | "transcribing";

export interface Dictation {
  state: DictationState;
  elapsed: number;
  level: number; // 0..1 input loudness for the meter
  error: string;
  supported: boolean;
  start: () => Promise<void>;
  stop: () => void;
  cancel: () => void;
  clearError: () => void;
}

// micSupported is false on insecure origins (the browser hides
// getUserMedia off https/localhost) and on engines without MediaRecorder.
export function micSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    window.isSecureContext &&
    !!navigator.mediaDevices?.getUserMedia &&
    typeof MediaRecorder !== "undefined"
  );
}

export function useDictation(transcribe: (audio: Uint8Array, mime: string) => Promise<string>, onText: (text: string) => void): Dictation {
  const [state, setState] = useState<DictationState>("idle");
  const [elapsed, setElapsed] = useState(0);
  const [level, setLevel] = useState(0);
  const [error, setError] = useState("");
  const rec = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const audioCtx = useRef<AudioContext | null>(null);
  const raf = useRef(0);
  const timer = useRef(0);
  const cancelled = useRef(false);
  const onTextRef = useRef(onText);
  const transcribeRef = useRef(transcribe);
  onTextRef.current = onText;
  transcribeRef.current = transcribe;

  const release = useCallback(() => {
    cancelAnimationFrame(raf.current);
    window.clearInterval(timer.current);
    stream.current?.getTracks().forEach((t) => t.stop());
    stream.current = null;
    void audioCtx.current?.close().catch(() => {});
    audioCtx.current = null;
    setLevel(0);
  }, []);

  const stop = useCallback(() => {
    const r = rec.current;
    if (r && r.state !== "inactive") r.stop();
  }, []);

  const cancel = useCallback(() => {
    cancelled.current = true;
    stop();
  }, [stop]);

  const start = useCallback(async () => {
    if (rec.current) return;
    setError("");
    cancelled.current = false;
    let media: MediaStream;
    try {
      media = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true } });
    } catch (err) {
      setError(micError(err));
      return;
    }
    stream.current = media;
    const mime = pickMime((m) => MediaRecorder.isTypeSupported(m));
    let r: MediaRecorder;
    try {
      r = mime ? new MediaRecorder(media, { mimeType: mime, audioBitsPerSecond: 32000 }) : new MediaRecorder(media);
    } catch (err) {
      release();
      setError(micError(err));
      return;
    }
    rec.current = r;
    const chunks: Blob[] = [];
    r.ondataavailable = (e) => {
      if (e.data.size > 0) chunks.push(e.data);
    };
    r.onstop = () => {
      rec.current = null;
      release();
      const type = r.mimeType || mime || "audio/webm";
      if (cancelled.current || chunks.length === 0) {
        setState("idle");
        return;
      }
      setState("transcribing");
      void new Blob(chunks, { type })
        .arrayBuffer()
        .then((buf) => transcribeRef.current(new Uint8Array(buf), type))
        .then((text) => {
          if (text.trim()) onTextRef.current(text);
          else setError("No speech was heard.");
        })
        .catch((err: unknown) => setError(err instanceof Error ? err.message.replace(/^\[\w+\]\s*/, "") : "Transcription failed."))
        .finally(() => setState("idle"));
    };

    try {
      const Ctx = window.AudioContext ?? (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
      const ctx = new Ctx();
      audioCtx.current = ctx;
      const analyser = ctx.createAnalyser();
      analyser.fftSize = 512;
      ctx.createMediaStreamSource(media).connect(analyser);
      const buf = new Uint8Array(analyser.fftSize);
      const tick = () => {
        analyser.getByteTimeDomainData(buf);
        let sum = 0;
        for (const v of buf) sum += ((v - 128) / 128) ** 2;
        setLevel(Math.min(1, Math.sqrt(sum / buf.length) * 4));
        raf.current = requestAnimationFrame(tick);
      };
      tick();
    } catch {
      /* the meter is decoration; recording still works */
    }

    const began = Date.now();
    setElapsed(0);
    timer.current = window.setInterval(() => {
      const ms = Date.now() - began;
      setElapsed(ms);
      if (ms >= MAX_RECORDING_MS) stop();
    }, 250);
    r.start(1000);
    setState("recording");
  }, [release, stop]);

  // Leaving the page mid-recording drops the take and frees the mic.
  useEffect(
    () => () => {
      cancelled.current = true;
      const r = rec.current;
      if (r && r.state !== "inactive") r.stop();
      release();
    },
    [release],
  );

  return {
    state,
    elapsed,
    level,
    error,
    supported: micSupported(),
    start,
    stop,
    cancel,
    clearError: () => setError(""),
  };
}

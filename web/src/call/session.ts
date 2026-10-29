import type { MicVAD } from "@ricky0123/vad-web";
import { encodeWAV, joinFrames, loudness, pcm16, playoutMs } from "./audio";
import { micError } from "../voice";
import { t } from "../i18n.ts";

// A call, the browser's half. The microphone runs through the browser's echo
// canceller into a voice detector; each finished turn goes to the control
// plane as one WAV on the call's socket. The Bot's voice comes back on a
// WebRTC track, which is what lets the echo canceller hear it, and the
// moment you start speaking over it the voice is muted here (no round trip)
// and the control plane is told to pause, then to stop if it was real speech.

export type CallState = "connecting" | "listening" | "hearing" | "thinking" | "working" | "speaking" | "ended";

export type EndReason = "hangup" | "idle" | "limit" | "access" | "replaced" | "lost" | "error";

export type CallSnapshot = {
  state: CallState;
  // The tool the Bot is in while it works.
  tool: string;
  // Your last turn as it was heard, and the sentence the Bot is saying.
  heard: string;
  // Your words as they are being heard, while a turn streams live.
  hearing: string;
  saying: string;
  muted: boolean;
  // Hold to talk: no voice detection, a turn is what is said while held.
  ptt: boolean;
  holding: boolean;
  // 0..1: yours while you speak, the Bot's while it speaks.
  level: number;
  chatId: string;
  callId: string;
  error: string;
  ended?: EndReason;
  startedAt: number;
};

// Voice detection settings. A turn ends after redemptionMs of quiet; the Bot
// is ducked after duckFrames of likely speech and stopped once the detector
// is sure (minSpeechMs).
const redemptionMs = 550;
const duckFrames = 3;
const duckThreshold = 0.7;
const minTurnSamples = 16000 / 4;
// Live hearing: the frames kept from before speech starts (~320 ms), and how
// much silence lets the transcriber's end of turn end the turn early.
const preRollFrames = 10;
const eotQuietFrames = 3;
const quietThreshold = 0.35;

type ServerMsg = {
  t: string;
  call_id?: string;
  chat_id?: string;
  sdp?: string;
  candidate?: RTCIceCandidateInit;
  ice_servers?: { urls: string[]; username?: string; credential?: string }[];
  stt?: string;
  n?: number;
  state?: string;
  tool?: string;
  text?: string;
  reason?: string;
  message?: string;
};

export class CallSession {
  snap: CallSnapshot;
  private listeners = new Set<() => void>();
  private ws?: WebSocket;
  private pc?: RTCPeerConnection;
  private stream?: MediaStream;
  private ctx?: AudioContext;
  private vad?: MicVAD;
  private audio = new Audio();
  private botLevel?: AnalyserNode;
  private pending: RTCIceCandidateInit[] = [];
  private remoteSet = false;
  private serverState = "listening";
  private speechRun = 0;
  private ducked = false;
  private cut = false;
  private held: Float32Array[] = [];
  private timers: number[] = [];
  private raf = 0;
  private wake?: WakeLockSentinel;
  private lastEmit = 0;
  // Live hearing (hello's stt is "live"): turns stream as they are spoken.
  private live = false;
  private turnN = 0;
  private turnOpen = false;
  private ring: Float32Array[] = [];
  private quiet = 0;
  // A turn the transcriber ended early; speech that goes on opens the next.
  private endedEarly = false;

  constructor(
    private botId: string,
    chat: string,
  ) {
    this.snap = { state: "connecting", tool: "", heard: "", hearing: "", saying: "", muted: false, ptt: false, holding: false, level: 0, chatId: chat, callId: "", error: "", startedAt: Date.now() };
    this.audio.autoplay = true;
  }

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  get = () => this.snap;

  private set(patch: Partial<CallSnapshot>) {
    this.snap = { ...this.snap, ...patch };
    for (const fn of this.listeners) fn();
  }

  /** Starts the call. Must run from a press: it asks for the microphone and starts audio. */
  async start(): Promise<void> {
    try {
      this.stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 } });
    } catch (e) {
      this.fail(micError(e));
      return;
    }
    this.ctx = new AudioContext();
    void this.audio.play().catch(() => {});
    void navigator.wakeLock?.request("screen").then((w) => (this.wake = w)).catch(() => {});
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const chat = this.snap.chatId;
    this.ws = new WebSocket(`${proto}//${location.host}/voice/call?bot=${encodeURIComponent(this.botId)}&chat=${encodeURIComponent(chat)}`);
    this.ws.binaryType = "arraybuffer";
    this.ws.onmessage = (e) => {
      if (typeof e.data === "string") void this.onMessage(JSON.parse(e.data) as ServerMsg);
    };
    this.ws.onclose = (e) => {
      if (this.snap.state === "ended") return;
      // A refusal before the socket opened comes back as a close with no reason.
      if (e.code === 1006 && !this.snap.callId) this.fail(t("The call could not start. Voice may be off, or this Bot is not yours to call."));
      else this.finish("lost");
    };
    try {
      await this.startVAD();
    } catch (e) {
      this.fail(t("The voice detector did not load: {{error}}", { error: e instanceof Error ? e.message : String(e) }));
    }
  }

  private async startVAD() {
    const stream = this.stream!;
    // The detector and its runtime load on the first call only.
    const { MicVAD } = await import("@ricky0123/vad-web");
    this.vad = await MicVAD.new({
      model: "v5",
      baseAssetPath: "/vad/",
      onnxWASMBasePath: "/vad/",
      audioContext: this.ctx,
      getStream: async () => stream,
      pauseStream: async () => {},
      resumeStream: async () => stream,
      positiveSpeechThreshold: 0.5,
      negativeSpeechThreshold: 0.35,
      redemptionMs,
      preSpeechPadMs: 300,
      minSpeechMs: 250,
      submitUserSpeechOnPause: false,
      ortConfig: (ort) => {
        ort.env.wasm.numThreads = 1;
      },
      onFrameProcessed: (p, frame) => this.onFrame(p.isSpeech, frame),
      onSpeechStart: () => {
        if (this.snap.ptt) return;
        this.set({ state: "hearing" });
        if (this.live) this.openTurn();
      },
      onSpeechRealStart: () => {
        if (!this.snap.ptt && this.ducked) this.interrupt();
      },
      onVADMisfire: () => {
        if (this.ducked && !this.cut) this.unduck();
        if (this.live && !this.snap.ptt && this.turnOpen) {
          this.turnOpen = false;
          this.send({ t: "cancel", n: this.turnN });
        }
        this.endedEarly = false;
        if (!this.snap.ptt && this.snap.state === "hearing") this.set({ state: this.fromServer() });
      },
      onSpeechEnd: (audio) => {
        if (this.snap.ptt) return;
        if (this.live) {
          if (this.turnOpen) this.endTurn(redemptionMs);
          this.endedEarly = false;
          return;
        }
        this.sendTurn(audio, redemptionMs);
      },
    });
    if (this.snap.state !== "ended") void this.vad.start();
  }

  // Every 32 ms frame of the microphone: the meter, hold-to-talk capture, and
  // ducking the Bot the moment you seem to start speaking over it.
  private onFrame(speech: number, frame: Float32Array) {
    if (this.snap.holding && !this.live) this.held.push(frame.slice());
    if (this.live && !this.snap.muted) this.liveFrame(speech, frame);
    if (this.serverState === "speaking" && !this.ducked && !this.snap.ptt) {
      this.speechRun = speech > duckThreshold ? this.speechRun + 1 : 0;
      if (this.speechRun >= duckFrames) this.duck();
    }
    if (this.serverState !== "speaking" || this.ducked) this.level(loudness(frame));
  }

  private level(v: number) {
    const now = performance.now();
    if (now - this.lastEmit < 60 && Math.abs(v - this.snap.level) < 0.15) return;
    this.lastEmit = now;
    this.set({ level: v });
  }

  private duck() {
    this.ducked = true;
    this.audio.muted = true;
    this.send({ t: "duck" });
  }

  private unduck() {
    this.ducked = false;
    this.speechRun = 0;
    this.audio.muted = false;
    this.send({ t: "unduck" });
  }

  /** Stops the Bot mid-sentence: what it was saying is cut off, and the next turn tells it so. */
  interrupt() {
    if (this.serverState !== "speaking" && !this.ducked) return;
    this.ducked = true;
    this.cut = true;
    this.audio.muted = true;
    this.send({ t: "interrupt" });
  }

  // A live turn's audio goes out as it comes; between turns the last few
  // frames are kept so a turn starts before the detector was sure.
  private liveFrame(speech: number, frame: Float32Array) {
    if (this.turnOpen) {
      this.ws?.send(pcm16(frame));
      this.quiet = speech < quietThreshold ? this.quiet + 1 : 0;
      return;
    }
    this.ring.push(frame.slice());
    if (this.ring.length > preRollFrames) this.ring.shift();
    // Still talking after the turn was ended early: that is the next turn.
    if (this.endedEarly && !this.snap.ptt && speech > 0.5) {
      this.endedEarly = false;
      this.openTurn();
    }
  }

  private openTurn() {
    if (this.snap.muted || this.turnOpen) return;
    this.turnN++;
    this.turnOpen = true;
    this.quiet = 0;
    this.send({ t: "speech", n: this.turnN });
    for (const f of this.ring) this.ws?.send(pcm16(f));
    this.ring = [];
    this.set({ hearing: "" });
  }

  private endTurn(eou: number) {
    this.turnOpen = false;
    this.send({ t: "turn", n: this.turnN, eou_ms: eou });
    this.set({ state: "thinking", heard: "", saying: "" });
  }

  private sendTurn(audio: Float32Array, eou: number) {
    if (audio.length < minTurnSamples || this.snap.muted) {
      this.set({ state: this.fromServer() });
      return;
    }
    this.send({ t: "turn", eou_ms: eou });
    this.ws?.send(encodeWAV(audio, 16000));
    this.set({ state: "thinking", heard: "", saying: "" });
  }

  private async onMessage(m: ServerMsg) {
    switch (m.t) {
      case "hello":
        this.live = m.stt === "live";
        this.set({ callId: m.call_id ?? "", chatId: m.chat_id ?? this.snap.chatId, state: "listening" });
        await this.connect(m.ice_servers ?? []);
        break;
      case "answer":
        await this.pc?.setRemoteDescription({ type: "answer", sdp: m.sdp });
        this.remoteSet = true;
        for (const c of this.pending.splice(0)) await this.pc?.addIceCandidate(c).catch(() => {});
        break;
      case "ice":
        if (!m.candidate) break;
        if (this.remoteSet) await this.pc?.addIceCandidate(m.candidate).catch(() => {});
        else this.pending.push(m.candidate);
        break;
      case "state":
        this.serverState = m.state ?? "listening";
        if (this.serverState !== "speaking") this.speechRun = 0;
        if (this.snap.state !== "hearing" || this.serverState === "speaking") this.set({ state: this.fromServer(), tool: m.tool ?? "" });
        else this.set({ tool: m.tool ?? "" });
        break;
      case "say":
        // A new sentence after an interruption is the answer to what was said.
        if (this.cut || this.ducked) {
          this.cut = false;
          this.ducked = false;
          this.audio.muted = false;
        }
        this.set({ saying: m.text ?? "" });
        break;
      case "heard":
        this.set({ heard: m.text ?? "", hearing: "", error: "" });
        break;
      case "caption":
        // Live words of the turn being heard (a late one for a closed turn is dropped).
        if (m.n === this.turnN) this.set({ hearing: m.text ?? "" });
        break;
      case "eot":
        // The transcriber judged the turn over: end it now if there is
        // silence already, instead of waiting out the detector.
        if (this.turnOpen && m.n === this.turnN && this.quiet >= eotQuietFrames && !this.snap.ptt) {
          this.endTurn(this.quiet * 32);
          this.endedEarly = true;
        }
        break;
      case "error":
        this.set({ error: m.message ?? "" });
        break;
      case "bye":
        if (m.message) this.set({ error: m.message });
        this.finish((m.reason as EndReason) ?? "hangup");
        break;
    }
  }

  private fromServer(): CallState {
    switch (this.serverState) {
      case "speaking":
      case "thinking":
      case "working":
        return this.serverState;
      default:
        return "listening";
    }
  }

  private async connect(iceServers: RTCIceServer[]) {
    const pc = new RTCPeerConnection({ iceServers });
    this.pc = pc;
    pc.addTransceiver("audio", { direction: "recvonly" });
    pc.ontrack = (e) => {
      const s = e.streams[0] ?? new MediaStream([e.track]);
      this.audio.srcObject = s;
      void this.audio.play().catch(() => {});
      if (this.ctx) {
        const an = this.ctx.createAnalyser();
        an.fftSize = 512;
        this.ctx.createMediaStreamSource(s).connect(an);
        this.botLevel = an;
        this.meter();
      }
    };
    pc.onicecandidate = (e) => {
      if (e.candidate) this.send({ t: "ice", candidate: e.candidate.toJSON() });
    };
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === "failed") this.fail(t("The call's audio could not connect. A firewall may be blocking its port."));
    };
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    this.send({ t: "offer", sdp: offer.sdp });
    // What the browser still holds when you start talking, so a pause resumes
    // where you actually stopped hearing.
    this.timers.push(
      window.setInterval(() => {
        void pc.getStats().then((stats) => {
          stats.forEach((r) => {
            if (r.type === "inbound-rtp" && r.kind === "audio") {
              const ms = playoutMs(r);
              if (ms > 0) this.send({ t: "playout", delay_ms: ms });
            }
          });
        });
      }, 3000),
    );
  }

  // The Bot's loudness while it speaks, for the orb.
  private meter() {
    const an = this.botLevel;
    if (!an) return;
    const buf = new Float32Array(an.fftSize);
    const tick = () => {
      if (this.snap.state === "ended") return;
      if (this.serverState === "speaking" && !this.ducked) {
        an.getFloatTimeDomainData(buf);
        this.level(loudness(buf));
      }
      this.raf = requestAnimationFrame(tick);
    };
    this.raf = requestAnimationFrame(tick);
  }

  private send(m: object) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(m));
  }

  /** Mutes your microphone: nothing you say is heard until you unmute. */
  setMuted(muted: boolean) {
    if (muted) void this.vad?.pause();
    else void this.vad?.start();
    this.set({ muted, level: 0, state: muted && this.snap.state === "hearing" ? this.fromServer() : this.snap.state });
  }

  /** Hold to talk instead of the voice detector: for a noisy room, or a speaker that leaks. */
  setPtt(ptt: boolean) {
    this.set({ ptt, holding: false });
    this.held = [];
  }

  /** The hold-to-talk press. */
  hold(down: boolean) {
    if (!this.snap.ptt || this.snap.muted) return;
    if (down) {
      this.held = [];
      if (this.serverState === "speaking") this.interrupt();
      this.set({ holding: true, state: "hearing" });
      if (this.live) {
        this.ring = [];
        this.openTurn();
      }
      return;
    }
    if (this.live) {
      this.set({ holding: false });
      if (this.turnOpen) this.endTurn(0);
      return;
    }
    const audio = joinFrames(this.held);
    this.held = [];
    this.set({ holding: false });
    this.sendTurn(audio, 0);
  }

  /** Ends the call. */
  hangup() {
    this.send({ t: "hangup" });
    this.finish("hangup");
  }

  private fail(error: string) {
    this.set({ error });
    this.finish("error");
  }

  private finish(reason: EndReason) {
    if (this.snap.state === "ended") return;
    this.set({ state: "ended", ended: reason, level: 0 });
    for (const id of this.timers) clearInterval(id);
    cancelAnimationFrame(this.raf);
    void this.vad?.destroy();
    for (const tr of this.stream?.getTracks() ?? []) tr.stop();
    this.pc?.close();
    this.audio.pause();
    this.audio.srcObject = null;
    void this.ctx?.close().catch(() => {});
    void this.wake?.release().catch(() => {});
    if (this.ws && this.ws.readyState <= WebSocket.OPEN) setTimeout(() => this.ws?.close(), 300);
  }
}

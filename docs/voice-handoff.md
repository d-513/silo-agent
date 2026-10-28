# Talk to the Bot: handoff

Status: phases 1a, 1b, 2 and 3 are built (see AGENTS.md, **Model kinds** and
**Calls**). Not done yet: phase 0's measurements, Smart Turn (turns end on
the voice detector's silence, 550 ms), the WebSocket audio fallback when
ICE finds no route, approvals and asks by voice (an approval is announced and
answered on screen), phases 4 and 5 (streaming STT and TTS), the Admin
P50/P95 panel over `voice_turns`, and phase 8. One change from the design
below: the browser sends each finished turn as a WAV on the control socket
instead of streaming the microphone over WebRTC; the Bot's voice still comes
back on the WebRTC track, which is what the echo canceller needs. Streaming
STT (phase 4) will send PCM frames on the same socket.

Read AGENTS.md first, then the research note this plan draws on ("Building
an Actually Good Voice Pipeline", Oct 2026).

Dictation stays exactly as it is: the composer mic records, `Transcribe` turns
the take into text, and the human edits it before sending. This plan adds a
second, separate feature: a **call**. You press **Talk**, speak, and the Bot
answers out loud, hands-free, until you hang up.

## What it is

A **call** is a live spoken conversation with one Bot, held inside one of its
chats (by default the main chat). Everything said is written into that chat as
text, so the call is part of the Bot's history like any message, and the
memory collector, compaction and the thread all keep working.

The design is the research's "fast front, smart back":

- A **talker** (a small, fast text model) holds the conversation. It answers
  small talk and anything already on the table, and says what it is about to
  do before handing off ("Let me check your calendar").
- The **Bot itself** (its normal run, model, tools, rules, approvals and output
  filter) does every piece of real work. The talker reaches it with one tool,
  `delegate`. What the Bot says back is spoken.
- **Speech in and out are Silo's own pipeline in Go**, with the provider behind
  an interface, like every other model in Silo.

What a call is not, in this plan:
- Not a phone line (SIP/Twilio) and not a Telegram/WhatsApp voice call. Phone
  is a later phase; voice *notes* on channels are a cheap side benefit of the
  speech-out layer (phase 8).
- Not a speech-to-speech model (OpenAI Realtime, Gemini Live). It would trade
  away transcripts, per-stage timing and vendor choice; a realtime model could
  later be offered as a talker (see the end).
- Not wake-word, always-listening. A call starts on a press.

Rules for the build:
- **The talker cannot act.** Its only tools are `delegate`, `cancel_task` and
  `end_call`. Every file, connector, secret or message goes through the Bot's
  run, so Rules, approvals and output rules apply unchanged.
- **A call is text in the chat.** Audio is never stored (no `voice.keep_audio`
  in this plan). The transcript is the record.
- **A Bot with voice off sees nothing new**: no Talk button, no prompt text, no
  tool.
- **Every vendor is a config line.** Defaults work with one OpenRouter key; a
  streaming STT or TTS vendor is an upgrade, not a requirement.

## Settled by the research, adopted here

1. Stream and overlap every stage; that beats any single faster model.
2. Turn-taking is a model, not a silence timer: VAD plus a semantic
   end-of-turn model, with an adaptive hold.
3. On barge-in, history holds only what the human actually heard.
4. Pick the front model for time-to-first-token; put intelligence behind an
   async delegate tool.
5. Measure P50/P95 per stage on every turn from the first working call.

Target: median end-of-speech to first audio under ~800 ms, P95 under ~1.5 s,
with the talker on. The direct mode (phase 2) will be slower; it is the
baseline the talker has to beat.

## Open decisions, with the recommended answer

1. **Our own Go pipeline, or Pipecat/LiveKit as a sidecar?** Own Go pipeline.
   The talker needs the Bot's context, the run engine, `Place`, approvals and
   the bus, all of which are Go in the CP. A Python sidecar would need a second
   copy of half of that over RPC, plus a new long-running service class. The
   pieces that are hard and ML-shaped (echo cancellation, VAD, turn detection)
   run in the client (below), so the CP part is a state machine, two queues and
   provider adapters. We borrow the frameworks' lessons, not their code.
2. **Where do VAD and turn detection run?** In the client. The browser already
   gives echo cancellation (`getUserMedia({echoCancellation: true})`); Silero
   VAD (`@ricky0123/vad-web`, MIT) and Pipecat Smart Turn v3 (ONNX, BSD-2,
   ~8M params, needs only audio, 23 languages) run in onnxruntime-web. That
   puts endpointing next to the microphone (no network in the loop) and lets
   the client stop playback within 100 ms on barge-in. iOS does the same on
   device later. A provider's end-of-turn signal (Deepgram Flux, Soniox) is an
   extra input, never the only one.
   **The same two models also run in the CP**, now that it builds with cgo
   (`yalue/onnxruntime_go` over the onnxruntime shared library): for a client
   with no compute of its own (a phone line in phase 8), and as a check that a
   client's `turn_end` is plausible. The browser stays first because a network
   hop in endpointing costs exactly the latency we are trying to save.
3. **Transport: WebRTC for the audio, a WebSocket for everything else.** See
   **Transport** below for the reasoning. In short: Chromium cancels echo
   reliably only for audio it receives on an `RTCPeerConnection`, not for audio
   played through Web Audio. On a phone network, UDP with Opus, a jitter buffer
   and loss concealment beats PCM over TCP, where one lost packet stalls
   everything behind it. And Opus upstream is ~30 kbps instead of 256 kbps.
   The media peer is `pion/webrtc` inside the CP (pure Go). `/voice`, a
   WebSocket like `/console`, carries the signalling and the control protocol,
   and it is also the fallback media path when no UDP or ICE-TCP route exists.
4. **Which chat does a call live in?** The one you are in; the sidebar's Talk
   goes to your main chat. Not a new kind of chat. On `/run/new` the first
   utterance makes the chat, like the first `Send` with `new_chat`.
5. **Talker or direct?** Both, as `voice.mode`. Direct (every turn goes to the
   Bot's run) ships first because it is all existing machinery and gives the
   baseline; talker becomes the default once it lands and `model_voice` is set.
   Direct stays for operators who would rather wait than run a second model.
6. **Approvals by voice?** Yes, Allow once only, behind an operator switch
   (`voice.approve_by_voice`, default on), with the slip still on screen. The
   CP speaks a fixed sentence, never the talker (see Security).
7. **Default profile?** OpenRouter only: `transcribe_model` on each turn,
   talker via OpenRouter with latency routing, TTS via OpenRouter
   `/audio/speech`. One key, matches today's setup. No model ids are baked
   into the code as defaults: the operator sets `model_voice` and
   `speech_model`, and phase 0's numbers are what the docs suggest. Polish is
   out of scope for now; **Adding a speech provider** below shows how any of
   the Polish candidates plugs in.
8. **Is the talker model on the `models` allowlist?** No. It is a role like
   `model_title`: `model_voice`, text kind only (`admin.modelRoles`). Chats and
   `switch_model` never see it.
9. **Several people on one Bot.** A call is its author's, in a chat they may
   write in (`access.Chat` Write). A manager can read a call's transcript in
   someone else's chat (as for any chat) but cannot call into it. One call per
   person per Bot at a time.

## The shape

```
 Browser                                   Control Plane (Go)
 ───────                                   ──────────────────
 mic ─ AEC (getUserMedia) ─┬─ Opus track ══ WebRTC (UDP) ══▶ pion peer ─ voice.Call (one per call)
                           │                                    │
                           └─ worklet 16 kHz ─ Silero VAD       │  STT: transcribe_model per turn (phase 2)
                                             ─ Smart Turn       │       or a streaming session (phase 4)
                  speech_start / turn_end ── /voice WS ───────▶ │
                                                                ├─ talker (model_voice, streaming, 3 tools)
                                                                │   │ text ─▶ chunker ─▶ normalizer ─▶ TTS ─┐
                                                                │   └ delegate ─▶ run.Engine (Bot's run)    │
                                                                │      Origin.Voice: sections ─▶ TTS ───────┤
 speaker ◀─ remote track ◀══ WebRTC ══ Opus RTP, paced 20 ms ◀── pacer ◀──────────────────────────────────┘
            (muted locally on speech onset; the WS carries say/flush/duck)
```

### Transport

**Why WebRTC for media.** A first draft of this plan used one WebSocket for
everything, and that is the weaker choice:
- **Echo cancellation.** Chromium's canceller reliably uses as its reference
  only the audio that arrives on an `RTCPeerConnection`. Audio played through
  Web Audio is often not cancelled. Chrome-wide AEC exists but is a feature-flag
  default, not a guarantee (Edge, for one, may not ship it). Without
  cancellation the Bot hears itself and barge-in breaks. The usual workaround
  is to loop Web Audio through a local peer connection, which is WebRTC anyway.
  Firefox and Safari cancel more broadly, but Chrome is the browser that has to
  work.
- **The network.** On a phone, TCP turns one lost packet into a stall of
  everything behind it, which shows up in P95. RTP over UDP with Opus FEC,
  packet-loss concealment and the browser's adaptive jitter buffer is built for
  exactly this.
- **Bandwidth.** About 30 kbps of Opus each way, against 256 kbps of PCM up.
- **It is what the ecosystem tests.** OpenAI Realtime, LiveKit and Pipecat's
  SmallWebRTC all reach the browser this way, and the iOS client later gets
  the same stack (or AVAudioEngine voice processing) for free.

**Why still a WebSocket.** Signalling (SDP offer and answer, trickled ICE),
the JSON control protocol and the turn signals ride `/voice`. That way the
access checks, `socketOrigin`, `access.Watch`/`App.Gone` and the reconnect
logic stay where `/console` already has them, and nothing about who may talk
depends on a DataChannel. When ICE finds no route within ~3 s (UDP blocked, a
proxy that only passes HTTP), media falls back to the same socket: Opus frames
from WebCodecs `AudioEncoder`, played through the loopback trick so echo
cancellation still holds. The fallback is slower but never absent.

**Running it in the CP.**
- `pion/webrtc`, one `PeerConnection` per call, audio only, Opus, no SFU (a
  call is one person and one Bot).
- One UDP port for every call (`voice.webrtc.udp_port`, pion's `ICEUDPMux`),
  plus an ICE-TCP port as the second route (`ICETCPMux`). Neither can go
  through the HTTP reverse proxy, so the operator publishes them: one more line
  in the compose file and in CONFIGURATION.md.
- `voice.webrtc.public_ips` (pion `SetNAT1To1IPs`) for a CP behind NAT, and
  optional `voice.webrtc.ice_servers` (STUN/TURN, credentials as secrets) for
  clients on networks that need a relay. TURN is optional, not required.
- **Opus.** Most of the path never decodes. Upstream, the call can wrap the
  RTP Opus in Ogg (pion's `oggwriter`) for batch STT, which every
  OpenAI-compatible transcription endpoint accepts, and vendors that take Opus
  get it as is. Downstream, a TTS that answers Opus/Ogg is repacketized with
  `oggreader` and sent without re-encoding. What needs a codec is a provider
  that only speaks PCM (OpenAI Realtime transcription, some TTS, the CP-side
  VAD and Smart Turn): that is **libopus through cgo** (`internal/speech/opus`),
  the reference codec, not a port. Both libopus and onnxruntime are **opened
  at run time** (`dlopen`, as `onnxruntime_go` already does), not linked, so
  `bin/silo` starts on a host without them and only the features that need
  them say so. A dev-shell package here, a documented optional dependency for
  operators.
- **Pacing and heard text.** The CP sends RTP at real time (a 20 ms ticker)
  instead of bursting, so at any moment it knows the playout position: what it
  has sent, minus the client's playout delay (from `getStats`
  `jitterBufferDelay`, reported on the socket every few seconds). Heard text
  comes from that and each chunk's text and duration, at word granularity, or
  exactly with word timestamps.
- **Barge-in over WebRTC.** On speech onset the client mutes the remote track
  at once (no round trip) and sends `duck`. The pacer stops and *rewinds by the
  playout delay*, so a false barge-in resumes from the first sample that was
  muted rather than losing it. A confirmed barge-in is `flush`.

### Where the code lives

- `internal/llm`: two new optional interfaces next to `Transcriber`.
  - `Speaker`: `Speak(ctx, model, SpeechRequest) (AudioStream, error)`, the
    OpenAI `/audio/speech` shape that OpenAI, OpenRouter, LocalAI, Speaches,
    Kokoro-FastAPI and vLLM-omni speak; implemented once in `openaicompat.go`
    with the body streamed (`response_format: pcm` at 24 kHz where offered, else
    opus/mp3 passed through). Whole text in, one request per sentence.
  - `StreamTranscriber` (phase 4): `OpenTranscription(ctx, model, opts)
    (TranscriptionSession, error)` with `Send(pcm)`, partial/final/end-of-turn
    events and `Close`. One adapter for the OpenAI Realtime transcription
    protocol (OpenAI, LocalAI, Speaches), then vendor ones where a socket buys
    real latency.
  - Later a `SpeechSession` for vendors that take streamed text in (phase 5).
  - **Capabilities as data** on `Descriptor`: kinds gain `transcription` and
    `speech`, plus streaming in/out, word timestamps, sample rates and
    languages, so the pipeline adapts (sentence-chunk only when the TTS cannot
    take streamed text) and Admin lists the right models.
  - The dummy provider becomes a `Speaker` (deterministic PCM, a fixed number of
    samples per rune, `Test_TTS_Error`) and its transcriber gains a scripted
    streaming session.
- `internal/speech` (new, pure, no I/O): PCM framing and resampling, the WAV
  wrapper for batch STT, the **sentence chunker** (rune-aware; the first chunk
  may cut at a clause to get audio out sooner; knows `np.`, `itd.`, `z.B.`,
  `e.g.`, decimals, times and URLs), the **normalizer** (markdown stripped,
  a code block or table becomes a fixed translated "I've put it in the chat",
  a URL becomes its domain, numbers/dates/currency spelled per language) and
  **heard-text truncation** (chunks played plus the played share of the current
  one, cut at a word; exact when the TTS gave word timestamps).
- `internal/app/voice` grows from dictation into the voice domain: the `Call`
  service (sessions, the talker loop, the TTS queue, metrics), the `/voice`
  handler, the RPCs, and the `delegate` plumbing through `run.Engine`. It keeps
  the `Host` interface pattern and never imports package `app`.
- `internal/prompts`: `VOICE.md` (the conversation note the Bot's run gets
  while it is being spoken, like `CHANNEL.md`) and `VOICE_TALKER.md`.
- `web/src/voice/` (new folder; today's `voice.ts` dictation stays): the
  peer connection and its socket fallback, the capture worklet that feeds VAD
  and the turn detector, the control client and its state machine, the call
  bar.

### How a call maps onto what exists

The one hook that makes this cheap: **`run.Origin.Deliver`**. A channel run
already hands every finished `<section_send />` block, and the tail, to a
`Deliver` func. A call adds `Origin.Voice` (the call) whose `Deliver` puts the
text on the call's TTS queue. So the Bot's run needs no new code path to be
spoken; `VOICE.md` only asks it, as `CHANNEL.md` does, to send short sections
early, plain sentences, key facts first, no markdown, and to leave long output
(code, tables, lists of links) in the chat with a one-line spoken pointer.

**Events written into the chat:**
- `call_start` / `call_end` (visible dividers: "Call · 6 min"); replayed as
  nothing, except that the user turns inside are stamped as spoken.
- Each final utterance: a `user` event, `From = "voice"` (`run.FromVoice`),
  body = the transcript. Replay stamps it `(spoken)` so the model knows a
  transcript can be misheard.
- Each talker reply: a `section` event with `tool="voice"`, body = **what was
  heard**; `Meta.unheard` holds the rest after a barge-in (drawn faint in the
  thread, never replayed).
- The Bot's own sections are its normal `section` events. If the human cut one
  off, the next thing the Bot reads carries a short note ("You were
  interrupted after: '…'") instead of rewriting a persisted event.

**Keeping context append-only** (`TestHistoryIsAppendOnly` gets a call
scenario). The talker answering while a Bot run is live is the one new hazard:
written straight into the chat by `seq`, its exchanges would land between the
run's turns and the next replay would differ from the live history. So:
- While **no** run is live in the chat: a delegation starts one through
  `run.Engine.Place` with the utterance as the opening `user` event; an
  exchange the talker answered itself is written in a run of its own with
  origin `voice` (like `quote.Put`).
- While a run **is** live: a delegation is injected (`StartOrInject`, the
  existing inbox, re-sequenced at drain); an exchange the talker answered is
  injected as one framed context turn ("On the call meanwhile, already
  answered: …"), so the Bot sees it in order and does not answer it again.
- All of it under the conversation lock `Place` already holds.

## The talker

- **Model**: `model_voice` (text). On OpenRouter its requests carry
  `provider.sort: "latency"` (a new `llm.Request.Latency` flag the OpenRouter
  provider maps, merged with `providers.openrouter.ignore`; never `:nitro`,
  which sorts by throughput). Thinking off, or the lowest level the model
  takes. **No default model**: the role is empty until the operator sets it,
  and while it is empty a call runs in direct mode. Phase 0 measures current
  models for time-to-first-token *and* the delegation eval (a fast model that
  answers what it should delegate is worse than a slow one). The research's
  benchmark rows are out of date and are not candidates.
- **Prompt** (`VOICE_TALKER.md`, stable → volatile with cache breakpoints, the
  same discipline as the main prompt): who it is the voice of (the Bot's name,
  a short SOUL, the Bot's language), what the Bot can do in one line per
  connector/capability (names only), the memory digest, the chat's last few
  turns as plain text, then the call so far, appended turn by turn so the
  provider's cache holds.
- **Rules it is given** (the failure modes the research names):
  - Never answer anything about the human's data, the world's facts it is not
    sure of, or an action: delegate. Guessing and then being contradicted by
    the Bot is the worst failure.
  - Say a short preamble in the same turn as the `delegate` call.
  - Do not repeat a task that is already running (open tasks are in its
    context); offer to cancel when the human changes their mind.
  - Plain spoken sentences only.
- **Tools**: `delegate(task, context)` → a Bot run (above), returns a task id
  at once; `cancel_task(id)` → `StopRun` on that run; `end_call()`.
- **Delivery is code's job, not the talker's.** The Bot's sections go straight
  to the TTS queue at the next free floor (never over the human), so a fast
  model cannot decide a late result was "already covered". The talker's
  context gets each one as "[the Bot]: …" so follow-ups make sense. A result
  that arrives two or more human turns after its question is opened with a
  fixed translated "About <task>: ".
- **No dead air, no chatter.** While a task runs with nothing to say, the call
  bar shows its step (`Bot.activity` / `liveRun.step`). After ~4 s the client
  plays a soft working earcon; after ~10 s and then at most every ~15 s the CP
  speaks one honest status line from a fixed translated table keyed by the
  tool ("Searching the web.", "Reading your mail."), never written by a model.

Direct mode (`voice.mode: direct`, or no `model_voice`) skips the talker:
every turn is `StartOrInject` into the chat, and only the Bot speaks.

## Turn-taking, barge-in and heard text

Client side, per frame:
1. **Silero VAD** decides speech vs silence (stop time ~200 ms, 300 ms
   pre-roll kept so the first syllable is not lost).
2. On each pause, **Smart Turn** scores the last ≤8 s of audio. Complete →
   `turn_end` at once. Incomplete → hold up to ~2.5 s more, then end anyway.
   A provider EOT, once there is one, can end the turn early.
3. **Push-to-talk** (hold Space or the button) replaces both, for noisy rooms
   and for when echo cancellation lets the Bot hear itself.

Barge-in:
- Speech onset while the Bot is speaking **mutes the remote track locally at
  once** and sends `duck`; the pacer stops and rewinds by the playout delay
  (see Transport).
- It becomes a real interruption after ~300 ms of speech or the first STT
  words; then the CP flushes the TTS queue, cancels the talker's in-flight
  turn, and keeps listening (`interrupt`, then `flush`). Shorter ("mm-hm", a
  cough) and the client sends `unduck`, so playback resumes from the first
  muted sample; the turn is logged as a false barge-in.
- Heard text is computed on the CP from the pacer's position, the reported
  playout delay and the chunks' text and durations, and committed as above.
- A barge-in **does not stop the Bot's run**; what the human says next is
  injected into it. Stopping work is the talker's `cancel_task` ("never mind")
  or the call bar's Stop.

## Wire protocol (`/voice`, v1)

Opened as `/voice?bot_id=…&chat_id=…` with the session cookie. The handler
checks `socketOrigin`, `access.Chat(…, Write)`, a suspended Bot, a disabled
owner and voice being on, then registers with `access.Watch` so `App.Gone`
closes the socket *and* the peer connection. Text frames are JSON; binary
frames are audio and only appear on the fallback path.

Setup:
- `hello{v, lang, mode: vad|ptt, offer}` (the client's SDP offer, audio only)
  first; the CP answers `ready{call_id, answer, caps}`, then both sides trickle
  `ice{candidate}`. With no connected route after ~3 s the client sends
  `fallback` and media moves onto this socket.

Media:
- **WebRTC (normal).** Up, the microphone track as Opus. It stays on with DTX
  (silence is nearly free). The CP keeps the last ~500 ms in a ring buffer and
  forwards audio to STT only between `speech_start` (plus that pre-roll) and
  `turn_end`, because STT is billed by audio time. Down, one Opus track paced
  at real time.
- **Fallback (binary frames).** 1 type byte + 4-byte chunk id + one Opus frame
  from WebCodecs, both ways.

Up (JSON):
- `speech_start`, `turn_end{eou_ms, p}`, `turn_resume`.
- `duck` (speech onset while the Bot speaks; the client has already muted),
  `unduck` (it was a backchannel), `interrupt` (confirmed barge-in).
- `playout{delay_ms}` every few seconds from `getStats`.
- `typed{text}` (the composer during a call adds a typed turn), `mute{on}`,
  `hangup`.

Down (JSON):
- `say{chunk, text, ms}` as each chunk starts playing out (by the pacer's
  clock), for captions and so the CP can compute heard text.
- `state{listening|thinking|speaking|working}`, `caption{partial|final,
  text}`, `flush`, `task{id, state, step, title}`, `approval{id, sentence}`,
  `bye{reason}` (hung up, idle, max length, Bot suspended, access ended).

## Configuration

silo.yaml, under `voice`, edited in a new **Voice** category in Admin →
Settings (`web/src/admin/sections.ts`):

```yaml
voice:
  enabled: true              # the operator's switch; off hides Talk everywhere
  mode: talker               # talker | direct (talker needs model_voice)
  speech_model: ""           # picked from speech_models (see Model kinds)
  voices:                    # default voice per speech model and language
    "<provider/model>": {en: …, de: …}
  stream_transcribe_model: "" # optional llm.StreamTranscriber; empty = transcribe_model per turn
  max_minutes: 30            # one call
  idle_minutes: 3            # no speech → hang up
  approve_by_voice: true
  webrtc:
    udp_port: 3479           # one UDP port for every call (ICEUDPMux)
    tcp_port: 3479           # ICE-TCP, the second route (ICETCPMux)
    public_ips: []           # the CP's public address when behind NAT
    ice_servers: []          # optional STUN/TURN for the client; credentials are secrets
model_voice: ""              # the talker; text kind (admin.modelRoles); empty = direct mode
speech_models: []            # the speech allowlist; transcription_models likewise
```

`transcribe_model: off` or no `speech_model` means no calls. Per Bot:
`bots.voice` (a voice id for the current `speech_model`, default from
`voices` and the Bot's language) and `bots.speech_rate`, in Settings → Voice
with a **Hear it** preview. A change of `speech_model` drops back to the new
model's defaults, because voice ids mean nothing across providers.
`ListModels.voice_enabled` stays dictation; `call_enabled` is new.

## Model kinds are modular

Today a model is one of two kinds, `text` and `decision`, and that idea is
already most of the way to modular: `Descriptor.Kinds` says what a provider
serves, `ModelInfo.Kind` labels a listing, each kind has its own allowlist
(`models`, `decision_models`), `Config.ModelKind` answers what an id is, and
`admin.modelRoles` says which kinds each role may take. Speech-to-text and
embeddings sit outside it, as free-text settings (`transcribe_model`,
`embedding_model`) checked only against an interface. Voice adds a third
outsider (`speech_model`), so this plan first makes kinds a registry and puts
all of them on it. Any provider, the universal ones included (`openrouter`,
`openai`, `local`), can then serve any mix of kinds, exactly as `openrouter`
serves text and decision today.

**The registry** (`internal/llm/kinds.go`). One row per kind:

| Kind | Interface | Allowlist (silo.yaml) | Roles that pick from it |
| --- | --- | --- | --- |
| `text` | `Client` | `models` | `model`, `model_title`, `model_subagent`, `model_memory`, `model_voice` |
| `decision` | `Decider` | `decision_models` | `model_decision` |
| `transcription` | `Transcriber` (+ `StreamTranscriber` as a capability) | `transcription_models` | `transcribe_model`, `voice.stream_transcribe_model` |
| `speech` | `Speaker` (+ `SpeechSession` as a capability) | `speech_models` | `voice.speech_model` |
| `embedding` | `Embedder` | `embedding_models` | `embedding_model` |

`model_approval` and `model_filter` keep taking text or decision. A row also
carries its label for Admin and how a listing recognises it. Adding a kind
later (a realtime speech-to-speech model, an image model) is one row, one
interface and one allowlist key, and every loop below picks it up.

**What becomes generic, replacing today's pairs of hand-written cases:**
- `validateModels` loops over the registry: every id on a list parses, its
  provider serves that kind, and no id is on two lists. The role check loops
  over `admin.modelRoles`.
- `Config.ModelKind(id)` is the list the id is on, else the provider's only
  kind.
- `models.Service` hands out a typed client per kind (`Speaker(id)`,
  `Transcriber(id)`, …) through the existing `models.Can[T]`, and every call
  is recorded in the LLM log with its kind.
- `TestProviderKinds` replaces `TestDecisionProviders`: for every registered
  provider, `Descriptor.Kinds` and the interfaces the client implements agree.
- The proto's `kind` fields (`ModelOption`, `Provider.kinds`,
  `ProviderModel.kind`, `SetModels.kind`) already are strings: they gain
  values, not shape.
- Admin → Settings → Models gets an **Allowed … models** list per kind (the
  existing `AllowedModels kind=…`), and each role is a picker over its kind's
  list instead of free text. A provider's **List models** shows one tab per
  kind it serves.

**Listing.** A provider labels what it can:
- OpenRouter: one extra `/models` query per output modality, as the
  decisions query does today. The exact query values for transcription and
  speech models get checked against the live API, the way `decisions` was.
- OpenAI: `/models` gives ids only, so known families are labelled (whisper
  and `*-transcribe` as transcription, `tts-*` and `*-tts` as speech,
  `text-embedding-*` as embedding).
- `local` (LocalAI, Speaches, vLLM): usually no labels. The listing shows
  them unlabelled and the admin chooses the kind with the tab they add it
  from. The provider's validation still holds, because `local` declares every
  kind its OpenAI-compatible client implements.

A model's kind is in the end the operator's word on an allowlist; a listing
is only a hint.

**Moving the existing settings.** No migration is needed. `setDefaults` puts
the default `transcribe_model` and `embedding_model` on their new lists when
those lists are unset, and a role that names a model missing from its list is
added to the list on load (logged once). So every silo.yaml that works today
keeps working, and Admin shows the lists from the first start.

**Capabilities ride on the kind.** Beyond "serves speech", the pipeline needs
facts such as streaming text in, word timestamps, output formats, sample
rates, languages and voices. They are optional interfaces and fields on a
kind's row, not new kinds. A `Speaker` may also be a `VoiceLister`, and the
pipeline asks about capabilities, never about provider ids.

### Adding a speech provider

How much work a new TTS or STT takes depends only on the API it speaks:

| What it speaks | What adding it costs | Examples |
| --- | --- | --- |
| OpenAI `/audio/speech` or `/audio/transcriptions`, directly or through OpenRouter | **A config line.** The universal providers serve `speech` and `transcription` through their shared OpenAI-compatible client, so the model goes on `speech_models` or `transcription_models` (or `local` with a `base_url` for a self-hosted server) | Fish S2.1 and Voxtral TTS through OpenRouter; Kokoro, Qwen3-TTS and Parakeet through LocalAI, Speaches or vLLM-omni |
| The OpenAI Realtime transcription protocol (phase 4) | A config line once that adapter is on the shared client | OpenAI, LocalAI, Speaches |
| Its own HTTP or WebSocket API | **One provider file** in `internal/llm`: a descriptor in the built-in list whose `Kinds` name what it serves (like `typesafe.go`, which is decision-only), the interfaces for those kinds, and an `httptest` test. Nothing else changes | ElevenLabs (HTTP stream and a text-in WebSocket), Soniox (WebSocket STT and TTS), Deepgram, Cartesia, Inworld |

- **Voices are the provider's**: a `VoiceLister` lists them, with languages,
  for the Settings picker, and the id is stored as an opaque string. A
  provider without a list takes a typed voice id.
- **Language is a parameter, not a model choice**: `SpeechRequest.Language`
  and the transcriber's language hint come from the Bot's language. A
  provider that ignores them is fine, and one that needs them (Soniox, for
  code-switching) gets them.
- **A speech-only provider is normal**: an `elevenlabs` or `soniox` provider
  serves only `speech`/`transcription`, and its `Complete`/`Stream` answer an
  error, as `typesafe`'s do.

So the Polish candidates from the research fit as follows: Fish S2.1 and
Voxtral TTS are config lines, and ElevenLabs and Soniox are one provider file
each. Polish quality itself is not part of this plan, but nothing in the
plumbing stands in its way.

## Security

- **The talker cannot act** (three tools, none touching anything). Text that
  reaches it from outside comes only through the Bot's sections and the chat
  tail; the worst it can do is say something wrong or delegate a task, and a
  task is a Bot run under the Bot's Rules. Tool results reach the talker only
  as the Bot chose to summarise them, after the output filter.
- **Voice approvals** are the CP's, not the talker's: when a run of this call
  raises an approval, the call holds the talker and speaks a fixed translated
  sentence built from `security.Describe` of the redacted arguments ("Send an
  email to anna@… with subject 'Invoice'? Say yes to allow it once."). The next
  utterance is classified by a decision model's `noul` (or the talker with a
  one-word prompt when no decision model is allowed); only a sure yes
  (`approval.Sure`) answers **Allow once** through the same `DecideApproval`
  path, so `ManagerOnly`, the per-person rules and `approvals.auto` all hold.
  Anything else leaves the slip pending on screen. Never Always.
- A voice "yes" is exactly as strong as a click from that signed-in session,
  and a TV in the room can say yes. The Settings copy says so;
  `approve_by_voice: false` turns it off.
- **Access** is rechecked as on `/console`: origin, `access.Chat` Write, and the
  socket closes when the person leaves the Bot. `TestEveryRPCChecksAccess`
  covers the new RPCs; `feature_hardening_test.go` gets `/voice` next to
  `/console` and `/vnc`.
- **No audio stored.** Turn audio lives in memory until transcribed. The LLM
  debug log records `[audio pcm, N bytes, 2.4s]` as dictation already does.
- **Cost**: `max_minutes`, `idle_minutes`, VAD-gated upload, one call per person
  per Bot. Every STT, talker and TTS call is in the LLM log with its label
  (`voice-stt`, `voice-talk`, `voice-tts`).

## UI

- **Talk**: a button next to the dictation mic in the composer (calls in this
  chat) and a row under the Bot's name in `BotSidebar` (calls in your main
  chat). Shown only while `call_enabled`.
- **The call bar** docks above the composer: a level orb that shows both
  directions, the state word, the live caption, running tasks as chips (with
  their step and a cancel), Mute, push-to-talk toggle, End. The composer stays
  usable; what you type joins the call as a turn. Below 960 it is a bottom
  sheet with a large orb, and the page holds a screen wake lock.
- **The thread**: "Call started" and "Call · 6 min" dividers; spoken turns are
  normal bubbles with a small mic or speaker glyph; an unheard remainder is
  faint with a `Tip` ("Not heard: you interrupted").
- **Read aloud** on any Bot bubble (phase 1), using the same speech path.
- **Settings → Voice** per Bot: voice picker with Hear it, speech rate.
- Every string in `pl` and `de` (`i18n_test.ts`); tokens only, no colours in
  components; `Tip` for controls that need more than a word.

## Measurement

From the first working call, one `voice_turns` row per turn (not run events, so
replay stays clean): call, turn number, mode, `eou_ms` (client: last speech →
`turn_end`), `stt_ms`, `talk_ttft_ms`, `tts_ttfb_ms`, `first_audio_ms`
(client: `turn_end` → first sample played; with `eou_ms` that is
mouth-to-ear), delegated, interrupted, false barge-in, the models used.
Admin → Settings → Voice shows P50/P95 per stage over the last day and week,
read from this table. Kept 30 days.

What "good" means, tracked separately: latency (above); cut-offs (the Bot
spoke over an unfinished turn, i.e. the human resumed within 1 s of a
`turn_end`) vs waits (turns that hit the full hold); delegated results spoken
(should be 100 % by construction); turns the talker answered that it should
have delegated (from the eval suite); history after a barge-in matching what
was heard.

## Testing

- **Pure Go units**: the chunker and normalizer table-tested in en/pl/de
  (abbreviations, decimals, times, URLs, code fences, lists); heard-text
  truncation; resampling and WAV wrapping; OpenAI-compatible `/audio/speech`
  against an `httptest` server (streamed body, errors, key masked).
- **`feature_voice_test.go`**: an in-process client (a pion peer for the
  audio plus a WebSocket for control, with the socket fallback as a second
  run of the same cases) drives a call against the dummy provider: hello → frames → `turn_end` → the dummy
  transcript → talker reply heard as `say` + audio → events in the chat. Then
  delegation (a dummy Bot turn with `<section_send />` is spoken and lands in
  the talker's context), barge-in mid-chunk (heard text in the event,
  `Meta.unheard`, the note the Bot reads next), a voice approval (sure yes →
  allow once; unsure → still pending), access (stranger, guest, a manager in
  someone else's chat), idle and max-length hang-ups, and a call during a live
  run under `TestHistoryIsAppendOnly`.
- **Web (node)**: frame packing and resampling, the playback queue's position
  accounting, the call state machine, `insertDictation` untouched.
- **Live** (`-tags live`): a latency probe that runs one scripted turn per
  configured profile and prints P50/P95 per stage; `TestLiveTalker` runs a set
  of scripted conversations against real talker models and counts answered
  vs delegated (re-run after touching `VOICE_TALKER.md`).
- **Replay suite**: 20 to 50 recorded calls of the owner's own (pauses,
  mumbling, interruptions, Polish/English mixing) run offline through Smart
  Turn and the STT on every change to either.
- The browser parts that need a microphone are tested by hand; the Chromium
  MCP can check the UI states with a fake stream, not the audio path.

## Phases

Each phase is usable on its own and ends with its tests green.

**0. Spikes and numbers (no product code).**
- A pion peer in a throwaway Go program and a bare page: echo cancellation
  with the Bot's voice on the remote track (Chrome, Firefox, Safari, a phone),
  the WebSocket fallback with the loopback trick, and the UDP and ICE-TCP mux
  through the dev compose's published ports.
- libopus and onnxruntime through cgo on this host (NixOS: from a dev shell,
  since neither is installed system-wide today): decode and encode CPU per
  call, Silero + Smart Turn per inference in the CP. Which STT and TTS
  endpoints take or give Ogg Opus as is.
- Silero + Smart Turn in onnxruntime-web: CPU per inference on a mid laptop and
  a phone, WASM vs WebGPU, download size.
- From the operator's region (EU): talker TTFT P50/P95 via OpenRouter with
  latency routing for current fast models, each also run through the
  delegation eval; TTS time to first byte per sentence for what OpenRouter
  offers; batch STT for a 3 s turn.
- Output: a table appended here and the profile the docs suggest. Nothing is
  hard-coded from it.

**1a. Model kinds as a registry (no behaviour change).**
`internal/llm/kinds.go`; `validateModels`, `ModelKind`, `modelRoles` and the
Admin lists made generic over it; `transcription` and `embedding` moved onto
it with their allowlists seeded from today's settings; `TestProviderKinds`.
Every existing config and test passes unchanged; that is the gate.

**1b. Speech out.**
The `speech` kind: `llm.Speaker` on the shared OpenAI-compatible client (so
`openrouter`, `openai` and `local` serve it) + dummy; `speech_models`,
`voice.speech_model` and `voices`; `internal/speech` chunker and normalizer;
`bots.voice`/`speech_rate` and Settings → Voice with Hear it; **Read aloud** on
Bot bubbles through a plain streaming route (`/voice/speak?bot_id&event_id`,
cookie auth, `access.Chat` Read).

**2. The call, direct mode.**
The `/voice` socket and protocol v1 with the pion peer and the socket
fallback, the Opus handling chosen in phase 0, the published UDP/TCP ports in
the compose file and CONFIGURATION.md, the `calls` table, `voice.Call`, the
pacer, the client capture/VAD/Smart Turn/playback, batch STT per turn, `Origin.Voice`
with `Deliver` → TTS, `VOICE.md`, the events and their thread rendering,
barge-in with heard text, push-to-talk, `voice_turns`, the call bar and Talk
buttons. Gate: a full call works on the OpenRouter-only profile, and the
baseline numbers are written down here.

**3. The talker.**
`model_voice`, `voice.mode`, `llm.Request.Latency`, `VOICE_TALKER.md` and its
context, `delegate`/`cancel_task`/`end_call`, forced delivery with the
staleness prefix, status lines and earcon, the append-only rule for exchanges
during a live run, `TestLiveTalker`. Gate: P50 under ~800 ms on the default
profile, or the numbers that say which layer is in the way.

**4. Streaming STT and speculative start.**
`llm.StreamTranscriber`; the OpenAI Realtime transcription adapter, then one
vendor (Soniox for code-switching, or Deepgram Flux for its end-of-turn;
phase 0 decides). Live captions from partials, provider EOT as a
second signal, and a speculative talker start on an eager end-of-turn,
cancelled silently if the human goes on.

**5. Streaming TTS.**
A `SpeechSession` for one vendor with streamed text in over a persistent
socket per call (ElevenLabs Flash, Inworld or Cartesia), word timestamps for
exact heard text, the capability flags that make the pipeline skip sentence
chunking for it.

**6. Asks and approvals by voice.**
Voice approvals as above; `ask_owner` cards read out with their choices and
answered by voice (choice questions through the decision model or the talker;
free-form fields stay on screen); a spoken read-back before side effects the
Bot's own prompt marks as needing confirmation.

**7. Hardening.**
A fallback chain per layer (primary, secondary, local) switched on error,
stall or a breached latency budget; an STT liveness watchdog; reconnect that
resumes the call (same `call_id`) after a network blip; the Admin P50/P95
panel; the replay suite in CI where the models allow it.

**8. Later.**
iOS (AVAudioEngine voice processing for echo cancellation, the same protocol,
Smart Turn through Core ML); voice notes on channels (inbound Telegram and
WhatsApp voice notes transcribed into the message; replies optionally sent as
voice notes through `Speaker`); a phone line (Twilio Media Streams or SIP over
pion, mu-law 8 kHz end to end); local wake word; a realtime speech-to-speech
model offered as the talker for operators who want its prosody, with the Bot
still behind `delegate`.

## Risks to watch

- **Networks with no UDP.** ICE-TCP and the socket fallback cover them, more
  slowly; a TURN server is the operator's option. Push-to-talk is the floor
  when echo cancellation fails on some device.
- **An operator who cannot publish a UDP port** (some PaaS hosts) gets the
  fallback path only. CONFIGURATION.md says what that costs.
- **Native libraries on the operator's host.** cgo makes libopus and
  onnxruntime optional runtime dependencies of `bin/silo` (opened at run
  time, never linked). Without them the CP still starts: the call works on the Ogg passthrough path with client-side
  turn detection, and the Voice settings say which providers need the codec.
- **Batch STT on long turns** adds the turn's length to the latency; that is
  the research's "biggest single upgrade" and why phase 4 exists.
- **The talker answering what it should delegate.** Prompt, eval suite and the
  forced-delivery design are the defence; measure it, do not assume it.
- **Client CPU** for Smart Turn on older phones; fall back to VAD with a longer
  hold if an inference takes over ~150 ms.
- **Sections the Bot does not send.** The tail at run end is spoken as it is
  for channels, but a run that writes one long block will be slow to start
  speaking; `VOICE.md` and the eval suite must push for early short sections.

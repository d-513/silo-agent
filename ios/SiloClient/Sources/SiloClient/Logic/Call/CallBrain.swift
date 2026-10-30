import Foundation

// A call's logic, with no audio and no network in it: what a frame of the microphone, a press, or a
// message from the control plane means, and what to do about it. Port of `CallSession` in
// `web/src/call/session.ts`; keep the two in step. `CallSession` (SiloCall) owns the socket, the
// peer connection and the audio engine and calls in here from one serial queue.

public struct ICEServer: Decodable, Equatable, Sendable {
    public var urls: [String]
    public var username: String?
    public var credential: String?
}

public struct ICECandidate: Codable, Equatable, Sendable {
    public var candidate: String
    public var sdpMid: String?
    public var sdpMLineIndex: Int32?

    public init(candidate: String, sdpMid: String?, sdpMLineIndex: Int32?) {
        self.candidate = candidate
        self.sdpMid = sdpMid
        self.sdpMLineIndex = sdpMLineIndex
    }
}

/// A message from the control plane.
public struct CallMessage: Decodable, Sendable {
    public var t: String
    public var call_id: String?
    public var chat_id: String?
    public var sdp: String?
    public var candidate: ICECandidate?
    public var ice_servers: [ICEServer]?
    public var stt: String?
    public var n: Int?
    public var state: String?
    public var tool: String?
    public var text: String?
    public var reason: String?
    public var message: String?
}

/// What the brain needs done outside itself.
public protocol CallBrainIO: AnyObject {
    func sendText(_ object: [String: Any])
    func sendBinary(_ data: Data)
    /// Mutes the Bot's voice here and now, with no round trip.
    func muteVoice(_ muted: Bool)
    /// `hello` came: make the peer and offer it.
    func connectPeer(iceServers: [ICEServer])
    func setAnswer(sdp: String)
    func addCandidate(_ candidate: ICECandidate)
    func changed(_ snapshot: CallSnapshot)
    func finished(_ reason: EndReason)
}

/// Turns shorter than this are a cough, not a turn.
private let minTurnSamples = 16000 / 4
/// Live hearing: the frames kept from before speech starts (~320 ms), and how much silence lets
/// the transcriber's end of turn end the turn early.
private let preRollFrames = 10
private let eotQuietFrames = 3
private let quietThreshold: Float = 0.35

public final class CallBrain {
    public private(set) var snap: CallSnapshot
    private weak var io: CallBrainIO?
    private var gate = SpeechGate()
    private lazy var barge = BargeIn(
        tell: { [weak self] in self?.io?.sendText(["t": $0]) },
        mute: { [weak self] in self?.io?.muteVoice($0) }
    )
    private var serverState = "listening"
    private var held: [[Float]] = []
    private var lastEmit = Date.distantPast
    // Live hearing (hello's stt is "live"): turns stream as they are spoken.
    private var live = false
    private var turnN = 0
    private var turnOpen = false
    private var ring: [[Float]] = []
    private var quiet = 0
    /// A turn the transcriber ended early; speech that goes on opens the next.
    private var endedEarly = false

    public init(chat: String, io: CallBrainIO) {
        snap = CallSnapshot(chatID: chat)
        self.io = io
    }

    private func set(_ patch: (inout CallSnapshot) -> Void) {
        patch(&snap)
        io?.changed(snap)
    }

    public var isSpeakingBot: Bool { serverState == "speaking" && !barge.ducked }

    // MARK: microphone

    /// One 32 ms frame with the detector's probability that it is speech.
    public func frame(_ frame: [Float], probability: Float) {
        guard snap.state != .ended else { return }
        if !snap.muted {
            onFrame(frame, speech: probability)
            for event in gate.process(frame, probability: probability) { handle(event) }
        }
    }

    private func onFrame(_ frame: [Float], speech: Float) {
        if snap.holding && !live { held.append(frame) }
        if live { liveFrame(speech, frame) }
        barge.frame(speech: speech, detect: !snap.ptt)
        if serverState != "speaking" || barge.ducked { level(loudness(frame)) }
    }

    private func handle(_ event: SpeechEvent) {
        switch event {
        case .start:
            if snap.ptt { return }
            set { $0.state = .hearing }
            if live { openTurn() }
        case .realStart:
            if !snap.ptt { barge.sure() }
        case .misfire:
            guard snap.state != .ended else { return }
            if !snap.ptt { barge.ended() }
            if live && !snap.ptt && turnOpen {
                turnOpen = false
                io?.sendText(["t": "cancel", "n": turnN])
            }
            endedEarly = false
            if !snap.ptt && snap.state == .hearing { set { $0.state = fromServer() } }
        case .end(let audio):
            if snap.ptt || snap.state == .ended { return }
            barge.ended()
            if live {
                if turnOpen { endTurn(SpeechGate.redemptionMs) }
                endedEarly = false
                return
            }
            sendTurn(audio, eou: SpeechGate.redemptionMs)
        }
    }

    /// The meter: only moves when it has something to say.
    public func level(_ value: Float) {
        let now = Date()
        if now.timeIntervalSince(lastEmit) < 0.06 && abs(value - snap.level) < 0.15 { return }
        lastEmit = now
        set { $0.level = value }
    }

    /// The Bot's loudness while it speaks, for the orb.
    public func botLevel(_ value: Float) {
        if isSpeakingBot { level(value) }
    }

    // A live turn's audio goes out as it comes; between turns the last few frames are kept so a
    // turn starts before the detector was sure.
    private func liveFrame(_ speech: Float, _ frame: [Float]) {
        if turnOpen {
            io?.sendBinary(pcm16(frame))
            quiet = speech < quietThreshold ? quiet + 1 : 0
            return
        }
        ring.append(frame)
        if ring.count > preRollFrames { ring.removeFirst() }
        // Still talking after the turn was ended early: that is the next turn.
        if endedEarly && !snap.ptt && speech > 0.5 {
            endedEarly = false
            openTurn()
        }
    }

    private func openTurn() {
        if snap.muted || turnOpen { return }
        turnN += 1
        turnOpen = true
        quiet = 0
        io?.sendText(["t": "speech", "n": turnN])
        for frame in ring { io?.sendBinary(pcm16(frame)) }
        ring = []
        set { $0.hearing = "" }
    }

    private func endTurn(_ eou: Int) {
        turnOpen = false
        io?.sendText(["t": "turn", "n": turnN, "eou_ms": eou])
        set {
            $0.state = .thinking
            $0.heard = ""
            $0.saying = ""
        }
    }

    private func sendTurn(_ audio: [Float], eou: Int) {
        if audio.count < minTurnSamples || snap.muted {
            set { $0.state = fromServer() }
            return
        }
        io?.sendText(["t": "turn", "eou_ms": eou])
        io?.sendBinary(encodeWAV(audio, rate: 16000))
        set {
            $0.state = .thinking
            $0.heard = ""
            $0.saying = ""
        }
    }

    // MARK: the control plane

    public func receive(_ m: CallMessage) {
        guard snap.state != .ended else { return }
        switch m.t {
        case "hello":
            live = m.stt == "live"
            set {
                $0.callID = m.call_id ?? ""
                $0.chatID = m.chat_id ?? $0.chatID
                $0.state = .listening
            }
            io?.connectPeer(iceServers: m.ice_servers ?? [])
        case "answer":
            if let sdp = m.sdp { io?.setAnswer(sdp: sdp) }
        case "ice":
            if let candidate = m.candidate { io?.addCandidate(candidate) }
        case "state":
            serverState = m.state ?? "listening"
            barge.speaking = serverState == "speaking"
            let tool = m.tool ?? ""
            // Whatever the Bot is up to, while you talk the call is hearing you.
            if barge.talking {
                set {
                    $0.state = .hearing
                    $0.tool = tool
                }
            } else if snap.state != .hearing || serverState == "speaking" {
                set {
                    $0.state = fromServer()
                    $0.tool = tool
                }
            } else {
                set { $0.tool = tool }
            }
        case "say":
            barge.said()
            set { $0.saying = m.text ?? "" }
        case "heard":
            set {
                $0.heard = m.text ?? ""
                $0.hearing = ""
                $0.error = ""
            }
        case "caption":
            // Live words of the turn being heard (a late one for a closed turn is dropped).
            if m.n == turnN { set { $0.hearing = m.text ?? "" } }
        case "eot":
            // The transcriber judged the turn over: end it now if there is silence already,
            // instead of waiting out the detector.
            if turnOpen && m.n == turnN && quiet >= eotQuietFrames && !snap.ptt {
                endTurn(quiet * SpeechGate.frameMs)
                endedEarly = true
            }
        case "error":
            set { $0.error = m.message ?? "" }
        case "bye":
            if let message = m.message, !message.isEmpty { set { $0.error = message } }
            finish(EndReason(rawValue: m.reason ?? "") ?? (m.reason == "network" ? .lost : .hangup))
        default:
            break
        }
    }

    private func fromServer() -> CallState {
        switch serverState {
        case "speaking": return .speaking
        case "thinking": return .thinking
        case "working": return .working
        case "needs_you": return .needsYou
        default: return .listening
        }
    }

    // MARK: presses

    /// Stops the Bot mid-sentence: what it was saying is cut off, and the next turn tells it so.
    public func interrupt() { barge.interrupt() }

    /// Mutes your microphone: nothing you say is heard until you unmute.
    public func setMuted(_ muted: Bool) {
        // Set first: what the paused detector hands over is dropped (sendTurn).
        set {
            $0.muted = muted
            $0.level = 0
            if muted && $0.state == .hearing { $0.state = self.fromServer() }
        }
        guard muted else { return }
        barge.ended()
        if turnOpen {
            turnOpen = false
            io?.sendText(["t": "cancel", "n": turnN])
        }
        _ = gate.pause()
    }

    /// Hold to talk instead of the voice detector: for a noisy room, or a speaker that leaks.
    public func setPtt(_ ptt: Bool) {
        barge.ended()
        // Nothing will end a turn the detector opened, once it stops listening.
        if turnOpen {
            turnOpen = false
            io?.sendText(["t": "cancel", "n": turnN])
        }
        set {
            $0.ptt = ptt
            $0.holding = false
        }
        held = []
    }

    /// The hold-to-talk press.
    public func hold(_ down: Bool) {
        guard snap.ptt, !snap.muted else { return }
        if down {
            held = []
            barge.sure()
            set {
                $0.holding = true
                $0.state = .hearing
            }
            if live {
                ring = []
                openTurn()
            }
            return
        }
        barge.ended()
        if live {
            set { $0.holding = false }
            if turnOpen { endTurn(0) }
            return
        }
        let audio = held.flatMap { $0 }
        held = []
        set { $0.holding = false }
        sendTurn(audio, eou: 0)
    }

    /// Ends the call, after handing over whatever you were still saying.
    public func hangup() {
        guard snap.state != .ended else { return }
        if snap.holding { hold(false) }
        for event in gate.pause() { handle(event) }
        io?.sendText(["t": "hangup"])
        finish(.hangup)
    }

    public func fail(_ error: String) {
        guard snap.state != .ended else { return }
        set { $0.error = error }
        finish(.error)
    }

    /// The socket closed: refused before it opened, or lost.
    public func socketClosed(opened: Bool) {
        guard snap.state != .ended else { return }
        if !opened && snap.callID.isEmpty {
            fail("The call could not start. Voice may be off, or this Bot is not yours to call.")
        } else {
            finish(.lost)
        }
    }

    public func finish(_ reason: EndReason) {
        guard snap.state != .ended else { return }
        set {
            $0.state = .ended
            $0.ended = reason
            $0.level = 0
        }
        io?.finished(reason)
    }
}

#if os(iOS)
import AVFoundation
import Foundation
import OSLog
import SiloClient
import WebRTC

private let log = Logger(subsystem: "silo", category: "call")

/// A call, the phone's half: the socket to the control plane, the WebRTC peer the Bot's voice
/// arrives on, the audio engine and the voice detector. What it all means is `CallBrain`; this is
/// the plumbing around it. Everything runs on one serial queue.
public final class CallSession: NSObject, @unchecked Sendable {
    private let queue = DispatchQueue(label: "silo.call", qos: .userInteractive)
    private let host: String
    private let botID: String
    private let chat: String
    private let onChange: @Sendable (CallSnapshot) -> Void
    private var brain: CallBrain!
    private let audio = CallAudioDevice()
    private var vad: SileroVAD?
    private var urlSession: URLSession?
    private var socket: URLSessionWebSocketTask?
    private var opened = false
    private var factory: RTCPeerConnectionFactory?
    private var peer: RTCPeerConnection?
    private var remoteSet = false
    private var pendingCandidates: [RTCIceCandidate] = []
    // The control plane reads nothing until the offer: a candidate that beat it would be lost.
    private var offered = false
    private var heldCandidates: [[String: Any]] = []
    private var timers: [DispatchSourceTimer] = []
    private var finished = false
    private var closed = false

    private static let ssl: Void = { RTCInitializeSSL() }()

    public init(host: String, botID: String, chat: String, microphone: Bool = true, onChange: @escaping @Sendable (CallSnapshot) -> Void) {
        self.host = host
        self.botID = botID
        self.chat = chat
        self.onChange = onChange
        super.init()
        brain = CallBrain(chat: chat, io: self)
        audio.microphone = microphone
        _ = Self.ssl
        audio.onFrame = { [weak self] frame in
            self?.queue.async { self?.heard(frame) }
        }
        audio.onFailure = { [weak self] reason in self?.fail(reason) }
        // Hold to talk needs no echo canceller, and that is better than no call.
        audio.onNoEchoCancelling = { [weak self] in self?.setPtt(true) }
    }

    // MARK: starting

    /// Opens the call. Audio starts with `audioActivated`, once the session is live.
    public func start() {
        queue.async { [self] in
            do {
                vad = try SileroVAD()
            } catch {
                brain.fail("The voice detector did not load: \(error.localizedDescription)")
                return
            }
            connectSocket()
        }
    }

    /// The audio session is live: CallKit said so, or the app activated it itself. Also after it
    /// was taken away, to take it back.
    public func audioActivated() {
        queue.async { [self] in
            guard !finished else { return }
            audio.start()
        }
    }

    /// Something else has the audio for now.
    public func audioHeld() {
        audio.hold()
    }

    /// Ends the call with the reason on screen.
    public func fail(_ reason: String) {
        queue.async { [self] in brain.fail(reason) }
    }

    private func connectSocket() {
        guard var parts = URLComponents(string: host) else {
            brain.fail("The server address is not valid.")
            return
        }
        parts.scheme = parts.scheme == "https" ? "wss" : "ws"
        parts.path = "/voice/call"
        parts.queryItems = [URLQueryItem(name: "bot", value: botID), URLQueryItem(name: "chat", value: chat)]
        guard let url = parts.url else { return }
        let session = URLSession(configuration: SiloSession.configuration(), delegate: self, delegateQueue: nil)
        let task = session.webSocketTask(with: url)
        task.maximumMessageSize = 1 << 20
        urlSession = session
        socket = task
        task.resume()
        receive(task)
        every(.seconds(15)) { [weak self] in self?.socket?.sendPing { _ in } }
    }

    private func receive(_ task: URLSessionWebSocketTask) {
        task.receive { [weak self] result in
            guard let self else { return }
            self.queue.async {
                switch result {
                case .success(.string(let text)):
                    if let message = try? JSONDecoder().decode(CallMessage.self, from: Data(text.utf8)) {
                        log.debug("got \(message.t) \(message.state ?? "")")
                        self.brain.receive(message)
                    } else {
                        log.error("unreadable message: \(text.prefix(120))")
                    }
                    self.receive(task)
                case .success:
                    self.receive(task)
                case .failure(let error):
                    log.info("socket closed: \(error.localizedDescription)")
                    self.brain.socketClosed(opened: self.opened)
                }
            }
        }
    }

    // MARK: presses

    public func setMuted(_ muted: Bool) {
        queue.async { [self] in
            brain.setMuted(muted)
            if !muted { vad?.reset() }
        }
    }

    public func setPtt(_ ptt: Bool) { queue.async { [self] in brain.setPtt(ptt) } }
    public func hold(_ down: Bool) { queue.async { [self] in brain.hold(down) } }
    public func interrupt() { queue.async { [self] in brain.interrupt() } }
    public func hangup() { queue.async { [self] in brain.hangup() } }

    // MARK: audio in

    private func heard(_ frame: [Float]) {
        guard !finished, !brain.snap.muted, let vad else { return }
        do {
            brain.frame(frame, probability: try vad.probability(frame))
        } catch {
            brain.frame(frame, probability: 0)
        }
    }

    private func every(_ interval: DispatchTimeInterval, _ work: @escaping () -> Void) {
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now() + interval, repeating: interval)
        timer.setEventHandler(handler: work)
        timer.resume()
        timers.append(timer)
    }

    // What is still held back from the speaker when you start talking, so a pause resumes where
    // you actually stopped hearing.
    private func reportPlayout() {
        peer?.statistics { [weak self] report in
            for stat in report.statistics.values where stat.type == "inbound-rtp" {
                log.info("rtp packets \(stat.values["packetsReceived"] as? NSNumber ?? 0) lost \(stat.values["packetsLost"] as? NSNumber ?? 0)")
                guard stat.values["kind"] as? String == "audio",
                      let delay = stat.values["jitterBufferDelay"] as? NSNumber,
                      let emitted = stat.values["jitterBufferEmittedCount"] as? NSNumber else { continue }
                let ms = playoutMs(jitterBufferDelay: delay.doubleValue, emitted: emitted.doubleValue)
                if ms > 0 { self?.queue.async { self?.sendText(["t": "playout", "delay_ms": ms]) } }
            }
        }
    }
}

// MARK: - The brain's hands

extension CallSession: CallBrainIO {
    public func sendText(_ object: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: object),
              let text = String(data: data, encoding: .utf8) else { return }
        socket?.send(.string(text)) { _ in }
    }

    public func sendBinary(_ data: Data) {
        socket?.send(.data(data)) { _ in }
    }

    public func muteVoice(_ muted: Bool) {
        audio.voiceMuted = muted
    }

    public func changed(_ snapshot: CallSnapshot) {
        onChange(snapshot)
    }

    /// How loud the Bot's voice is coming out, for a check that it does.
    public var voiceLevel: Float { audio.voiceLevel }

    #if DEBUG
    public func debugStopEngine() { audio.debugStopEngine() }
    public var debugEngineRunning: Bool { audio.debugEngineRunning }
    #endif

    public func connectPeer(iceServers: [ICEServer]) {
        let config = RTCConfiguration()
        config.sdpSemantics = .unifiedPlan
        config.continualGatheringPolicy = .gatherContinually
        config.iceServers = iceServers.map {
            RTCIceServer(urlStrings: $0.urls, username: $0.username, credential: $0.credential)
        }
        let factory = RTCPeerConnectionFactory(encoderFactory: RTCDefaultVideoEncoderFactory(),
                                               decoderFactory: RTCDefaultVideoDecoderFactory(),
                                               audioDevice: audio)
        self.factory = factory
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        guard let pc = factory.peerConnection(with: config, constraints: constraints, delegate: self) else {
            brain.fail("The call's audio could not start.")
            return
        }
        peer = pc
        let direction = RTCRtpTransceiverInit()
        direction.direction = .recvOnly
        pc.addTransceiver(of: .audio, init: direction)
        pc.offer(for: constraints) { [weak self] offer, error in
            guard let self, let offer else {
                self?.queue.async { self?.brain.fail(error?.localizedDescription ?? "The call's audio could not start.") }
                return
            }
            pc.setLocalDescription(offer) { error in
                if let error { log.error("local description: \(error.localizedDescription)") }
                self.queue.async {
                    log.info("offer sent")
                    self.offered = true
                    self.sendText(["t": "offer", "sdp": offer.sdp])
                    for candidate in self.heldCandidates { self.sendText(candidate) }
                    self.heldCandidates = []
                }
            }
        }
        every(.seconds(3)) { [weak self] in self?.reportPlayout() }
        every(.milliseconds(60)) { [weak self] in
            guard let self else { return }
            self.brain.botLevel(self.audio.voiceLevel)
        }
    }

    public func setAnswer(sdp: String) {
        peer?.setRemoteDescription(RTCSessionDescription(type: .answer, sdp: sdp)) { [weak self] _ in
            self?.queue.async {
                guard let self else { return }
                self.remoteSet = true
                for candidate in self.pendingCandidates { self.peer?.add(candidate) { _ in } }
                self.pendingCandidates = []
            }
        }
    }

    public func addCandidate(_ candidate: ICECandidate) {
        let ice = RTCIceCandidate(sdp: candidate.candidate, sdpMLineIndex: candidate.sdpMLineIndex ?? 0, sdpMid: candidate.sdpMid)
        if remoteSet {
            peer?.add(ice) { _ in }
        } else {
            pendingCandidates.append(ice)
        }
    }

    public func finished(_ reason: EndReason) {
        guard !finished else { return }
        finished = true
        timers.forEach { $0.cancel() }
        timers = []
        audio.stop()
        peer?.close()
        peer = nil
        factory = nil
        // Sends are in order, so the pong means the goodbye and a last turn got through: closing
        // drops whatever is still going out, and on a slow uplink a turn takes a while.
        socket?.sendPing { [self] _ in queue.async { [self] in closeSocket() } }
        queue.asyncAfter(deadline: .now() + 5) { [self] in closeSocket() }
    }

    private func closeSocket() {
        guard !closed else { return }
        closed = true
        socket?.cancel(with: .normalClosure, reason: nil)
        urlSession?.finishTasksAndInvalidate()
    }
}

// MARK: - WebRTC

extension CallSession: RTCPeerConnectionDelegate {
    public func peerConnection(_ pc: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {
        let line = Int(candidate.sdpMLineIndex)
        queue.async { [self] in
            var init_: [String: Any] = ["candidate": candidate.sdp, "sdpMLineIndex": line]
            if let mid = candidate.sdpMid { init_["sdpMid"] = mid }
            let message: [String: Any] = ["t": "ice", "candidate": init_]
            if offered { sendText(message) } else { heldCandidates.append(message) }
        }
    }

    public func peerConnection(_ pc: RTCPeerConnection, didChange state: RTCPeerConnectionState) {
        log.info("peer state \(state.rawValue)")
        guard state == .failed else { return }
        queue.async { [self] in
            brain.fail("The call's audio could not connect. A firewall may be blocking its port.")
        }
    }

    public func peerConnection(_ pc: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    public func peerConnection(_ pc: RTCPeerConnection, didAdd stream: RTCMediaStream) {}
    public func peerConnection(_ pc: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    public func peerConnectionShouldNegotiate(_ pc: RTCPeerConnection) {}
    public func peerConnection(_ pc: RTCPeerConnection, didChange newState: RTCIceConnectionState) {}
    public func peerConnection(_ pc: RTCPeerConnection, didChange newState: RTCIceGatheringState) {}
    public func peerConnection(_ pc: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    public func peerConnection(_ pc: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}
}

// MARK: - The socket

extension CallSession: URLSessionWebSocketDelegate {
    public func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didOpenWithProtocol protocol: String?) {
        log.info("socket open")
        queue.async { [self] in opened = true }
    }

    public func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didCloseWith closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?) {
        queue.async { [self] in brain.socketClosed(opened: opened) }
    }
}
#endif

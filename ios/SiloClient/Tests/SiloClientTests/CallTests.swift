import XCTest
@testable import SiloClient

/// The same cases as `web/src/call/bargein_test.ts`, then the turn logic of `session.ts`.
final class BargeInTests: XCTestCase {
    private var told: [String] = []
    private var muted: [Bool] = []
    private lazy var barge = BargeIn(tell: { self.told.append($0) }, mute: { self.muted.append($0) })

    private func talk(_ frames: Int, _ speech: Float = 0.9) {
        for _ in 0..<frames { barge.frame(speech: speech, detect: true) }
    }

    func testNothingWhileTheBotIsSilent() {
        talk(10)
        XCTAssertEqual(told, [])
    }

    func testDucksThenResumesWhenItWasNothing() {
        barge.speaking = true
        talk(3)
        XCTAssertEqual(told, ["duck"])
        XCTAssertEqual(muted, [true])
        barge.ended()
        XCTAssertEqual(told, ["duck", "unduck"])
        XCTAssertEqual(muted, [true, false])
    }

    func testSureStopsTheBotAndADuckNeverOutlivesIt() {
        barge.speaking = true
        talk(3)
        barge.sure()
        XCTAssertEqual(told, ["duck", "interrupt"])
        barge.ended()
        XCTAssertEqual(told, ["duck", "interrupt"], "a cut stays cut until the next sentence")
        barge.said()
        XCTAssertEqual(muted.last, false)
    }

    func testAlreadyTalkingWhenTheVoiceBegins() {
        barge.sure()
        barge.speaking = true
        barge.frame(speech: 0, detect: true)
        XCTAssertEqual(told, ["interrupt"])
    }

    func testHoldToTalkDoesNotDuck() {
        barge.speaking = true
        for _ in 0..<5 { barge.frame(speech: 0.9, detect: false) }
        XCTAssertEqual(told, [])
    }
}

final class SpeechGateTests: XCTestCase {
    private let frame = [Float](repeating: 0.1, count: 512)

    func testATurnStartsIsSureAndEnds() {
        var gate = SpeechGate()
        var events: [SpeechEvent] = []
        for _ in 0..<12 { events += gate.process(frame, probability: 0.9) }
        XCTAssertEqual(events, [.start, .realStart], "sure after 7 frames of speech (250 ms)")
        events = []
        for _ in 0..<20 { events += gate.process(frame, probability: 0.1) }
        XCTAssertEqual(events, [])
        events = gate.process(frame, probability: 0.1)
        guard case .end(let audio) = events.first else { return XCTFail("no end") }
        XCTAssertEqual(audio.count, (12 + 21) * 512)
    }

    func testAMisfire() {
        var gate = SpeechGate()
        var events: [SpeechEvent] = []
        for _ in 0..<2 { events += gate.process(frame, probability: 0.9) }
        for _ in 0..<22 { events += gate.process(frame, probability: 0.1) }
        XCTAssertEqual(events, [.start, .misfire])
    }

    func testAPauseInASentenceDoesNotEndTheTurn() {
        var gate = SpeechGate()
        var events: [SpeechEvent] = []
        for _ in 0..<10 { events += gate.process(frame, probability: 0.9) }
        for _ in 0..<15 { events += gate.process(frame, probability: 0.1) }
        for _ in 0..<3 { events += gate.process(frame, probability: 0.9) }
        for _ in 0..<15 { events += gate.process(frame, probability: 0.1) }
        XCTAssertEqual(events, [.start, .realStart])
    }

    func testThePadCarriesWhatCameBefore() {
        var gate = SpeechGate()
        for _ in 0..<40 { _ = gate.process(frame, probability: 0) }
        for _ in 0..<10 { _ = gate.process(frame, probability: 0.9) }
        guard case .end(let audio) = gate.pause().first else { return XCTFail("no end") }
        XCTAssertEqual(audio.count, (9 + 10) * 512, "300 ms of pad, then the speech")
    }

    func testPausingHandsOverWhatIsBeingSaid() {
        var gate = SpeechGate()
        XCTAssertEqual(gate.pause(), [])
        for _ in 0..<10 { _ = gate.process(frame, probability: 0.9) }
        guard case .end = gate.pause().first else { return XCTFail("a turn should be handed over") }
        XCTAssertFalse(gate.speaking)
    }
}

private final class FakeIO: CallBrainIO {
    var texts: [[String: Any]] = []
    var binaries: [Data] = []
    var muted: [Bool] = []
    var ice: [ICEServer]?
    var ended: EndReason?
    func sendText(_ object: [String: Any]) { texts.append(object) }
    func sendBinary(_ data: Data) { binaries.append(data) }
    func muteVoice(_ muted: Bool) { self.muted.append(muted) }
    func connectPeer(iceServers: [ICEServer]) { ice = iceServers }
    func setAnswer(sdp: String) {}
    func addCandidate(_ candidate: ICECandidate) {}
    func changed(_ snapshot: CallSnapshot) {}
    func finished(_ reason: EndReason) { ended = reason }
    var kinds: [String] { texts.compactMap { $0["t"] as? String } }
}

final class CallBrainTests: XCTestCase {
    private let io = FakeIO()
    private lazy var brain = CallBrain(chat: "", io: io)
    private let frame = [Float](repeating: 0.2, count: 512)

    private func message(_ json: String) -> CallMessage {
        try! JSONDecoder().decode(CallMessage.self, from: Data(json.utf8))
    }

    private func hello(stt: String = "turn") {
        brain.receive(message(#"{"t":"hello","call_id":"c1","chat_id":"m","stt":"\#(stt)","ice_servers":[{"urls":["stun:x"]}]}"#))
    }

    private func speak(_ frames: Int) { for _ in 0..<frames { brain.frame(frame, probability: 0.9) } }
    private func silence(_ frames: Int) { for _ in 0..<frames { brain.frame(frame, probability: 0.05) } }

    func testHelloStartsThePeer() {
        hello()
        XCTAssertEqual(brain.snap.state, .listening)
        XCTAssertEqual(brain.snap.chatID, "m")
        XCTAssertEqual(io.ice?.first?.urls, ["stun:x"])
    }

    func testATurnGoesUpAsOneWAV() {
        hello()
        speak(10)
        XCTAssertEqual(brain.snap.state, .hearing)
        silence(22)
        XCTAssertEqual(io.kinds, ["turn"])
        XCTAssertEqual(io.binaries.count, 1)
        XCTAssertEqual(String(decoding: io.binaries[0].prefix(4), as: UTF8.self), "RIFF")
        XCTAssertEqual(brain.snap.state, .thinking)
    }

    func testALiveTurnStreamsWithItsPreRoll() {
        hello(stt: "live")
        silence(20)
        speak(3)
        XCTAssertEqual(io.kinds, ["speech"])
        XCTAssertEqual(io.binaries.count, 10 + 2, "the pre-roll, then what follows")
        brain.receive(message(#"{"t":"caption","n":1,"text":"hello"}"#))
        XCTAssertEqual(brain.snap.hearing, "hello")
        speak(8)
        silence(22)
        XCTAssertEqual(io.kinds, ["speech", "turn"])
    }

    func testEndOfTurnEndsEarlyOnlyInSilence() {
        hello(stt: "live")
        speak(10)
        brain.receive(message(#"{"t":"eot","n":1}"#))
        XCTAssertEqual(io.kinds, ["speech"], "still talking")
        silence(3)
        brain.receive(message(#"{"t":"eot","n":1}"#))
        XCTAssertEqual(io.kinds, ["speech", "turn"])
        XCTAssertEqual(brain.snap.state, .thinking)
    }

    func testSpeakingOverTheBotStopsIt() {
        hello()
        brain.receive(message(#"{"t":"state","state":"speaking"}"#))
        XCTAssertEqual(brain.snap.state, .speaking)
        speak(3)
        XCTAssertEqual(io.kinds, ["duck"])
        speak(6)
        XCTAssertEqual(io.kinds, ["duck", "interrupt"])
        XCTAssertEqual(io.muted, [true, true])
        XCTAssertEqual(brain.snap.state, .hearing)
        brain.receive(message(#"{"t":"say","text":"Answer"}"#))
        XCTAssertEqual(io.muted, [true, true, false])
    }

    func testMutingCancelsAnOpenTurnAndHearsNothing() {
        hello(stt: "live")
        speak(3)
        brain.setMuted(true)
        XCTAssertEqual(io.kinds, ["speech", "cancel"])
        speak(10)
        XCTAssertEqual(io.kinds, ["speech", "cancel"])
    }

    func testHoldToTalk() {
        hello()
        brain.setPtt(true)
        speak(10)
        XCTAssertEqual(io.kinds, [], "the detector is out of it")
        brain.hold(true)
        XCTAssertEqual(brain.snap.state, .hearing)
        speak(10)
        brain.hold(false)
        XCTAssertEqual(io.kinds, ["turn"])
    }

    func testTurningOnHoldToTalkClosesAnOpenTurn() {
        hello(stt: "live")
        silence(20)
        speak(3)
        XCTAssertEqual(io.kinds, ["speech"])
        brain.setPtt(true)
        XCTAssertEqual(io.kinds, ["speech", "cancel"])
    }

    func testAHelloAfterHangingUpBringsNothingBack() {
        brain.hangup()
        hello()
        XCTAssertEqual(brain.snap.state, .ended)
        XCTAssertNil(io.ice)
    }

    func testALateFailureDoesNotRewriteTheEnding() {
        hello()
        brain.hangup()
        brain.fail("too late")
        XCTAssertEqual(brain.snap.ended, .hangup)
        XCTAssertEqual(brain.snap.error, "")
    }

    func testHangingUpHandsOverTheSentence() {
        hello()
        speak(10)
        brain.hangup()
        XCTAssertEqual(io.kinds, ["turn", "hangup"])
        XCTAssertEqual(io.ended, .hangup)
        XCTAssertEqual(brain.snap.state, .ended)
    }

    func testByeSaysWhy() {
        hello()
        brain.receive(message(#"{"t":"bye","reason":"network"}"#))
        XCTAssertEqual(io.ended, .lost)
    }

    func testARefusalIsNotALostCall() {
        brain.socketClosed(opened: false)
        XCTAssertEqual(io.ended, .error)
        XCTAssertFalse(brain.snap.error.isEmpty)
    }
}

final class CallLookTests: XCTestCase {
    func testToneAndWords() {
        var s = CallSnapshot()
        s.state = .hearing
        XCTAssertEqual(callTone(s), .you)
        s.state = .listening
        s.muted = true
        XCTAssertEqual(callTone(s), .muted)
        XCTAssertEqual(stateWords(s, botName: "Scout"), "Muted")
        s.state = .working
        s.tool = "exec_python"
        XCTAssertEqual(stateWords(s, botName: "Scout"), "Using Python")
        XCTAssertEqual(stateWords(s, botName: "Scout", doing: Doing(task: "Draft it")), "Draft it")
        s.state = .ended
        s.ended = .lost
        XCTAssertEqual(stateWords(s, botName: "Scout"), "The call's connection dropped")
    }

    func testAudioHelpers() {
        XCTAssertEqual(callClock(seconds: 65), "1:05")
        XCTAssertEqual(pcm16([0, 1, -1]), Data([0, 0, 0xFF, 0x7F, 0, 0x80]))
        XCTAssertEqual(encodeWAV([0.5], rate: 16000).count, 46)
        XCTAssertEqual(loudness([]), 0)
        XCTAssertGreaterThan(loudness([0.1, -0.1]), 0.5)
        XCTAssertEqual(playoutMs(jitterBufferDelay: 2, emitted: 100), 60)
        XCTAssertEqual(playoutMs(jitterBufferDelay: 2, emitted: 0), 0)
    }
}

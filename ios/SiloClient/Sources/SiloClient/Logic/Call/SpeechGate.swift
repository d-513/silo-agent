// The speech gate: from the detector's per-frame probability to a person's turns. Port of
// `FrameProcessor` in @ricky0123/vad-web with the settings `web/src/call/session.ts` gives it, so a
// turn starts and ends where it does on the web. Frames are 512 samples at 16 kHz (32 ms).

public enum SpeechEvent: Equatable, Sendable {
    /// It may be speech: the first likely frame.
    case start
    /// It is speech: `minSpeechMs` of it.
    case realStart
    /// It was not speech after all.
    case misfire
    /// A turn is over; here it is, with what came just before it.
    case end([Float])
}

public struct SpeechGate {
    public static let frameMs = 32
    /// A turn ends after this much quiet: long enough to get through a pause for thought.
    public static let redemptionMs = 700
    static let positive: Float = 0.5
    static let negative: Float = 0.35
    static let preSpeechPadMs = 300
    static let minSpeechMs = 250

    private let redemptionFrames = redemptionMs / frameMs
    private let padFrames = preSpeechPadMs / frameMs
    private let minSpeechFrames = minSpeechMs / frameMs
    private var buffer: [(frame: [Float], speech: Bool)] = []
    public private(set) var speaking = false
    private var redemption = 0
    private var speechCount = 0
    private var realStartFired = false

    public init() {}

    public mutating func reset() {
        buffer = []
        speaking = false
        redemption = 0
        speechCount = 0
        realStartFired = false
    }

    public mutating func process(_ frame: [Float], probability: Float) -> [SpeechEvent] {
        var events: [SpeechEvent] = []
        let isSpeech = probability >= Self.positive
        buffer.append((frame, isSpeech))
        if isSpeech {
            speechCount += 1
            redemption = 0
        }
        if isSpeech && !speaking {
            speaking = true
            events.append(.start)
        }
        if speaking && speechCount == minSpeechFrames && !realStartFired {
            realStartFired = true
            events.append(.realStart)
        }
        if probability < Self.negative && speaking {
            redemption += 1
            if redemption >= redemptionFrames {
                redemption = 0
                speechCount = 0
                speaking = false
                realStartFired = false
                events.append(takeSegment())
            }
        }
        if !speaking {
            if buffer.count > padFrames { buffer.removeFirst(buffer.count - padFrames) }
            speechCount = 0
        }
        return events
    }

    /// Hands over what is being said: hanging up in the middle of a sentence must not lose it.
    public mutating func pause() -> [SpeechEvent] {
        let was = speaking
        let event = was ? takeSegment() : nil
        reset()
        return event.map { [$0] } ?? []
    }

    private mutating func takeSegment() -> SpeechEvent {
        let taken = buffer
        buffer = []
        guard taken.filter(\.speech).count >= minSpeechFrames else { return .misfire }
        return .end(taken.flatMap(\.frame))
    }
}

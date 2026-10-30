// Barge-in: how you take the floor back from the Bot's voice. The moment you seem to start speaking
// over it the voice is muted here (no round trip) and the control plane is told to pause; once the
// detector is sure, to stop. If it was nothing, the voice picks up where it paused.
//
// A pause must always end in one of those two: the control plane holds the voice until it does,
// however long that takes. Port of `web/src/call/bargein.ts`; keep the two in step.

/// The Bot is ducked after `duckFrames` of likely speech.
private let duckFrames = 3
private let duckThreshold: Float = 0.7

public final class BargeIn {
    /// The Bot's voice is on, as the control plane says.
    public var speaking = false
    /// You are speaking for sure: the detector is, or the button is held.
    public private(set) var talking = false
    public private(set) var ducked = false
    private var cut = false
    private var run = 0
    private let tell: (String) -> Void
    private let mute: (Bool) -> Void

    public init(tell: @escaping (String) -> Void, mute: @escaping (Bool) -> Void) {
        self.tell = tell
        self.mute = mute
    }

    /// One frame of the microphone. `detect` is false while hold-to-talk replaces the detector.
    public func frame(speech: Float, detect: Bool) {
        guard speaking else {
            run = 0
            return
        }
        // You were already talking when the voice began: it does not get to start.
        if talking {
            interrupt()
            return
        }
        if ducked || !detect { return }
        run = speech > duckThreshold ? run + 1 : 0
        if run >= duckFrames {
            ducked = true
            mute(true)
            tell("duck")
        }
    }

    /// You are speaking for sure. The frames may not have ducked first.
    public func sure() {
        talking = true
        interrupt()
    }

    /// Your speech ended, or was never speech: a pause that was not followed through lifts.
    public func ended() {
        talking = false
        run = 0
        if ducked && !cut {
            ducked = false
            mute(false)
            tell("unduck")
        }
    }

    /// Stops the Bot mid-sentence.
    public func interrupt() {
        if cut || (!speaking && !ducked) { return }
        ducked = true
        cut = true
        mute(true)
        tell("interrupt")
    }

    /// The Bot started a new sentence. After a cut, that is the answer to what you said.
    public func said() {
        guard cut else { return }
        cut = false
        ducked = false
        run = 0
        mute(false)
    }
}

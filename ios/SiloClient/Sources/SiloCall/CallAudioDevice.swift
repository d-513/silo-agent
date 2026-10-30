#if os(iOS)
import AVFoundation
import OSLog
import WebRTC

private let log = Logger(subsystem: "silo", category: "audio")

/// What the render thread reads and writes. Plain memory on purpose: it may not take a lock, and a
/// torn meter reading costs nothing.
private final class RenderState {
    var playing = false
    var muted = false
    var level: Float = 0
    var getPlayout: RTCAudioDeviceGetPlayoutDataBlock?
}

private enum AudioProblem: LocalizedError {
    case noInput
    var errorDescription: String? { "The microphone is not available." }
}

/// One microphone tap, with its converter and what is left over. A rebuild makes a new one, so it
/// cannot change these under a buffer still on its way.
private final class Hearing {
    private static let frameSamples = 512
    private static let detector = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 16_000, channels: 1, interleaved: false)!

    private let mono: AVAudioFormat
    private let converter: AVAudioConverter
    private let onFrame: (([Float]) -> Void)?
    private var pending: [Float] = []

    init?(sampleRate: Double, onFrame: (([Float]) -> Void)?) {
        guard let mono = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: sampleRate, channels: 1, interleaved: false),
              let converter = AVAudioConverter(from: mono, to: Self.detector) else { return nil }
        self.mono = mono
        self.converter = converter
        self.onFrame = onFrame
    }

    func take(_ buffer: AVAudioPCMBuffer) {
        guard let channels = buffer.floatChannelData, buffer.frameLength > 0,
              let first = AVAudioPCMBuffer(pcmFormat: mono, frameCapacity: buffer.frameLength) else { return }
        // Voice processing can hand over several channels; the first is the processed one.
        first.frameLength = buffer.frameLength
        memcpy(first.floatChannelData![0], channels[0], Int(buffer.frameLength) * MemoryLayout<Float>.size)
        let ratio = Self.detector.sampleRate / mono.sampleRate
        let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 32
        guard let out = AVAudioPCMBuffer(pcmFormat: Self.detector, frameCapacity: capacity) else { return }
        var given = false
        var failure: NSError?
        converter.convert(to: out, error: &failure) { _, status in
            if given {
                status.pointee = .noDataNow
                return nil
            }
            given = true
            status.pointee = .haveData
            return first
        }
        guard failure == nil, out.frameLength > 0, let samples = out.floatChannelData?[0] else { return }
        pending.append(contentsOf: UnsafeBufferPointer(start: samples, count: Int(out.frameLength)))
        while pending.count >= Self.frameSamples {
            onFrame?(Array(pending.prefix(Self.frameSamples)))
            pending.removeFirst(Self.frameSamples)
        }
    }
}

/// One audio engine for the whole call, in voice processing mode: WebRTC's decoded voice plays
/// through its output, the microphone comes from its input, and because they are one I/O unit the
/// echo canceller hears exactly what it has to take out. WebRTC only pulls samples; the frames for
/// the detector are tapped here at 16 kHz.
///
/// An engine stops itself when the hardware changes and after an interruption, and nothing says so
/// for certain, so `running` is what the call wants and `engine.isRunning` is what is true: the
/// watchdog brings the one back to the other. Everything that touches the engine runs on `work`,
/// or a route change and a hang-up would pull it apart together.
public final class CallAudioDevice: NSObject, RTCAudioDevice {
    private static let voiceRate = 48_000.0
    /// How often it looks, and how many failed rebuilds it takes to give up (about nine seconds).
    private static let watchEvery: DispatchTimeInterval = .milliseconds(1500)
    private static let maxMisses = 6

    /// One 512-sample frame at 16 kHz, on the tap's thread.
    var onFrame: (([Float]) -> Void)?
    /// False plays the Bot's voice with no microphone at all (a simulator with no way to hear).
    var microphone = true
    /// The engine would not run, and the call has no audio.
    var onFailure: ((String) -> Void)?
    /// It runs without the echo canceller: over a speaker the Bot would interrupt itself.
    var onNoEchoCancelling: (() -> Void)?

    private let work = DispatchQueue(label: "silo.call.engine", qos: .userInteractive)
    private let engine = AVAudioEngine()
    private let state = RenderState()
    private var source: AVAudioSourceNode?
    private var observer: NSObjectProtocol?
    private var watchdog: DispatchSourceTimer?
    private var running = false
    // Something else has the audio: no use fighting for it until `start`.
    private var held = false
    private var echoCancelling = true
    private var misses = 0

    // WebRTC's own thread sets this, and `work` reads it.
    private let delegateLock = NSLock()
    private var heldDelegate: (any RTCAudioDeviceDelegate)?
    private var delegate: (any RTCAudioDeviceDelegate)? {
        get { delegateLock.withLock { heldDelegate } }
        set { delegateLock.withLock { heldDelegate = newValue } }
    }

    // MARK: control

    /// Starts the engine, or brings back one that stopped. The audio session must be active.
    public func start() {
        work.async { [self] in
            held = false
            misses = 0
            guard !running else {
                if !engine.isRunning { revive() }
                return
            }
            running = true
            echoCancelling = microphone
            observer = NotificationCenter.default.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: nil) { [weak self] _ in
                self?.work.async { self?.check() }
            }
            watch()
            comeUp()
        }
    }

    /// Something else has the audio for now. `start` takes it back.
    public func hold() {
        work.async { [self] in held = true }
    }

    /// Waits for the engine: WebRTC must not go away under a rebuild.
    public func stop() {
        work.sync { [self] in
            running = false
            watchdog?.cancel()
            watchdog = nil
            if let observer { NotificationCenter.default.removeObserver(observer) }
            observer = nil
            teardown()
        }
    }

    /// Mutes the Bot's voice at once: WebRTC keeps being read so nothing piles up behind the mute.
    public var voiceMuted: Bool {
        get { state.muted }
        set { state.muted = newValue }
    }

    /// How loud the Bot's voice is right now, 0...1.
    public var voiceLevel: Float { state.level }

    #if DEBUG
    /// Stops the engine behind the call's back, as the system does.
    public func debugStopEngine() { work.async { [self] in engine.stop() } }
    public var debugEngineRunning: Bool { work.sync { engine.isRunning } }
    #endif

    // MARK: keeping it running

    private func watch() {
        let timer = DispatchSource.makeTimerSource(queue: work)
        timer.schedule(deadline: .now() + Self.watchEvery, repeating: Self.watchEvery)
        timer.setEventHandler { [weak self] in self?.check() }
        timer.resume()
        watchdog = timer
    }

    private func check() {
        guard running, !held, !engine.isRunning else { return }
        revive()
    }

    private func revive() {
        log.info("engine stopped, bringing it back")
        teardown()
        // Before it plays again: the next pull may come from another thread, with another buffer.
        if let delegate {
            delegate.dispatchSync {
                delegate.notifyAudioOutputInterrupted()
                delegate.notifyAudioOutputParametersChange()
            }
        }
        if attempt(voiceProcessing: echoCancelling) {
            misses = 0
            return
        }
        misses += 1
        if misses >= Self.maxMisses {
            running = false
            onFailure?("The call lost its audio.")
        }
    }

    private func comeUp() {
        if attempt(voiceProcessing: echoCancelling) { return }
        // A session that has only just gone live can refuse the first time.
        Thread.sleep(forTimeInterval: 0.3)
        if attempt(voiceProcessing: echoCancelling) { return }
        if echoCancelling, attempt(voiceProcessing: false) {
            echoCancelling = false
            log.error("running with no echo canceller")
            onNoEchoCancelling?()
            return
        }
        running = false
        onFailure?("The microphone could not start.")
    }

    // Leaves nothing behind on failure: a tap installed twice is an exception nothing can catch.
    private func attempt(voiceProcessing: Bool) -> Bool {
        do {
            try build(voiceProcessing: voiceProcessing)
            log.info("audio engine started")
            return true
        } catch {
            log.error("audio engine: \(error.localizedDescription)")
            teardown()
            return false
        }
    }

    private func build(voiceProcessing: Bool) throws {
        if microphone {
            let input = engine.inputNode
            if input.isVoiceProcessingEnabled != voiceProcessing {
                try input.setVoiceProcessingEnabled(voiceProcessing)
            }
            if voiceProcessing { input.isVoiceProcessingAGCEnabled = true }
            let format = input.outputFormat(forBus: 0)
            guard format.sampleRate > 0, format.channelCount > 0 else { throw AudioProblem.noInput }
            // Voice processing needs the two ends the same (AVAudioIONode.h), and a route changes them.
            let matching = voiceProcessing && format.channelCount <= 2
            engine.connect(engine.mainMixerNode, to: engine.outputNode, format: matching ? format : nil)
            attachVoice()
            guard let hearing = Hearing(sampleRate: format.sampleRate, onFrame: onFrame) else { throw AudioProblem.noInput }
            input.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in hearing.take(buffer) }
        } else {
            attachVoice()
        }
        engine.prepare()
        try engine.start()
    }

    private func teardown() {
        if microphone { engine.inputNode.removeTap(onBus: 0) }
        engine.stop()
        if let source {
            engine.disconnectNodeOutput(source)
            engine.detach(source)
        }
        source = nil
    }

    private func attachVoice() {
        let format = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: Self.voiceRate, channels: 1, interleaved: true)!
        let state = state
        let node = AVAudioSourceNode(format: format) { silence, timestamp, frames, output in
            let buffers = UnsafeMutableAudioBufferListPointer(output)
            func zero() {
                for buffer in buffers { if let data = buffer.mData { memset(data, 0, Int(buffer.mDataByteSize)) } }
            }
            guard state.playing, let get = state.getPlayout else {
                zero()
                silence.pointee = true
                state.level = 0
                return noErr
            }
            var flags = AudioUnitRenderActionFlags()
            guard get(&flags, timestamp, 0, frames, output) == noErr, let data = buffers[0].mData else {
                zero()
                state.level = 0
                return noErr
            }
            let samples = data.assumingMemoryBound(to: Int16.self)
            let count = Int(buffers[0].mDataByteSize) / MemoryLayout<Int16>.size
            var sum: Float = 0
            for i in 0..<count {
                let s = Float(samples[i]) / 32768
                sum += s * s
            }
            state.level = count > 0 ? min(1, ((sum / Float(count)).squareRoot() * 6).squareRoot()) : 0
            if state.muted { zero() }
            return noErr
        }
        engine.attach(node)
        engine.connect(node, to: engine.mainMixerNode, format: format)
        source = node
    }

    // MARK: RTCAudioDevice

    public var deviceInputSampleRate: Double { Self.voiceRate }
    public var inputIOBufferDuration: TimeInterval { AVAudioSession.sharedInstance().ioBufferDuration }
    public var inputNumberOfChannels: Int { 1 }
    public var inputLatency: TimeInterval { AVAudioSession.sharedInstance().inputLatency }
    public var deviceOutputSampleRate: Double { Self.voiceRate }
    public var outputIOBufferDuration: TimeInterval { AVAudioSession.sharedInstance().ioBufferDuration }
    public var outputNumberOfChannels: Int { 1 }
    public var outputLatency: TimeInterval { AVAudioSession.sharedInstance().outputLatency }
    public private(set) var isInitialized = false
    public private(set) var isPlayoutInitialized = false
    public private(set) var isPlaying = false
    // The call never sends audio on the peer: its turns go on the socket.
    public var isRecordingInitialized: Bool { true }
    public var isRecording: Bool { false }

    public func initialize(with delegate: any RTCAudioDeviceDelegate) -> Bool {
        self.delegate = delegate
        state.getPlayout = delegate.getPlayoutData
        isInitialized = true
        return true
    }

    public func terminateDevice() -> Bool {
        state.playing = false
        state.getPlayout = nil
        delegate = nil
        isInitialized = false
        isPlayoutInitialized = false
        isPlaying = false
        return true
    }

    public func initializePlayout() -> Bool {
        isPlayoutInitialized = true
        return true
    }

    public func startPlayout() -> Bool {
        log.info("playout started")
        isPlaying = true
        state.playing = true
        return true
    }

    public func stopPlayout() -> Bool {
        isPlaying = false
        state.playing = false
        return true
    }

    public func initializeRecording() -> Bool { true }
    public func startRecording() -> Bool { true }
    public func stopRecording() -> Bool { true }
}
#endif

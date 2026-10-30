#if os(iOS)
import AudioToolbox
import AVFoundation

/// Plays Read aloud while it arrives. The server speaks sentence by sentence into one open-ended
/// answer and its first GET is the stream, so a player that probes with ranges would spend it:
/// this parses the bytes itself (MP3, or PCM in a WAV of unknown length) and schedules them.
public final class SpeechStreamPlayer: @unchecked Sendable {
    private let engine = AVAudioEngine()
    private let node = AVAudioPlayerNode()
    private let queue = DispatchQueue(label: "silo.readaloud")
    private var parser: AudioFileStreamID?
    private var source: AVAudioFormat?
    private var target: AVAudioFormat?
    private var converter: AVAudioConverter?
    private var scheduled = 0
    private var ending = false
    private var stopped = false
    private var started = false
    private var observers: [NSObjectProtocol] = []
    /// The first sound came out.
    public var onStart: (@Sendable () -> Void)?
    /// All of it was played, or it was stopped.
    public var onFinish: (@Sendable () -> Void)?

    public init(mime: String) {
        let hint: AudioFileTypeID = mime.contains("wav") ? kAudioFileWAVEType : kAudioFileMP3Type
        let context = Unmanaged.passUnretained(self).toOpaque()
        AudioFileStreamOpen(context, { context, stream, property, _ in
            Unmanaged<SpeechStreamPlayer>.fromOpaque(context).takeUnretainedValue().property(stream, property)
        }, { context, bytes, packets, data, descriptions in
            Unmanaged<SpeechStreamPlayer>.fromOpaque(context).takeUnretainedValue()
                .packets(bytes: bytes, count: packets, data: data, descriptions: descriptions)
        }, hint, &parser)
        // Either stops the engine for good, and what was scheduled would never finish playing.
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: nil) { [weak self] _ in
            self?.stop()
        })
        observers.append(center.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: nil) { [weak self] _ in
            self?.stop()
        })
    }

    deinit {
        observers.forEach(NotificationCenter.default.removeObserver)
        if let parser { AudioFileStreamClose(parser) }
    }

    public func feed(_ data: Data) {
        queue.async { [self] in
            guard !stopped, let parser else { return }
            data.withUnsafeBytes { raw in
                _ = AudioFileStreamParseBytes(parser, UInt32(raw.count), raw.baseAddress!, [])
            }
        }
    }

    /// Nothing more is coming.
    public func finish() {
        queue.async { [self] in
            ending = true
            finishIfDone()
        }
    }

    public func stop() {
        queue.async { [self] in
            guard !stopped else { return }
            stopped = true
            node.stop()
            engine.stop()
            onFinish?()
        }
    }

    private func finishIfDone() {
        guard ending, scheduled == 0, !stopped else { return }
        stopped = true
        engine.stop()
        onFinish?()
    }

    private func property(_ stream: AudioFileStreamID, _ property: AudioFileStreamPropertyID) {
        guard property == kAudioFileStreamProperty_ReadyToProducePackets else { return }
        var format = AudioStreamBasicDescription()
        var size = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
        guard AudioFileStreamGetProperty(stream, kAudioFileStreamProperty_DataFormat, &size, &format) == noErr,
              let input = AVAudioFormat(streamDescription: &format),
              let output = AVAudioFormat(standardFormatWithSampleRate: format.mSampleRate, channels: format.mChannelsPerFrame) else { return }
        source = input
        target = output
        converter = AVAudioConverter(from: input, to: output)
        engine.attach(node)
        engine.connect(node, to: engine.mainMixerNode, format: output)
        do {
            try engine.start()
        } catch {
            stop()
        }
    }

    private func packets(bytes: UInt32, count: UInt32, data: UnsafeRawPointer, descriptions: UnsafeMutablePointer<AudioStreamPacketDescription>?) {
        guard let source, let target, let converter, !stopped else { return }
        let input: AVAudioBuffer
        if let descriptions {
            let largest = (0..<Int(count)).map { Int(descriptions[$0].mDataByteSize) }.max() ?? 0
            let buffer = AVAudioCompressedBuffer(format: source, packetCapacity: count, maximumPacketSize: largest)
            memcpy(buffer.data, data, Int(bytes))
            buffer.byteLength = bytes
            buffer.packetCount = count
            if let out = buffer.packetDescriptions { out.update(from: descriptions, count: Int(count)) }
            input = buffer
        } else {
            let frames = bytes / max(source.streamDescription.pointee.mBytesPerFrame, 1)
            guard let buffer = AVAudioPCMBuffer(pcmFormat: source, frameCapacity: frames) else { return }
            buffer.frameLength = frames
            memcpy(buffer.mutableAudioBufferList.pointee.mBuffers.mData, data, Int(bytes))
            input = buffer
        }
        let frames = max(source.streamDescription.pointee.mFramesPerPacket, 1) * count + 4096
        guard let pcm = AVAudioPCMBuffer(pcmFormat: target, frameCapacity: frames) else { return }
        var given = false
        var failure: NSError?
        converter.convert(to: pcm, error: &failure) { _, status in
            if given {
                status.pointee = .noDataNow
                return nil
            }
            given = true
            status.pointee = .haveData
            return input
        }
        guard failure == nil, pcm.frameLength > 0 else { return }
        scheduled += 1
        node.scheduleBuffer(pcm, completionCallbackType: .dataPlayedBack) { [weak self] _ in
            self?.queue.async {
                guard let self else { return }
                self.scheduled -= 1
                self.finishIfDone()
            }
        }
        if !started, engine.isRunning {
            started = true
            node.play()
            onStart?()
        }
    }
}
#endif

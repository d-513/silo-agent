import Foundation

// Pure helpers for a call's audio. Port of `web/src/call/audio.ts`.

private func clamp16(_ sample: Float) -> Int16 {
    let s = max(-1, min(1, sample))
    return Int16(s < 0 ? s * 0x8000 : s * 0x7FFF)
}

/// A turn's samples (-1...1 at `rate`) as a 16-bit mono WAV file.
public func encodeWAV(_ samples: [Float], rate: Int) -> Data {
    var out = Data(capacity: 44 + samples.count * 2)
    func u32(_ v: UInt32) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }
    func u16(_ v: UInt16) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }
    out.append(contentsOf: Array("RIFF".utf8))
    u32(UInt32(36 + samples.count * 2))
    out.append(contentsOf: Array("WAVEfmt ".utf8))
    u32(16)
    u16(1)
    u16(1)
    u32(UInt32(rate))
    u32(UInt32(rate * 2))
    u16(2)
    u16(16)
    out.append(contentsOf: Array("data".utf8))
    u32(UInt32(samples.count * 2))
    for sample in samples { withUnsafeBytes(of: clamp16(sample).littleEndian) { out.append(contentsOf: $0) } }
    return out
}

/// A frame (-1...1) as 16-bit little-endian PCM, what a live turn streams.
public func pcm16(_ frame: [Float]) -> Data {
    var out = Data(capacity: frame.count * 2)
    for sample in frame { withUnsafeBytes(of: clamp16(sample).littleEndian) { out.append(contentsOf: $0) } }
    return out
}

/// How loud a frame is, 0...1: its RMS on a curve that makes speech fill the meter.
public func loudness(_ frame: [Float]) -> Float {
    guard !frame.isEmpty else { return 0 }
    let sum = frame.reduce(Float(0)) { $0 + $1 * $1 }
    return min(1, (sum / Float(frame.count)).squareRoot().times(6).squareRoot())
}

private extension Float {
    func times(_ by: Float) -> Float { self * by }
}

/// The playout delay in ms from an inbound-rtp report, or 0 when it cannot tell.
public func playoutMs(jitterBufferDelay: Double, emitted: Double) -> Int {
    guard emitted > 0, jitterBufferDelay >= 0 else { return 0 }
    // Plus what the output device holds on to.
    return Int((jitterBufferDelay / emitted * 1000).rounded()) + 40
}

/// A call's length as m:ss.
public func callClock(seconds: Int) -> String {
    let total = max(0, seconds)
    return "\(total / 60):\(String(format: "%02d", total % 60))"
}

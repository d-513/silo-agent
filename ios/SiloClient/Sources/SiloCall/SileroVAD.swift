#if os(iOS)
import Foundation
import OnnxRuntimeBindings

/// Silero v5, the detector the web's calls use, so a phone hears a turn where the browser does. It
/// takes 512 samples at 16 kHz with the last 64 of the frame before, and keeps its own state. Port
/// of `Silero` in @ricky0123/vad-web.
final class SileroVAD {
    private static let contextSamples = 64
    private static let stateSize = 2 * 128

    private let env: ORTEnv
    private let session: ORTSession
    private let rate: ORTValue
    private var state = [Float](repeating: 0, count: stateSize)
    private var context = [Float](repeating: 0, count: contextSamples)

    init() throws {
        guard let path = Bundle.module.path(forResource: "silero_vad_v5", ofType: "onnx") else {
            throw NSError(domain: "SileroVAD", code: 1, userInfo: [NSLocalizedDescriptionKey: "the model is not in the app"])
        }
        env = try ORTEnv(loggingLevel: .warning)
        let options = try ORTSessionOptions()
        try options.setIntraOpNumThreads(1)
        try options.setGraphOptimizationLevel(.all)
        session = try ORTSession(env: env, modelPath: path, sessionOptions: options)
        var sr = Int64(16000)
        rate = try ORTValue(tensorData: NSMutableData(bytes: &sr, length: MemoryLayout<Int64>.size),
                            elementType: .int64, shape: [1])
    }

    func reset() {
        state = [Float](repeating: 0, count: Self.stateSize)
        context = [Float](repeating: 0, count: Self.contextSamples)
    }

    /// How sure it is that this frame is speech, 0...1.
    func probability(_ frame: [Float]) throws -> Float {
        let samples = context + frame
        context = Array(frame.suffix(Self.contextSamples))
        let input = try samples.withUnsafeBufferPointer {
            try ORTValue(tensorData: NSMutableData(bytes: $0.baseAddress, length: $0.count * MemoryLayout<Float>.size),
                         elementType: .float, shape: [1, NSNumber(value: samples.count)])
        }
        let stateValue = try state.withUnsafeBufferPointer {
            try ORTValue(tensorData: NSMutableData(bytes: $0.baseAddress, length: $0.count * MemoryLayout<Float>.size),
                         elementType: .float, shape: [2, 1, 128])
        }
        let out = try session.run(withInputs: ["input": input, "state": stateValue, "sr": rate],
                                  outputNames: ["output", "stateN"], runOptions: nil)
        guard let output = out["output"], let next = out["stateN"] else { return 0 }
        let nextState = try next.tensorData() as Data
        nextState.withUnsafeBytes { raw in
            state = Array(raw.bindMemory(to: Float.self))
        }
        return try (output.tensorData() as Data).withUnsafeBytes { $0.bindMemory(to: Float.self).first ?? 0 }
    }
}
#endif

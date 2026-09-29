import AVFoundation
import Observation
import SwiftUI

/// Composer dictation: record with `AVAudioRecorder`, hand the take to `Transcribe`, and put the
/// text back in the draft. Never auto-sends.
@MainActor
@Observable
final class Dictation: NSObject {
    enum State { case idle, recording, transcribing }

    private(set) var state: State = .idle
    private(set) var elapsed: Double = 0
    var error: String?

    static let maxSeconds: Double = 10 * 60

    private var recorder: AVAudioRecorder?
    private var ticker: Task<Void, Never>?
    private var url: URL?

    func start() async {
        guard state == .idle else { return }
        error = nil
        let session = AVAudioSession.sharedInstance()
        guard await AVAudioApplication.requestRecordPermission() else {
            error = "Microphone access is off. Turn it on in Settings."
            return
        }
        do {
            try session.setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker])
            try session.setActive(true)
            let file = FileManager.default.temporaryDirectory.appendingPathComponent("dictation-\(UUID().uuidString).m4a")
            let settings: [String: Any] = [
                AVFormatIDKey: kAudioFormatMPEG4AAC,
                AVSampleRateKey: 16_000,
                AVNumberOfChannelsKey: 1,
                AVEncoderBitRateKey: 32_000,
            ]
            let recorder = try AVAudioRecorder(url: file, settings: settings)
            guard recorder.record() else { throw NSError(domain: "Dictation", code: 1) }
            self.recorder = recorder
            url = file
            elapsed = 0
            state = .recording
            ticker = Task { [weak self] in
                while !Task.isCancelled {
                    try? await Task.sleep(for: .milliseconds(250))
                    guard let self, self.state == .recording else { return }
                    self.elapsed = self.recorder?.currentTime ?? self.elapsed
                    if self.elapsed >= Self.maxSeconds { self.recorder?.stop(); return }
                }
            }
        } catch {
            self.error = "Could not start the microphone."
        }
    }

    /// Stops recording and returns the audio, or nil for a cancelled/empty take.
    func finish() -> Data? {
        guard state == .recording else { return nil }
        recorder?.stop()
        ticker?.cancel()
        recorder = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        defer { if let url { try? FileManager.default.removeItem(at: url) } }
        guard let url, let data = try? Data(contentsOf: url), !data.isEmpty else {
            state = .idle
            return nil
        }
        state = .transcribing
        return data
    }

    func cancel() {
        recorder?.stop()
        ticker?.cancel()
        recorder = nil
        if let url { try? FileManager.default.removeItem(at: url) }
        state = .idle
    }

    func done() { state = .idle }
}

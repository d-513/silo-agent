import AVFoundation
import Observation
import SiloCall
import SiloClient
import SwiftUI

/// A Bot's words in its own voice, one voice at a time: starting another stops this one. Web:
/// `web/src/ReadAloud.tsx`.
@MainActor
@Observable
final class ReadAloud {
    enum State { case idle, loading, playing }

    private(set) var state: State = .idle
    /// What is being read, so a reply can tell it is its own.
    private(set) var text = ""
    private(set) var error: String?

    private var player: SpeechStreamPlayer?
    private var task: Task<Void, Never>?

    func toggle(_ text: String, client: SiloClient, botID: String) {
        if state != .idle, self.text == text {
            stop()
        } else {
            speak(text, client: client, botID: botID)
        }
    }

    /// `model`, `voice` and `speed` override the Bot's own, for Hear it in Settings.
    func speak(_ text: String, client: SiloClient, botID: String, model: String = "", voice: String = "", speed: Double = 0) {
        // A call has the audio.
        guard !CallModel.shared.live else { return }
        stop()
        self.text = text
        state = .loading
        error = nil
        task = Task { [weak self] in
            do {
                let url = try await client.prepareSpeech(botID: botID, text: text, model: model, voice: voice, speed: speed)
                // A call may have started meanwhile, and the audio is its now.
                try Task.checkCancellation()
                try AVAudioSession.sharedInstance().setCategory(.playback, mode: .spokenAudio)
                try AVAudioSession.sharedInstance().setActive(true)
                let session = URLSession(configuration: SiloSession.configuration())
                let (bytes, response) = try await session.bytes(from: url)
                guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw SiloError.server("The voice did not answer. Try again in a moment.") }
                let player = SpeechStreamPlayer(mime: response.mimeType ?? "audio/mpeg")
                player.onStart = { Task { @MainActor [weak self] in self?.state = .playing } }
                player.onFinish = { Task { @MainActor [weak self] in self?.finished() } }
                self?.player = player
                var chunk = Data()
                for try await byte in bytes {
                    chunk.append(byte)
                    if chunk.count >= 4096 {
                        player.feed(chunk)
                        chunk = Data()
                    }
                }
                if !chunk.isEmpty { player.feed(chunk) }
                player.finish()
            } catch is CancellationError {
                return
            } catch {
                guard !Task.isCancelled else { return }
                self?.error = (error as? SiloError)?.errorDescription ?? error.localizedDescription
                self?.finished()
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
        player?.stop()
        player = nil
        finished()
    }

    private func finished() {
        state = .idle
        text = ""
        player = nil
        guard !CallModel.shared.live else { return }
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
}

/// The reply's Read aloud glyph: a speaker, a spinner while it readies, a square while it plays.
struct ReadAloudButton: View {
    @Environment(AppModel.self) private var model
    let text: String

    private var mine: Bool { model.readAloud.text == text }

    var body: some View {
        Button {
            guard let botID = model.selectedBotID else { return }
            model.readAloud.toggle(text, client: model.client, botID: botID)
        } label: {
            ZStack {
                if mine && model.readAloud.state == .loading {
                    Spinner(size: 13)
                } else if mine && model.readAloud.state == .playing {
                    Image(systemName: "stop.fill").font(.system(size: 11)).foregroundStyle(Theme.cobalt)
                } else {
                    Image(systemName: "speaker.wave.2").font(.system(size: 13))
                }
            }
        }
        .buttonStyle(.glyph(tint: Theme.ink3, side: 32))
        .accessibilityLabel(mine && model.readAloud.state != .idle ? "Stop reading" : "Read aloud")
    }
}

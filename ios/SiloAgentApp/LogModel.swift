import Observation
import SiloClient
import SwiftUI

/// A read-only run-event stream for one chat log (a subagent's, later an automation's). The
/// selected chat has its own in `AppModel`; this one is for pages that show a different log.
@MainActor
@Observable
final class LogModel {
    private(set) var events: [Silo_V1_RunEvent] = []
    private let client: SiloClient
    private let botID: String
    private let chatID: String
    private var task: Task<Void, Never>?

    init(client: SiloClient, botID: String, chatID: String) {
        self.client = client
        self.botID = botID
        self.chatID = chatID
    }

    var busy: Bool { chatBusy(events) }

    func start() {
        guard task == nil else { return }
        task = Task { [weak self] in
            var after = ""
            var seen = Set<String>()
            while !Task.isCancelled {
                guard let self else { return }
                streamLoop: for await item in self.client.streamRun(botID: self.botID, chatID: self.chatID, afterEventID: after) {
                    if Task.isCancelled { return }
                    guard case .event(let event) = item else { continue }
                    if event.kind == "reset" {
                        self.events = []
                        after = ""
                        seen.removeAll()
                        break streamLoop
                    }
                    if event.kind == "board" || event.kind == "subagents" { continue }
                    if !event.id.isEmpty {
                        if seen.contains(event.id) { continue }
                        seen.insert(event.id)
                        after = event.id
                    }
                    self.events.append(event)
                }
                guard !Task.isCancelled else { return }
                try? await Task.sleep(for: .milliseconds(800))
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
    }
}

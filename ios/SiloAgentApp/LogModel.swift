import Observation
import SiloClient
import SwiftUI

/// A read-only run-event stream for one chat log (a subagent's, later an automation's). The
/// selected chat has its own in `AppModel`; this one is for pages that show a different log.
@MainActor
@Observable
final class LogModel {
    private(set) var events: [Silo_V1_RunEvent] = []
    /// A long log replays its newest turns only; these load the pages before (see `AppModel`).
    private(set) var earlier = false
    private(set) var prepended = 0
    private(set) var loadingEarlier = false
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
                        self.earlier = false
                        self.prepended = 0
                        after = ""
                        seen.removeAll()
                        break streamLoop
                    }
                    if event.kind == "earlier" {
                        self.earlier = true
                        continue
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

    func loadEarlier() async {
        guard let before = events.first?.id, !before.isEmpty, !loadingEarlier else { return }
        loadingEarlier = true
        defer { loadingEarlier = false }
        guard let page = try? await client.listChatEvents(botID: botID, chatID: chatID, beforeEventID: before),
              events.first?.id == before else { return }
        events.insert(contentsOf: page.events, at: 0)
        prepended += page.events.count
        earlier = page.more
    }

    func stop() {
        task?.cancel()
        task = nil
    }
}

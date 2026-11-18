import Foundation
import Observation
import SiloClient
import WidgetKit

/// What the watch knows: the phone's shared state (the same `SharedState` the iPhone widgets
/// read), refreshed from the server while the app is open, and the one problem in the way, worded
/// by `reachProblem`. A failed refresh keeps the last good state on screen under the problem, so
/// the watch never goes blank because the network did.
@Observable
final class WatchModel {
    static let shared = WatchModel()

    private(set) var state = SiloStore.load()
    private(set) var problem: ReachProblem?
    private(set) var refreshing = false
    /// The phone is in reach for a handoff right now.
    var phoneNearby = false
    /// A handoff asked for and not yet answered.
    var asking = false

    var signedIn: Bool { state.signedIn && !state.server.isEmpty }

    // MARK: the handoff

    /// Takes the phone's sign-in: its cookies replace the watch's, and what the phone last saw is
    /// shown until the watch's own refresh lands (unless the watch's copy is newer).
    func adopt(_ handoff: SessionHandoff) {
        let store = SiloSession.cookies
        for cookie in store.cookies ?? [] { store.deleteCookie(cookie) }
        guard handoff.signedIn else {
            state = SharedState()
            problem = nil
            SiloStore.save(state)
            WidgetCenter.shared.reloadAllTimelines()
            return
        }
        for cookie in handoff.cookies { store.setCookie(cookie) }
        var next = handoff.snapshot.flatMap { try? JSONDecoder().decode(SharedState.self, from: $0) } ?? SharedState()
        if state.server == handoff.server, state.email == handoff.email, state.at > next.at { next = state }
        next.server = handoff.server
        next.email = handoff.email
        state = next
        if problem?.kind == .signedOut { problem = nil }
        SiloStore.save(next)
        WidgetCenter.shared.reloadAllTimelines()
        Task { await refresh() }
    }

    // MARK: reading

    func refresh() async {
        guard signedIn, !refreshing else { return }
        refreshing = true
        defer { refreshing = false }
        do {
            let next = try await SiloFeed.load(state)
            state = next
            problem = nil
            if SiloStore.save(next) { WidgetCenter.shared.reloadAllTimelines() }
        } catch {
            note(error)
        }
    }

    /// Records why a request failed; a session the server turned away asks the phone again.
    private func note(_ error: Error) {
        let found = reachProblem(error, server: state.server)
        guard found.kind != .cancelled else { return }
        problem = found
        if found.kind == .signedOut { WatchLink.shared.ask() }
    }

    var client: SiloClient { SiloClient(host: state.server) }

    func bot(_ id: String) -> SharedBot? { state.bots.first { $0.id == id } }

    // MARK: acting

    /// Allow once or Deny. Nil when it went through; the problem when it did not.
    func decide(_ approval: SharedApproval, allow: Bool) async -> ReachProblem? {
        do {
            try await client.decideApproval(id: approval.id, decision: allow ? "allow_once" : "deny")
            state.approvals.removeAll { $0.id == approval.id }
            if let index = state.bots.firstIndex(where: { $0.id == approval.botID }) {
                state.bots[index].needs = max(0, state.bots[index].needs - 1)
            }
            SiloStore.save(state)
            WidgetCenter.shared.reloadAllTimelines()
            Task { await refresh() }
            return nil
        } catch {
            let found = reachProblem(error, server: state.server)
            if found.kind == .signedOut { WatchLink.shared.ask() }
            // Answered somewhere else meanwhile: drop it here too.
            if found.kind == .refusedAction { Task { await refresh() } }
            return found
        }
    }

    enum AskOutcome: Equatable {
        case replied(String)
        /// Sent, but no reply in time (or the stream dropped): it is in the chat on the phone.
        case stillWorking
        /// Not sent.
        case failed(ReachProblem)
    }

    /// Sends `text` to the Bot's main chat and waits a while for its reply.
    func ask(_ botID: String, _ text: String) async -> AskOutcome {
        let client = client
        let sent: Silo_V1_SendResponse
        do {
            sent = try await client.send(botID: botID, chatID: "", text: text)
        } catch {
            let found = reachProblem(error, server: state.server)
            if found.kind == .signedOut { WatchLink.shared.ask() }
            return .failed(found)
        }
        var reply = ""
        let deadline = Date().addingTimeInterval(75)
        stream: for await item in client.streamRun(botID: botID, chatID: sent.chatID, afterEventID: "") {
            guard Date() < deadline else { break }
            switch item {
            case .event(let event):
                guard event.runID == sent.runID else { continue }
                switch event.kind {
                case "assistant", "section": reply = event.body
                case "done", "error": break stream
                default: continue
                }
            case .finished:
                break stream
            }
        }
        return reply.isEmpty ? .stillWorking : .replied(reply)
    }

    /// The newest Feed posts, or why they could not be had.
    func feed(_ botID: String) async -> Result<[Silo_V1_FeedPost], ReachProblem> {
        do {
            return .success(Array(try await client.listFeed(botID: botID).prefix(5)))
        } catch {
            return .failure(reachProblem(error, server: state.server))
        }
    }
}

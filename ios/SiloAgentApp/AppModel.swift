import Foundation
import Observation
import SiloClient

/// App-wide state: session, the Bot list, and the selected conversation's run stream.
@MainActor
@Observable
final class AppModel {
    static let defaultServerURL = "http://localhost:8080"

    // Session
    private(set) var user: Silo_V1_User?
    private(set) var restoring = true
    var serverURL: String {
        didSet {
            UserDefaults.standard.set(serverURL, forKey: "serverURL")
            client = SiloClient(host: normalizedServerURL())
        }
    }
    var email = ""
    var password = ""
    private(set) var signingIn = false
    var signInError: String?

    // Bots
    private(set) var bots: [Silo_V1_Bot] = []
    private(set) var botsError: String?
    private(set) var startingBotID: String?

    // Chats
    private(set) var chats: [Silo_V1_Chat] = []
    private(set) var events: [Silo_V1_RunEvent] = []
    private(set) var sending = false
    var errorMessage: String?

    // Selection
    var selectedBotID: String?
    var selectedChatID: String?
    var tab: BotTab = .chat

    private var client: SiloClient
    private var streamTask: Task<Void, Never>?
    private var pollTask: Task<Void, Never>?
    private var loadedBotID: String?
    private var streamingChatID: String?

    init() {
        let stored = UserDefaults.standard.string(forKey: "serverURL") ?? Self.defaultServerURL
        serverURL = stored
        client = SiloClient(host: stored)
    }

    var selectedBot: Silo_V1_Bot? {
        guard let selectedBotID else { return nil }
        return bots.first { $0.id == selectedBotID }
    }

    var isRunning: Bool { sending || chatBusy(events) }

    // MARK: - Session lifecycle

    func restoreSession() async {
        restoring = true
        defer { restoring = false }
        do {
            user = try await client.me()
            await refreshBots()
            startPolling()
        } catch {
            user = nil
        }
    }

    func signIn() async {
        guard !signingIn else { return }
        signingIn = true
        signInError = nil
        defer { signingIn = false }
        let host = normalizedServerURL()
        if host != serverURL { serverURL = host }
        do {
            user = try await client.signIn(email: email.trimmingCharacters(in: .whitespaces), password: password)
            password = ""
            await refreshBots()
            startPolling()
        } catch {
            signInError = describe(error)
        }
    }

    func signOut() async {
        try? await client.signOut()
        stopStream()
        stopPolling()
        user = nil
        bots = []
        botsError = nil
        chats = []
        events = []
        sending = false
        selectedBotID = nil
        selectedChatID = nil
        tab = .chat
        loadedBotID = nil
        streamingChatID = nil
    }

    // MARK: - Bots

    func refreshBots() async {
        do {
            bots = try await client.listBots()
            botsError = nil
        } catch {
            if isUnauthenticated(error) { await signOut() } else { botsError = describe(error) }
        }
    }

    func chooseBot(_ id: String?) async {
        guard let id, id != loadedBotID else { return }
        stopStream()
        streamingChatID = nil
        loadedBotID = id
        selectedChatID = nil
        chats = []
        events = []
        tab = .chat
        do {
            chats = try await client.listChats(botID: id)
        } catch {
            if isUnauthenticated(error) { await signOut() } else { errorMessage = describe(error) }
        }
        if let first = chats.first { selectChat(first.id) }
    }

    func createBot(name: String, crest: Int32, description: String) async throws -> Silo_V1_Bot {
        let bot = try await client.createBot(name: name, crest: crest, description: description)
        await refreshBots()
        return bot
    }

    func startBot() async {
        guard let botID = selectedBotID else { return }
        startingBotID = botID
        defer { startingBotID = nil }
        do {
            _ = try await client.startBot(botID)
        } catch {
            errorMessage = describe(error)
        }
        await refreshBots()
    }

    func stopBot() async {
        guard let botID = selectedBotID else { return }
        do {
            _ = try await client.stopBot(botID)
        } catch {
            errorMessage = describe(error)
        }
        await refreshBots()
    }

    // MARK: - Chats and runs

    func newChat() async {
        guard let botID = selectedBotID else { return }
        do {
            let chat = try await client.createChat(botID: botID)
            chats.insert(chat, at: 0)
            tab = .chat
            selectChat(chat.id)
        } catch {
            errorMessage = describe(error)
        }
    }

    func selectChat(_ id: String) {
        guard id != streamingChatID else { return }
        selectedChatID = id
        events = []
        sending = false
        startStream(for: id)
    }

    func send(_ text: String) async {
        guard let botID = selectedBotID else { return }
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty else { return }
        errorMessage = nil
        sending = true
        do {
            var chatID = selectedChatID ?? ""
            if chatID.isEmpty {
                let chat = try await client.createChat(botID: botID)
                chats.insert(chat, at: 0)
                chatID = chat.id
                selectedChatID = chat.id
                startStream(for: chat.id)
            }
            _ = try await client.send(botID: botID, chatID: chatID, text: message)
            await refreshChats()
            await refreshBots()
        } catch {
            sending = false
            if isUnauthenticated(error) {
                await signOut()
            } else {
                errorMessage = describe(error)
            }
        }
    }

    func stopRun() async {
        guard let botID = selectedBotID, let chatID = selectedChatID else { return }
        do {
            try await client.stopRun(botID: botID, chatID: chatID)
        } catch {
            errorMessage = describe(error)
        }
    }

    func refreshChats() async {
        guard let botID = selectedBotID else { return }
        chats = (try? await client.listChats(botID: botID)) ?? chats
    }

    // MARK: - Stream

    private func startStream(for chatID: String) {
        stopStream()
        guard let botID = selectedBotID else { return }
        streamingChatID = chatID
        streamTask = Task { [weak self] in
            var after = ""
            var seen = Set<String>()
            while !Task.isCancelled {
                guard let self else { return }
                for await item in self.client.streamRun(botID: botID, chatID: chatID, afterEventID: after) {
                    if Task.isCancelled { return }
                    switch item {
                    case .event(let event):
                        if !event.id.isEmpty {
                            if seen.contains(event.id) { continue }
                            seen.insert(event.id)
                            after = event.id
                        }
                        self.events.append(event)
                        self.sending = chatBusy(self.events)
                        if event.kind == "done" || event.kind == "chat_title" {
                            await self.refreshChats()
                        }
                    case .finished(let error):
                        if let error, self.isUnauthenticated(error) {
                            await self.signOut()
                            return
                        }
                    }
                }
                guard !Task.isCancelled else { return }
                try? await Task.sleep(for: .milliseconds(800))
            }
        }
    }

    private func stopStream() {
        streamTask?.cancel()
        streamTask = nil
    }

    // MARK: - Polling

    private func startPolling() {
        stopPolling()
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(4))
                guard let self, !Task.isCancelled else { return }
                await self.refreshBots()
            }
        }
    }

    private func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    // MARK: - Helpers

    private func normalizedServerURL() -> String {
        var value = serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.isEmpty { value = Self.defaultServerURL }
        if !value.contains("://") { value = "http://" + value }
        while value.hasSuffix("/") { value.removeLast() }
        return value
    }

    private func isUnauthenticated(_ error: Error) -> Bool {
        if let silo = error as? SiloError, case .unauthenticated = silo { return true }
        return false
    }

    private func describe(_ error: Error) -> String {
        (error as? SiloError)?.errorDescription ?? error.localizedDescription
    }
}

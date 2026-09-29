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

    // Composer: models, thinking, context, attachments
    private(set) var models: [Silo_V1_ModelOption] = []
    private(set) var defaultModel = ""
    private(set) var voiceEnabled = false
    private(set) var usage: Usage?
    private(set) var attachments: [Silo_V1_Attachment] = []
    var attachError: String?

    // Approvals (the Bot's "Needs you" queue, oldest first)
    private(set) var approvals: [Silo_V1_Approval] = []
    /// The approval whose sheet the reader swiped away; a banner brings it back.
    var dismissedApprovalID: String?

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

    var selectedChat: Silo_V1_Chat? {
        guard let selectedChatID else { return nil }
        return chats.first { $0.id == selectedChatID }
    }

    /// The model the next turn uses: the chat's override, else the operator default.
    var activeModel: String {
        let chosen = selectedChat?.model ?? ""
        return chosen.isEmpty ? defaultModel : chosen
    }

    var activeModelOption: Silo_V1_ModelOption? { models.first { $0.id == activeModel } }

    var thinkingLevels: [String] { activeModelOption?.thinkingLevels ?? [] }

    /// The thinking level that will actually be sent (the chat's choice fitted to the model).
    var fittedThinking: String { fitThinking(selectedChat?.thinking ?? "", thinkingLevels) }

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
        approvals = []
        dismissedApprovalID = nil
        usage = nil
        attachments = []
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
        await refreshApprovals()
        await loadModels(botID: id)
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

    // MARK: - Chat and message actions

    func renameChat(_ id: String, title: String) async {
        guard let botID = selectedBotID else { return }
        let clean = title.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !clean.isEmpty else { return }
        do {
            replaceChat(try await client.renameChat(botID: botID, chatID: id, title: clean))
        } catch {
            errorMessage = describe(error)
        }
    }

    func deleteChat(_ id: String) async {
        guard let botID = selectedBotID else { return }
        do {
            try await client.deleteChat(botID: botID, chatID: id)
            chats.removeAll { $0.id == id }
            if selectedChatID == id {
                stopStream()
                streamingChatID = nil
                events = []
                selectedChatID = nil
                if let next = chats.first { selectChat(next.id) }
            }
        } catch {
            errorMessage = describe(error)
        }
    }

    func editMessage(eventID: String, text: String) async {
        guard let botID = selectedBotID, let chatID = selectedChatID else { return }
        do {
            try await client.editMessage(botID: botID, chatID: chatID, eventID: eventID, text: text)
        } catch {
            errorMessage = describe(error)
        }
    }

    func deleteMessage(eventID: String) async {
        guard let botID = selectedBotID, let chatID = selectedChatID else { return }
        do {
            try await client.deleteMessage(botID: botID, chatID: chatID, eventID: eventID)
        } catch {
            errorMessage = describe(error)
        }
    }

    /// Branch a new chat from the context before this message and open it.
    func diverge(eventID: String) async {
        guard let botID = selectedBotID, let chatID = selectedChatID else { return }
        do {
            let chat = try await client.divergeChat(botID: botID, chatID: chatID, eventID: eventID)
            chats.insert(chat, at: 0)
            selectChat(chat.id)
        } catch {
            errorMessage = describe(error)
        }
    }

    // MARK: - Composer

    func loadModels(botID: String) async {
        guard let list = try? await client.listModels(botID: botID), botID == selectedBotID else { return }
        models = list.models
        defaultModel = list.defaultModel
        voiceEnabled = list.voiceEnabled
    }

    func setModel(_ model: String) async {
        guard let botID = selectedBotID, let chatID = selectedChatID else { return }
        do {
            replaceChat(try await client.setChatModel(botID: botID, chatID: chatID, model: model))
        } catch {
            errorMessage = describe(error)
        }
    }

    func setThinking(_ level: String) async {
        guard let botID = selectedBotID, let chatID = selectedChatID else { return }
        do {
            replaceChat(try await client.setChatThinking(botID: botID, chatID: chatID, thinking: level))
        } catch {
            errorMessage = describe(error)
        }
    }

    func compact() async {
        guard let botID = selectedBotID, let chatID = selectedChatID, !isRunning else { return }
        do {
            try await client.compactChat(botID: botID, chatID: chatID)
        } catch {
            errorMessage = describe(error)
        }
    }

    func collectMemories() async {
        guard let botID = selectedBotID, let chatID = selectedChatID else { return }
        do {
            try await client.collectMemories(botID: botID, chatID: chatID)
        } catch {
            errorMessage = describe(error)
        }
    }

    /// Uploads to `tmp/<name>` on the Bot's workspace (input only; wiped on container start).
    func attach(name: String, data: Data) async {
        guard let botID = selectedBotID else { return }
        attachError = nil
        guard selectedBot?.workerConnected == true else {
            attachError = "Start the Bot to attach files."
            return
        }
        guard data.count <= 50 << 20 else {
            attachError = "\(name) is larger than 50 MB"
            return
        }
        let safe = name.replacingOccurrences(of: "/", with: "_")
        let path = "tmp/\(safe)"
        do {
            try await client.putFile(botID: botID, path: path, data: data)
            var item = Silo_V1_Attachment()
            item.name = safe
            item.path = path
            item.size = Int64(data.count)
            attachments.removeAll { $0.path == path }
            attachments.append(item)
        } catch {
            attachError = describe(error)
        }
    }

    func removeAttachment(_ path: String) {
        attachments.removeAll { $0.path == path }
    }

    private func replaceChat(_ chat: Silo_V1_Chat) {
        if let index = chats.firstIndex(where: { $0.id == chat.id }) { chats[index] = chat }
    }

    // MARK: - Approvals

    func refreshApprovals() async {
        guard let botID = selectedBotID else { return }
        do {
            let list = try await client.listApprovals(botID: botID)
            guard botID == selectedBotID else { return }
            approvals = list
            if let dismissed = dismissedApprovalID, !list.contains(where: { $0.id == dismissed }) {
                dismissedApprovalID = nil
            }
        } catch {
            if isUnauthenticated(error) { await signOut() }
        }
    }

    /// `decision`: `allow_once`, `always`, `deny`, or `auto` (set the rule to Auto, allow this one).
    func decide(_ approval: Silo_V1_Approval, _ decision: String) async {
        do {
            if decision == "auto" {
                try await client.setRule(botID: approval.botID, connector: approval.connector, action: approval.action, decision: "auto")
                try await client.decideApproval(id: approval.id, decision: "allow_once")
            } else {
                try await client.decideApproval(id: approval.id, decision: decision)
            }
            approvals.removeAll { $0.id == approval.id }
            dismissedApprovalID = nil
        } catch {
            errorMessage = describe(error)
        }
        await refreshApprovals()
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
        usage = nil
        sending = false
        startStream(for: id)
    }

    func send(_ text: String) async {
        guard let botID = selectedBotID else { return }
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty || !attachments.isEmpty else { return }
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
            _ = try await client.send(botID: botID, chatID: chatID, text: message, attachments: attachments)
            attachments = []
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
                streamLoop: for await item in self.client.streamRun(botID: botID, chatID: chatID, afterEventID: after) {
                    if Task.isCancelled { return }
                    switch item {
                    case .event(let event):
                        if event.kind == "reset" {
                            // History was truncated by an edit/delete; replay from scratch.
                            self.events = []
                            self.sending = false
                            self.usage = nil
                            after = ""
                            seen.removeAll()
                            break streamLoop
                        }
                        if !event.id.isEmpty {
                            if seen.contains(event.id) { continue }
                            seen.insert(event.id)
                            after = event.id
                        }
                        if event.kind == "usage", let usage = Usage(eventBody: event.body) { self.usage = usage }
                        self.events.append(event)
                        self.sending = chatBusy(self.events)
                        if event.kind == "approval" {
                            await self.refreshApprovals()
                        }
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
                await self.refreshApprovals()
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

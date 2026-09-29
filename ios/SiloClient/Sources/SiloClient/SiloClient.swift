import Connect
import Foundation
import SwiftProtobuf

/// User-facing error surfaced by the Silo client.
public enum SiloError: LocalizedError, Sendable, Equatable {
    case unauthenticated
    case invalidArgument(String)
    case unavailable(String)
    case server(String)

    public var errorDescription: String? {
        switch self {
        case .unauthenticated:
            return "Your session expired. Sign in again."
        case .invalidArgument(let message):
            return message
        case .unavailable(let message):
            return message
        case .server(let message):
            return message
        }
    }

    static func wrap(_ error: ConnectError) -> SiloError {
        let message = error.message ?? "\(error.code)"
        switch error.code {
        case .unauthenticated:
            return .unauthenticated
        case .invalidArgument, .failedPrecondition, .permissionDenied, .notFound:
            return .invalidArgument(message)
        case .unavailable, .deadlineExceeded:
            return .unavailable("Cannot reach the Control Plane. \(message)")
        default:
            return .server(message)
        }
    }
}

/// One item from a `StreamRun` subscription.
public enum SiloRunEvent: Sendable {
    case event(Silo_V1_RunEvent)
    case finished(SiloError?)
}

/// Thin, typed wrapper over the generated Connect client for `silo.v1.UI`.
///
/// Cookie-based auth comes for free: `URLSessionHTTPClient` uses
/// `URLSessionConfiguration.default`, which persists `Set-Cookie` responses in
/// `HTTPCookieStorage.shared` for later requests on the same host.
public final class SiloClient: Sendable {
    private let protocolClient: ProtocolClient
    private let host: String

    public init(host: String) {
        self.host = host
        let config = ProtocolClientConfig(
            host: host,
            networkProtocol: .connect,
            codec: ProtoCodec()
        )
        self.protocolClient = ProtocolClient(
            httpClient: URLSessionHTTPClient(configuration: .default),
            config: config
        )
    }

    private var ui: Silo_V1_UiClient { Silo_V1_UiClient(client: protocolClient) }

    // MARK: - Session

    public func signIn(email: String, password: String) async throws -> Silo_V1_User {
        var request = Silo_V1_SignInRequest()
        request.email = email
        request.password = password
        let response = await ui.signIn(request: request, headers: [:])
        return try unwrap(response).user
    }

    public func signOut() async throws {
        _ = await ui.signOut(request: Silo_V1_SignOutRequest(), headers: [:])
    }

    public func me() async throws -> Silo_V1_User {
        let response = await ui.me(request: Silo_V1_MeRequest(), headers: [:])
        return try unwrap(response).user
    }

    // MARK: - Bots

    public func listBots() async throws -> [Silo_V1_Bot] {
        let response = await ui.listBots(request: Silo_V1_ListBotsRequest(), headers: [:])
        return try unwrap(response).bots
    }

    public func getBot(_ id: String) async throws -> Silo_V1_Bot {
        var request = Silo_V1_GetBotRequest()
        request.id = id
        let response = await ui.getBot(request: request, headers: [:])
        return try unwrap(response)
    }

    public func createBot(name: String, crest: Int32, description: String) async throws -> Silo_V1_Bot {
        var request = Silo_V1_CreateBotRequest()
        request.name = name
        request.crest = crest
        request.description_p = description
        let response = await ui.createBot(request: request, headers: [:])
        return try unwrap(response)
    }

    public func startBot(_ id: String) async throws -> Silo_V1_Bot {
        var request = Silo_V1_GetBotRequest()
        request.id = id
        let response = await ui.startBot(request: request, headers: [:])
        return try unwrap(response)
    }

    public func stopBot(_ id: String) async throws -> Silo_V1_Bot {
        var request = Silo_V1_GetBotRequest()
        request.id = id
        let response = await ui.stopBot(request: request, headers: [:])
        return try unwrap(response)
    }

    // MARK: - Chats

    public func listChats(botID: String) async throws -> [Silo_V1_Chat] {
        var request = Silo_V1_ListChatsRequest()
        request.botID = botID
        let response = await ui.listChats(request: request, headers: [:])
        return try unwrap(response).chats
    }

    public func createChat(botID: String) async throws -> Silo_V1_Chat {
        var request = Silo_V1_CreateChatRequest()
        request.botID = botID
        let response = await ui.createChat(request: request, headers: [:])
        return try unwrap(response)
    }

    // MARK: - Runs

    public func send(botID: String, chatID: String, text: String, attachments: [Silo_V1_Attachment] = []) async throws -> Silo_V1_SendResponse {
        var request = Silo_V1_SendRequest()
        request.botID = botID
        request.chatID = chatID
        request.text = text
        request.attachments = attachments
        let response = await ui.send(request: request, headers: [:])
        return try unwrap(response)
    }

    public func stopRun(botID: String, chatID: String) async throws {
        var request = Silo_V1_StopRunRequest()
        request.botID = botID
        request.chatID = chatID
        _ = try unwrap(await ui.stopRun(request: request, headers: [:]))
    }

    /// Subscribe to a conversation's run event stream, resuming after `afterEventID`.
    public func streamRun(botID: String, chatID: String, afterEventID: String) -> AsyncStream<SiloRunEvent> {
        let stream = ui.streamRun(headers: [:])
        var request = Silo_V1_StreamRunRequest()
        request.botID = botID
        request.chatID = chatID
        request.afterEventID = afterEventID

        return AsyncStream { continuation in
            do {
                try stream.send(request)
            } catch {
                continuation.yield(.finished(.server(error.localizedDescription)))
                continuation.finish()
                return
            }
            let pump = Task {
                for await result in stream.results() {
                    switch result {
                    case .message(let event):
                        continuation.yield(.event(event))
                    case .headers:
                        continue
                    case .complete(let code, let error, _):
                        if code == .ok || code == .canceled {
                            continuation.yield(.finished(nil))
                        } else {
                            continuation.yield(.finished(.server(error?.localizedDescription ?? "\(code)")))
                        }
                        continuation.finish()
                        return
                    }
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in
                pump.cancel()
                stream.cancel()
            }
        }
    }

    // MARK: - Subagents and taskboard

    public func listSubagents(botID: String, chatID: String) async throws -> [Silo_V1_Subagent] {
        var request = Silo_V1_ListSubagentsRequest()
        request.botID = botID
        request.chatID = chatID
        return try unwrap(await ui.listSubagents(request: request, headers: [:])).subagents
    }

    public func getSubagent(botID: String, id: String) async throws -> Silo_V1_Subagent {
        var request = Silo_V1_GetSubagentRequest()
        request.botID = botID
        request.id = id
        return try unwrap(await ui.getSubagent(request: request, headers: [:]))
    }

    public func stopSubagent(botID: String, id: String) async throws {
        var request = Silo_V1_StopSubagentRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.stopSubagent(request: request, headers: [:]))
    }

    public func getTaskboard(botID: String, chatID: String) async throws -> [Silo_V1_TaskItem] {
        var request = Silo_V1_GetTaskboardRequest()
        request.botID = botID
        request.chatID = chatID
        return try unwrap(await ui.getTaskboard(request: request, headers: [:])).items
    }

    public func clearTaskboard(botID: String, chatID: String) async throws {
        var request = Silo_V1_ClearTaskboardRequest()
        request.botID = botID
        request.chatID = chatID
        _ = try unwrap(await ui.clearTaskboard(request: request, headers: [:]))
    }

    // MARK: - Chat management and message actions

    public func renameChat(botID: String, chatID: String, title: String) async throws -> Silo_V1_Chat {
        var request = Silo_V1_RenameChatRequest()
        request.botID = botID
        request.id = chatID
        request.title = title
        return try unwrap(await ui.renameChat(request: request, headers: [:]))
    }

    public func deleteChat(botID: String, chatID: String) async throws {
        var request = Silo_V1_DeleteChatRequest()
        request.botID = botID
        request.id = chatID
        _ = try unwrap(await ui.deleteChat(request: request, headers: [:]))
    }

    /// Only the trailing user message can be edited; it re-sends as a fresh turn.
    public func editMessage(botID: String, chatID: String, eventID: String, text: String, attachments: [Silo_V1_Attachment] = []) async throws {
        var request = Silo_V1_EditMessageRequest()
        request.botID = botID
        request.chatID = chatID
        request.eventID = eventID
        request.text = text
        request.attachments = attachments
        _ = try unwrap(await ui.editMessage(request: request, headers: [:]))
    }

    public func deleteMessage(botID: String, chatID: String, eventID: String) async throws {
        var request = Silo_V1_DeleteMessageRequest()
        request.botID = botID
        request.chatID = chatID
        request.eventID = eventID
        _ = try unwrap(await ui.deleteMessage(request: request, headers: [:]))
    }

    /// Copies the context before a message into a new chat.
    public func divergeChat(botID: String, chatID: String, eventID: String) async throws -> Silo_V1_Chat {
        var request = Silo_V1_DivergeChatRequest()
        request.botID = botID
        request.chatID = chatID
        request.eventID = eventID
        return try unwrap(await ui.divergeChat(request: request, headers: [:])).chat
    }

    // MARK: - Models, thinking, compaction, files

    public struct ModelList: Sendable {
        public var models: [Silo_V1_ModelOption]
        public var defaultModel: String
        public var voiceEnabled: Bool
    }

    public func listModels(botID: String) async throws -> ModelList {
        var request = Silo_V1_ListModelsRequest()
        request.botID = botID
        let response = try unwrap(await ui.listModels(request: request, headers: [:]))
        return ModelList(models: response.models, defaultModel: response.defaultModel, voiceEnabled: response.voiceEnabled)
    }

    public func setChatModel(botID: String, chatID: String, model: String) async throws -> Silo_V1_Chat {
        var request = Silo_V1_SetChatModelRequest()
        request.botID = botID
        request.chatID = chatID
        request.model = model
        return try unwrap(await ui.setChatModel(request: request, headers: [:]))
    }

    public func setChatThinking(botID: String, chatID: String, thinking: String) async throws -> Silo_V1_Chat {
        var request = Silo_V1_SetChatThinkingRequest()
        request.botID = botID
        request.chatID = chatID
        request.thinking = thinking
        return try unwrap(await ui.setChatThinking(request: request, headers: [:]))
    }

    public func compactChat(botID: String, chatID: String) async throws {
        var request = Silo_V1_CompactChatRequest()
        request.botID = botID
        request.chatID = chatID
        _ = try unwrap(await ui.compactChat(request: request, headers: [:]))
    }

    public func collectMemories(botID: String, chatID: String) async throws {
        var request = Silo_V1_CollectMemoriesRequest()
        request.botID = botID
        request.chatID = chatID
        _ = try unwrap(await ui.collectMemories(request: request, headers: [:]))
    }

    public func putFile(botID: String, path: String, data: Data) async throws {
        var request = Silo_V1_PutFileRequest()
        request.botID = botID
        request.path = path
        request.data = data
        _ = try unwrap(await ui.putFile(request: request, headers: [:]))
    }

    public func transcribe(botID: String, audio: Data, mime: String) async throws -> String {
        var request = Silo_V1_TranscribeRequest()
        request.botID = botID
        request.audio = audio
        request.mime = mime
        return try unwrap(await ui.transcribe(request: request, headers: [:])).text
    }

    // MARK: - Downloads

    /// GETs a cookie-authed CP route (e.g. `ArtifactInfo.downloadPath`) and returns its bytes.
    public func download(path: String) async throws -> Data {
        guard let url = URL(string: host + path) else { throw SiloError.invalidArgument("Bad download URL") }
        let (data, response) = try await URLSession.shared.data(from: url)
        guard let http = response as? HTTPURLResponse else { throw SiloError.server("No response") }
        switch http.statusCode {
        case 200..<300: return data
        case 401: throw SiloError.unauthenticated
        default:
            let message = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            throw SiloError.server(message.isEmpty ? "Download failed (\(http.statusCode))" : message)
        }
    }

    public func saveSkill(botID: String, path: String, runID: String) async throws -> String {
        var request = Silo_V1_SaveSkillRequest()
        request.botID = botID
        request.path = path
        request.runID = runID
        return try unwrap(await ui.saveSkill(request: request, headers: [:])).name
    }

    // MARK: - Approvals and rules

    public func listApprovals(botID: String) async throws -> [Silo_V1_Approval] {
        var request = Silo_V1_ListApprovalsRequest()
        request.botID = botID
        return try unwrap(await ui.listApprovals(request: request, headers: [:])).approvals
    }

    /// `decision` is `allow_once`, `always`, or `deny`.
    public func decideApproval(id: String, decision: String) async throws {
        var request = Silo_V1_DecideApprovalRequest()
        request.id = id
        request.decision = decision
        _ = try unwrap(await ui.decideApproval(request: request, headers: [:]))
    }

    public func setRule(botID: String, connector: String, action: String, decision: String) async throws {
        var request = Silo_V1_SetRuleRequest()
        request.botID = botID
        request.connector = connector
        request.action = action
        request.decision = decision
        _ = try unwrap(await ui.setRule(request: request, headers: [:]))
    }

    // MARK: - Helpers

    private func unwrap<Output: ProtobufMessage>(_ response: ResponseMessage<Output>) throws -> Output {
        switch response.result {
        case .success(let message):
            return message
        case .failure(let error):
            throw SiloError.wrap(error)
        }
    }
}

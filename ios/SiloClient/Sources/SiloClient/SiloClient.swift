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

    // MARK: - Bot settings and lifecycle

    /// Send every field; a blank one clears it (so callers pass the Bot's current values through).
    public func updateBot(_ bot: Silo_V1_Bot, name: String? = nil, description: String? = nil, soul: String? = nil,
                          memory: String? = nil, model: String? = nil, autoApprove: String? = nil) async throws -> Silo_V1_Bot {
        var request = Silo_V1_UpdateBotRequest()
        request.id = bot.id
        request.name = name ?? bot.name
        request.description_p = description ?? bot.description_p
        request.soul = soul ?? bot.soul
        request.memory = memory ?? bot.memory
        request.autoApprove = autoApprove ?? bot.autoApprove
        request.model = model ?? bot.model
        return try unwrap(await ui.updateBot(request: request, headers: [:]))
    }

    public func deleteBot(_ id: String) async throws {
        var request = Silo_V1_GetBotRequest()
        request.id = id
        _ = try unwrap(await ui.deleteBot(request: request, headers: [:]))
    }

    public func resetContainer(_ id: String) async throws -> Silo_V1_Bot {
        var request = Silo_V1_GetBotRequest()
        request.id = id
        return try unwrap(await ui.resetContainer(request: request, headers: [:]))
    }

    public func listBotContainers(_ id: String) async throws -> [Silo_V1_BotContainer] {
        var request = Silo_V1_GetBotRequest()
        request.id = id
        return try unwrap(await ui.listBotContainers(request: request, headers: [:])).containers
    }

    public func removeBotContainers(_ id: String) async throws -> Silo_V1_Bot {
        var request = Silo_V1_GetBotRequest()
        request.id = id
        return try unwrap(await ui.removeBotContainers(request: request, headers: [:]))
    }

    // MARK: - Feed

    public func listFeed(botID: String) async throws -> [Silo_V1_FeedPost] {
        var request = Silo_V1_ListFeedRequest()
        request.botID = botID
        return try unwrap(await ui.listFeed(request: request, headers: [:])).posts
    }

    public func markFeedRead(botID: String) async throws {
        var request = Silo_V1_MarkFeedReadRequest()
        request.botID = botID
        _ = try unwrap(await ui.markFeedRead(request: request, headers: [:]))
    }

    public func deleteFeedPost(botID: String, id: String) async throws {
        var request = Silo_V1_DeleteFeedPostRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.deleteFeedPost(request: request, headers: [:]))
    }

    public func quoteFeedPost(botID: String, id: String) async throws -> Silo_V1_Chat {
        var request = Silo_V1_QuoteFeedPostRequest()
        request.botID = botID
        request.id = id
        return try unwrap(await ui.quoteFeedPost(request: request, headers: [:])).chat
    }

    // MARK: - Memories

    public func listMemories(botID: String) async throws -> [Silo_V1_Memory] {
        var request = Silo_V1_ListMemoriesRequest()
        request.botID = botID
        return try unwrap(await ui.listMemories(request: request, headers: [:])).memories
    }

    public func searchMemories(botID: String, query: String) async throws -> [Silo_V1_Memory] {
        var request = Silo_V1_SearchMemoriesRequest()
        request.botID = botID
        request.query = query
        request.limit = 20
        return try unwrap(await ui.searchMemories(request: request, headers: [:])).memories
    }

    public func deleteMemory(botID: String, id: String) async throws {
        var request = Silo_V1_DeleteMemoryRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.deleteMemory(request: request, headers: [:]))
    }

    // MARK: - Automations

    public func listAutomations(botID: String) async throws -> [Silo_V1_Automation] {
        var request = Silo_V1_ListAutomationsRequest()
        request.botID = botID
        return try unwrap(await ui.listAutomations(request: request, headers: [:])).automations
    }

    public func createAutomation(botID: String, name: String, prompt: String, schedule: String, enabled: Bool) async throws -> Silo_V1_Automation {
        var request = Silo_V1_CreateAutomationRequest()
        request.botID = botID
        request.name = name
        request.prompt = prompt
        request.schedule = schedule
        request.enabled = enabled
        return try unwrap(await ui.createAutomation(request: request, headers: [:]))
    }

    public func updateAutomation(botID: String, id: String, name: String, prompt: String, schedule: String, enabled: Bool) async throws -> Silo_V1_Automation {
        var request = Silo_V1_UpdateAutomationRequest()
        request.botID = botID
        request.id = id
        request.name = name
        request.prompt = prompt
        request.schedule = schedule
        request.enabled = enabled
        return try unwrap(await ui.updateAutomation(request: request, headers: [:]))
    }

    public func deleteAutomation(botID: String, id: String) async throws {
        var request = Silo_V1_DeleteAutomationRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.deleteAutomation(request: request, headers: [:]))
    }

    public func runAutomation(botID: String, id: String) async throws {
        var request = Silo_V1_RunAutomationRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.runAutomation(request: request, headers: [:]))
    }

    // MARK: - Secrets and rules

    public func listSecrets(botID: String) async throws -> [Silo_V1_SecretMeta] {
        var request = Silo_V1_ListSecretsRequest()
        request.botID = botID
        return try unwrap(await ui.listSecrets(request: request, headers: [:])).secrets
    }

    public func addSecret(botID: String, name: String, value: String) async throws {
        var request = Silo_V1_AddSecretRequest()
        request.botID = botID
        request.name = name
        request.value = value
        _ = try unwrap(await ui.addSecret(request: request, headers: [:]))
    }

    public func deleteSecret(botID: String, id: String) async throws {
        var request = Silo_V1_DeleteSecretRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.deleteSecret(request: request, headers: [:]))
    }

    public func listRuleSections(botID: String) async throws -> [Silo_V1_RuleSection] {
        var request = Silo_V1_ListRulesRequest()
        request.botID = botID
        return try unwrap(await ui.listRules(request: request, headers: [:])).sections
    }

    // MARK: - Workspace files

    public func listFiles(botID: String, path: String) async throws -> [Silo_V1_FileEntry] {
        var request = Silo_V1_ListFilesRequest()
        request.botID = botID
        request.path = path
        return try unwrap(await ui.listFiles(request: request, headers: [:])).entries
    }

    public func mkdir(botID: String, path: String) async throws {
        var request = Silo_V1_MkdirRequest()
        request.botID = botID
        request.path = path
        _ = try unwrap(await ui.mkdir(request: request, headers: [:]))
    }

    public func removeFile(botID: String, path: String) async throws {
        var request = Silo_V1_RemoveFileRequest()
        request.botID = botID
        request.path = path
        _ = try unwrap(await ui.removeFile(request: request, headers: [:]))
    }

    // MARK: - Drives

    public struct DriveList: Sendable {
        public var drives: [Silo_V1_Drive]
        public var bindOK: Bool
        public var unavailable: String
    }

    public func listDriveTemplates() async throws -> [Silo_V1_DriveTemplate] {
        try unwrap(await ui.listDriveTemplates(request: Silo_V1_ListDriveTemplatesRequest(), headers: [:])).templates
    }

    public func listDrives(botID: String, draftID: String = "") async throws -> DriveList {
        var request = Silo_V1_ListDrivesRequest()
        request.botID = botID
        request.draftID = draftID
        let response = try unwrap(await ui.listDrives(request: request, headers: [:]))
        return DriveList(drives: response.drives, bindOK: response.bindOk, unavailable: response.unavailable)
    }

    public func saveDrive(_ request: Silo_V1_SaveDriveRequest) async throws -> Silo_V1_Drive {
        try unwrap(await ui.saveDrive(request: request, headers: [:]))
    }

    public func deleteDrive(id: String) async throws {
        var request = Silo_V1_DeleteDriveRequest()
        request.id = id
        _ = try unwrap(await ui.deleteDrive(request: request, headers: [:]))
    }

    /// The URL to open in a browser to sign in to this drive's provider.
    public func beginDriveAuth(id: String) async throws -> String {
        var request = Silo_V1_BeginDriveAuthRequest()
        request.id = id
        return try unwrap(await ui.beginDriveAuth(request: request, headers: [:])).url
    }

    public func pickDriveOptions(id: String, key: String) async throws -> [Silo_V1_DriveOption] {
        var request = Silo_V1_PickDriveOptionsRequest()
        request.id = id
        request.key = key
        return try unwrap(await ui.pickDriveOptions(request: request, headers: [:])).options
    }

    public func browseDrive(id: String, path: String) async throws -> [Silo_V1_BrowseDriveDir] {
        var request = Silo_V1_BrowseDriveRequest()
        request.id = id
        request.path = path
        return try unwrap(await ui.browseDrive(request: request, headers: [:])).dirs
    }

    // MARK: - Connectors

    public func listBotConnectors(botID: String) async throws -> [Silo_V1_BotConnector] {
        var request = Silo_V1_ListBotConnectorsRequest()
        request.botID = botID
        return try unwrap(await ui.listBotConnectors(request: request, headers: [:])).connectors
    }

    public func listConnectors() async throws -> [Silo_V1_Connector] {
        try unwrap(await ui.listConnectors(request: Silo_V1_ListConnectorsRequest(), headers: [:])).connectors
    }

    public func attachConnector(botID: String, connectorID: String) async throws -> Silo_V1_BotConnector {
        var request = Silo_V1_AttachConnectorRequest()
        request.botID = botID
        request.connectorID = connectorID
        return try unwrap(await ui.attachConnector(request: request, headers: [:]))
    }

    public func detachConnector(botID: String, id: String) async throws {
        var request = Silo_V1_DetachConnectorRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.detachConnector(request: request, headers: [:]))
    }

    public func refreshBotConnector(botID: String, id: String) async throws -> Silo_V1_BotConnector {
        var request = Silo_V1_RefreshBotConnectorRequest()
        request.botID = botID
        request.id = id
        return try unwrap(await ui.refreshBotConnector(request: request, headers: [:]))
    }

    // MARK: - Channels

    public func listChannelAdapters() async throws -> [Silo_V1_ChannelAdapter] {
        try unwrap(await ui.listChannelAdapters(request: Silo_V1_ListChannelAdaptersRequest(), headers: [:])).adapters
    }

    public func listBotChannels(botID: String) async throws -> [Silo_V1_Channel] {
        var request = Silo_V1_ListBotChannelsRequest()
        request.botID = botID
        return try unwrap(await ui.listBotChannels(request: request, headers: [:])).channels
    }

    public func createChannel(_ request: Silo_V1_CreateChannelRequest) async throws -> Silo_V1_Channel {
        try unwrap(await ui.createChannel(request: request, headers: [:]))
    }

    public func updateChannel(_ request: Silo_V1_UpdateChannelRequest) async throws -> Silo_V1_Channel {
        try unwrap(await ui.updateChannel(request: request, headers: [:]))
    }

    public func deleteChannel(botID: String, id: String) async throws {
        var request = Silo_V1_DeleteChannelRequest()
        request.botID = botID
        request.id = id
        _ = try unwrap(await ui.deleteChannel(request: request, headers: [:]))
    }

    // MARK: - Skills

    public func listBotSkills(botID: String) async throws -> [Silo_V1_BotSkill] {
        var request = Silo_V1_ListBotSkillsRequest()
        request.botID = botID
        return try unwrap(await ui.listBotSkills(request: request, headers: [:])).skills
    }

    public func setBotSkill(botID: String, kind: String, name: String, enabled: Bool) async throws {
        var request = Silo_V1_SetBotSkillRequest()
        request.botID = botID
        request.kind = kind
        request.name = name
        request.enabled = enabled
        _ = try unwrap(await ui.setBotSkill(request: request, headers: [:]))
    }

    public func listSkills(scope: String) async throws -> [Silo_V1_Skill] {
        var request = Silo_V1_ListSkillsRequest()
        request.scope = scope
        return try unwrap(await ui.listSkills(request: request, headers: [:])).skills
    }

    public func installSkill(scope: String, url: String = "", archive: Data = Data(), filename: String = "") async throws -> Silo_V1_InstallSkillResponse {
        var request = Silo_V1_InstallSkillRequest()
        request.scope = scope
        request.url = url
        request.archive = archive
        request.filename = filename
        return try unwrap(await ui.installSkill(request: request, headers: [:]))
    }

    public func deleteSkill(scope: String, name: String) async throws {
        var request = Silo_V1_DeleteSkillRequest()
        request.scope = scope
        request.name = name
        _ = try unwrap(await ui.deleteSkill(request: request, headers: [:]))
    }

    public func listSkillFiles(scope: String, name: String, path: String) async throws -> [Silo_V1_FileEntry] {
        var request = Silo_V1_ListSkillFilesRequest()
        request.scope = scope
        request.name = name
        request.path = path
        return try unwrap(await ui.listSkillFiles(request: request, headers: [:])).entries
    }

    public func readSkillFile(scope: String, name: String, path: String) async throws -> Silo_V1_ReadFileResponse {
        var request = Silo_V1_ReadSkillFileRequest()
        request.scope = scope
        request.name = name
        request.path = path
        return try unwrap(await ui.readSkillFile(request: request, headers: [:]))
    }

    // MARK: - Files

    public func readFile(botID: String, path: String) async throws -> Silo_V1_ReadFileResponse {
        var request = Silo_V1_ReadFileRequest()
        request.botID = botID
        request.path = path
        return try unwrap(await ui.readFile(request: request, headers: [:]))
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

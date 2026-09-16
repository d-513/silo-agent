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

    public init(host: String) {
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

    public func send(botID: String, chatID: String, text: String) async throws -> Silo_V1_SendResponse {
        var request = Silo_V1_SendRequest()
        request.botID = botID
        request.chatID = chatID
        request.text = text
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

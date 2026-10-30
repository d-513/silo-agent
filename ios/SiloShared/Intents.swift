import AppIntents
import Foundation
import SiloClient
#if !SILO_EXTENSION
import UIKit
#endif

/// A Bot, as Shortcuts, Siri, widgets and controls know it.
struct BotEntity: AppEntity, Identifiable, Hashable {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Bot"
    static let defaultQuery = BotQuery()

    var id: String
    var name: String
    var crest: Int32

    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(name)") }

    init(id: String, name: String, crest: Int32) {
        self.id = id
        self.name = name
        self.crest = crest
    }

    init(_ bot: SharedBot) {
        self.init(id: bot.id, name: bot.name, crest: bot.crest)
    }
}

struct BotQuery: EntityStringQuery {
    func entities(for identifiers: [String]) async throws -> [BotEntity] {
        SiloStore.load().bots.filter { identifiers.contains($0.id) }.map(BotEntity.init)
    }

    func suggestedEntities() async throws -> [BotEntity] {
        SiloStore.load().bots.map(BotEntity.init)
    }

    func entities(matching string: String) async throws -> [BotEntity] {
        SiloStore.load().bots.filter { $0.name.localizedCaseInsensitiveContains(string) }.map(BotEntity.init)
    }

    func defaultResult() async -> BotEntity? {
        SiloStore.load().lastBot.map(BotEntity.init)
    }
}

/// Rings a Bot, without opening the app: the call lands on the Dynamic Island and the lock screen.
/// This is the one for the Action Button and Control Center.
struct CallBotIntent: AudioRecordingIntent, LiveActivityIntent {
    static let title: LocalizedStringResource = "Call a Bot"
    static let description = IntentDescription("Starts a voice call with a Bot, in its main chat.")
    static let supportedModes: IntentModes = .background
    // The widget extension has a copy of this that does nothing: the call has to be the app's.
    static var allowedExecutionTargets: IntentExecutionTargets { .main }

    @Parameter(title: "Bot") var bot: BotEntity?

    init() {}
    init(bot: BotEntity?) { self.bot = bot }

    func perform() async throws -> some IntentResult {
        #if !SILO_EXTENSION
        let state = SiloStore.load()
        guard let who = bot.map({ CallModel.Who(id: $0.id, name: $0.name, crest: $0.crest) })
            ?? state.lastBot.map({ CallModel.Who(id: $0.id, name: $0.name, crest: $0.crest) }) else {
            throw IntentProblem.noBot
        }
        await CallModel.shared.start(who, screen: false, host: state.server)
        #endif
        return .result()
    }
}

/// Opens the app on a Bot's page.
struct OpenBotIntent: AppIntent {
    enum Place: String, AppEnum {
        case chat, newChat, dashboard, needsYou

        static let typeDisplayRepresentation: TypeDisplayRepresentation = "Place"
        static let caseDisplayRepresentations: [Place: DisplayRepresentation] = [
            .chat: "Main chat", .newChat: "New chat", .dashboard: "Dashboard", .needsYou: "Needs you",
        ]
    }

    static let title: LocalizedStringResource = "Open a Bot"
    static let supportedModes: IntentModes = .foreground(.immediate)

    @Parameter(title: "Bot") var bot: BotEntity?
    @Parameter(title: "Place", default: .chat) var place: Place

    init() {}
    init(bot: BotEntity?, place: Place) {
        self.bot = bot
        self.place = place
    }

    func perform() async throws -> some IntentResult {
        #if !SILO_EXTENSION
        let id = bot?.id ?? SiloStore.load().lastBot?.id ?? ""
        let link: SiloLink
        switch place {
        case .chat: link = .mainChat(bot: id)
        case .newChat: link = .newChat(bot: id)
        case .dashboard: link = .dashboard(bot: id)
        case .needsYou: link = .needsYou
        }
        await AppModel.shared.open(link)
        #endif
        return .result()
    }
}

/// Asks a Bot something from Shortcuts or Siri and answers with its reply.
struct AskBotIntent: AppIntent {
    static let title: LocalizedStringResource = "Ask a Bot"
    static let description = IntentDescription("Sends a message to a Bot's main chat and waits for its reply.")
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Bot") var bot: BotEntity?
    @Parameter(title: "Message", requestValueDialog: "What should it do?") var message: String

    static var parameterSummary: some ParameterSummary {
        Summary("Ask \(\.$bot) \(\.$message)")
    }

    func perform() async throws -> some ReturnsValue<String> & ProvidesDialog {
        let state = SiloStore.load()
        guard let id = bot?.id ?? state.lastBot?.id, !state.server.isEmpty else { throw IntentProblem.noBot }
        let client = SiloClient(host: state.server)
        let sent = try await client.send(botID: id, chatID: "", text: message)
        var reply = ""
        // Shortcuts gives up on a long wait: the reply is in the chat whichever way this ends.
        let deadline = Date().addingTimeInterval(25)
        stream: for await item in client.streamRun(botID: id, chatID: sent.chatID, afterEventID: "") {
            guard Date() < deadline else { break }
            guard case .event(let event) = item, event.runID == sent.runID else { continue }
            switch event.kind {
            case "assistant", "section": reply = event.body
            case "done", "error": break stream
            default: continue
            }
        }
        let answer = reply.isEmpty ? "It is still working. The reply will be in the chat." : reply
        return .result(value: answer, dialog: "\(answer)")
    }
}

/// What a Bot is doing, for Siri or a Shortcut.
struct BotStatusIntent: AppIntent {
    static let title: LocalizedStringResource = "Check on a Bot"
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Bot") var bot: BotEntity?

    func perform() async throws -> some ReturnsValue<String> & ProvidesDialog {
        let state = await SiloFeed.fresh()
        guard let row = state.bots.first(where: { $0.id == bot?.id }) ?? state.lastBot else { throw IntentProblem.noBot }
        let line = "\(row.name): \(row.look.word)\(row.step.isEmpty ? "" : " \(row.step)")"
        return .result(value: line, dialog: "\(line)")
    }
}

/// Answers an approval in place: a widget, the Island, a notification. The server is told, and the
/// widgets redraw from what it says.
struct DecideApprovalIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Answer an approval"
    static let isDiscoverable = false

    @Parameter(title: "Approval") var approvalID: String
    @Parameter(title: "Decision") var decision: String

    init() {}
    init(approvalID: String, decision: String) {
        self.approvalID = approvalID
        self.decision = decision
    }

    func perform() async throws -> some IntentResult {
        let state = SiloStore.load()
        try await SiloClient(host: state.server).decideApproval(id: approvalID, decision: decision)
        var next = await SiloFeed.fresh(state)
        next.approvals.removeAll { $0.id == approvalID }
        SiloStore.save(next)
        return .result()
    }
}

enum IntentProblem: Error, CustomLocalizedStringResourceConvertible {
    case noBot

    var localizedStringResource: LocalizedStringResource {
        switch self {
        case .noBot: return "Open Silo and sign in first."
        }
    }
}

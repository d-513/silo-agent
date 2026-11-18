import AppIntents
import Foundation
import SiloClient
import UniformTypeIdentifiers

// Building blocks for Shortcuts and Siri, so a Bot fits into someone's own automations: answer what
// waits, hand it files, run one of its automations, start or stop its machine, read its Feed and its
// Dashboard. Each goes through the same RPCs the app uses, so the server's rules apply unchanged.

/// What waits on you, as a card with Allow once and Deny on it.
struct ReviewApprovalsIntent: AppIntent {
    static let title: LocalizedStringResource = "Review approvals"
    static let description = IntentDescription("Shows what your Bots are waiting on you to allow, and answers it in place.")
    static let supportedModes: IntentModes = .background
    static let authenticationPolicy: IntentAuthenticationPolicy = .requiresAuthentication

    func perform() async throws -> some IntentResult & ProvidesDialog & ShowsSnippetIntent {
        _ = try signedIn()
        let state = await SiloFeed.fresh()
        SiloStore.save(state)
        let dialog: IntentDialog
        switch state.approvals.count {
        case 0: dialog = "Nothing is waiting on you."
        case 1: dialog = "\(state.approvals[0].botName) needs you."
        default: dialog = "\(state.approvals.count) things are waiting on you."
        }
        return .result(dialog: dialog, snippetIntent: ApprovalsSnippet())
    }
}

enum ApprovalDecision: String, AppEnum {
    case allowOnce = "allow_once"
    case deny

    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Decision"
    static let caseDisplayRepresentations: [ApprovalDecision: DisplayRepresentation] = [
        .allowOnce: "Allow once", .deny: "Deny",
    ]
}

/// One approval, answered from a Shortcut. Allow once or Deny: Always is a rule, set in the app.
struct AnswerApprovalIntent: AppIntent {
    static let title: LocalizedStringResource = "Answer an approval"
    static let description = IntentDescription("Allows once or denies something a Bot is waiting on you for.")
    static let supportedModes: IntentModes = .background
    static let authenticationPolicy: IntentAuthenticationPolicy = .requiresAuthentication

    @Parameter(title: "Approval", requestValueDialog: "Which one?") var approval: ApprovalEntity
    @Parameter(title: "Decision", default: .allowOnce) var decision: ApprovalDecision

    static var parameterSummary: some ParameterSummary {
        Summary("\(\.$decision) \(\.$approval)")
    }

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let (client, state) = try signedIn()
        try await client.decideApproval(id: approval.id, decision: decision.rawValue)
        var next = await SiloFeed.fresh(state)
        next.approvals.removeAll { $0.id == approval.id }
        SiloStore.save(next)
        ApprovalsSnippet.reload()
        return .result(dialog: decision == .deny ? "Denied." : "Allowed once.")
    }
}

/// Files, and a line about them, into a Bot's main chat: what the share sheet does, for Shortcuts.
struct SendFilesToBotIntent: AppIntent {
    static let title: LocalizedStringResource = "Send files to a Bot"
    static let description = IntentDescription("Uploads files to a Bot and sends them to its main chat with your message.")
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Bot") var bot: BotEntity?
    @Parameter(title: "Files", supportedContentTypes: [.item]) var files: [IntentFile]
    @Parameter(title: "Message") var message: String?

    static var parameterSummary: some ParameterSummary {
        Summary("Send \(\.$files) to \(\.$bot)") { \.$message }
    }

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let (_, state) = try signedIn()
        guard let target = state.bots.first(where: { $0.id == bot?.id }) ?? state.lastBot else { throw IntentProblem.noBot }
        let stamp = Int(Date().timeIntervalSince1970)
        let items = files.enumerated().map { index, file in
            let ext = file.type?.preferredFilenameExtension.map { ".\($0)" } ?? ""
            return BotDelivery.File(name: file.filename.isEmpty ? "shortcut-\(stamp)-\(index)\(ext)" : file.filename, data: file.data)
        }
        try await BotDelivery.send(botID: target.id, text: message ?? "", files: items, server: state.server)
        return .result(dialog: "Sent to \(target.name).")
    }
}

/// Runs one of a Bot's automations now, as Run now on its page does.
struct RunAutomationIntent: AppIntent {
    static let title: LocalizedStringResource = "Run an automation"
    static let description = IntentDescription("Starts one of a Bot's automations now, outside its schedule.")
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Automation") var automation: AutomationEntity

    static var parameterSummary: some ParameterSummary { Summary("Run \(\.$automation)") }

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let (client, _) = try signedIn()
        try await client.runAutomation(botID: automation.botID, id: automation.automationID)
        return .result(dialog: "\(automation.botName) is running \(automation.name).")
    }
}

/// Starts a Bot's machine, so it is warm before you need it.
struct StartBotIntent: AppIntent {
    static let title: LocalizedStringResource = "Start a Bot's machine"
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Bot") var bot: BotEntity?

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let (client, state) = try signedIn()
        guard let target = state.bots.first(where: { $0.id == bot?.id }) ?? state.lastBot else { throw IntentProblem.noBot }
        _ = try await client.startBot(target.id)
        return .result(dialog: "Starting \(target.name).")
    }
}

/// Stops a Bot's machine. Asks first: whatever it is doing on it stops too.
struct StopBotIntent: AppIntent {
    static let title: LocalizedStringResource = "Stop a Bot's machine"
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Bot") var bot: BotEntity?

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let (client, state) = try signedIn()
        guard let target = state.bots.first(where: { $0.id == bot?.id }) ?? state.lastBot else { throw IntentProblem.noBot }
        try await requestConfirmation(dialog: "Stop \(target.name)? Whatever it is doing stops too.")
        _ = try await client.stopBot(target.id)
        return .result(dialog: "\(target.name) is stopped.")
    }
}

/// The newest post in a Bot's Feed, to read out or pass on.
struct LatestFeedPostIntent: AppIntent {
    static let title: LocalizedStringResource = "Get the latest Feed post"
    static let description = IntentDescription("Gets the newest post a Bot left in its Feed.")
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Bot") var bot: BotEntity?

    func perform() async throws -> some ReturnsValue<FeedPostEntity> & ProvidesDialog {
        let (client, state) = try signedIn()
        guard let target = state.bots.first(where: { $0.id == bot?.id }) ?? state.lastBot else { throw IntentProblem.noBot }
        guard let post = try await client.listFeed(botID: target.id).first else { throw IntentProblem.empty("\(target.name)'s Feed is empty.") }
        let entity = FeedPostEntity(post, bot: target)
        return .result(value: entity, dialog: "\(entity.spoken)")
    }
}

/// A Dashboard widget's value as one line: "Open issues: 12 (this week)".
struct GetWidgetValueIntent: AppIntent {
    static let title: LocalizedStringResource = "Get a Dashboard widget's value"
    static let description = IntentDescription("Reads one widget from a Bot's Dashboard as text.")
    static let supportedModes: IntentModes = .background

    @Parameter(title: "Bot") var bot: BotEntity?
    @Parameter(title: "Widget") var widget: DashboardWidgetEntity

    static var parameterSummary: some ParameterSummary { Summary("Get \(\.$widget)") { \.$bot } }

    func perform() async throws -> some ReturnsValue<String> & ProvidesDialog {
        let (client, _) = try signedIn()
        let board = try await client.listDashboard(botID: widget.botID)
        guard let found = board.widgets.first(where: { $0.id == widget.widgetID }) else {
            throw IntentProblem.empty("That widget is no longer on the Dashboard.")
        }
        let checked = Set((WidgetJSON.object(found.stateJson)["checked"] as? [Any])?.map { WidgetJSON.text($0) } ?? [])
        let line = widgetSpoken(kind: found.kind, title: found.title, data: WidgetJSON.object(found.dataJson), checked: checked)
        return .result(value: line, dialog: "\(line)")
    }
}

/// A chat Spotlight or Siri found, opened in the app.
struct OpenChatIntent: OpenIntent {
    static let title: LocalizedStringResource = "Open a chat"
    static let supportedModes: IntentModes = .foreground(.immediate)

    @Parameter(title: "Chat") var target: ChatEntity

    func perform() async throws -> some IntentResult {
        #if !SILO_EXTENSION
        await AppModel.shared.open(.chat(bot: target.botID, chat: target.chatID))
        #endif
        return .result()
    }
}

/// A Bot Spotlight or Siri found, opened on its main chat.
struct OpenBotEntityIntent: OpenIntent {
    static let title: LocalizedStringResource = "Show a Bot"
    static let isDiscoverable = false
    static let supportedModes: IntentModes = .foreground(.immediate)

    @Parameter(title: "Bot") var target: BotEntity

    func perform() async throws -> some IntentResult {
        #if !SILO_EXTENSION
        await AppModel.shared.open(.mainChat(bot: target.id))
        #endif
        return .result()
    }
}

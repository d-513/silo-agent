import AppIntents
import CoreSpotlight
import Foundation
import SiloClient

// What Siri, Spotlight and Shortcuts know besides a Bot: its chats (indexed, so "the invoice chat"
// is something Siri can find and open), what waits on you, its automations, its Feed and its
// Dashboard widgets. An id that names something inside a Bot carries the Bot's id first, so a
// query can ask the right Bot for it.

private let idSeparator: Character = "|"

private func entityID(_ bot: String, _ item: String) -> String { "\(bot)\(idSeparator)\(item)" }

private func entityParts(_ id: String) -> (bot: String, item: String)? {
    let parts = id.split(separator: idSeparator, maxSplits: 1).map(String.init)
    return parts.count == 2 ? (parts[0], parts[1]) : nil
}

/// The signed-in client and the Bots, or the one error every intent words the same way.
func signedIn() throws -> (SiloClient, SharedState) {
    let state = SiloStore.load()
    guard !state.server.isEmpty, state.signedIn else { throw IntentProblem.noBot }
    return (SiloClient(host: state.server), state)
}

/// Asks every Bot at once and keeps what answered: one slow or stopped Bot costs nothing.
private func eachBot<T: Sendable>(_ bots: [SharedBot], _ ask: @escaping @Sendable (SharedBot) async throws -> [T]) async -> [T] {
    await withTaskGroup(of: [T].self) { group in
        for bot in bots { group.addTask { (try? await ask(bot)) ?? [] } }
        var all: [T] = []
        for await part in group { all += part }
        return all
    }
}

extension BotEntity: IndexedEntity {
    var attributeSet: CSSearchableItemAttributeSet {
        let set = defaultAttributeSet
        set.contentDescription = "Bot"
        set.keywords = ["Silo", "Bot", name]
        return set
    }
}

// MARK: chats

struct ChatEntity: IndexedEntity, Identifiable, Hashable {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Chat"
    static let defaultQuery = ChatQuery()

    var id: String
    var title: String
    var botName: String

    var botID: String { entityParts(id)?.bot ?? "" }
    var chatID: String { entityParts(id)?.item ?? "" }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(title)", subtitle: "\(botName)")
    }

    var attributeSet: CSSearchableItemAttributeSet {
        let set = defaultAttributeSet
        set.contentDescription = "Chat with \(botName)"
        return set
    }

    init(id: String, title: String, botName: String) {
        self.id = id
        self.title = title
        self.botName = botName
    }

    init(_ chat: Silo_V1_Chat, bot: SharedBot) {
        self.init(id: entityID(bot.id, chat.id), title: chat.main ? "Main chat" : chat.title, botName: bot.name)
    }

    /// The person's own chats with a name: what is worth finding again.
    static func worthFinding(_ chat: Silo_V1_Chat) -> Bool { chat.mine && (chat.main || !chat.title.isEmpty) }
}

struct ChatQuery: EntityStringQuery {
    func entities(for identifiers: [String]) async throws -> [ChatEntity] {
        let (client, state) = try signedIn()
        let wanted = Set(identifiers.compactMap { entityParts($0)?.bot })
        return await eachBot(state.bots.filter { wanted.contains($0.id) }) { bot in
            try await client.listChats(botID: bot.id).map { ChatEntity($0, bot: bot) }
        }.filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [ChatEntity] {
        let (client, state) = try signedIn()
        guard let bot = state.lastBot else { return [] }
        return try await client.listChats(botID: bot.id).filter(ChatEntity.worthFinding).prefix(30).map { ChatEntity($0, bot: bot) }
    }

    func entities(matching string: String) async throws -> [ChatEntity] {
        let (client, state) = try signedIn()
        return await eachBot(state.bots) { bot in
            try await client.listChats(botID: bot.id).filter(ChatEntity.worthFinding).map { ChatEntity($0, bot: bot) }
        }.filter { $0.title.localizedCaseInsensitiveContains(string) || $0.botName.localizedCaseInsensitiveContains(string) }
    }
}

// MARK: approvals

struct ApprovalEntity: AppEntity, Identifiable {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Approval"
    static let defaultQuery = ApprovalQuery()

    var id: String
    @Property(title: "Action") var title: String
    @Property(title: "Bot") var botName: String
    var summary: String
    var onceOnly: Bool

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(title)", subtitle: "\(botName)")
    }

    init(_ approval: SharedApproval) {
        id = approval.id
        summary = approval.summary
        onceOnly = approval.onceOnly
        title = approval.title
        botName = approval.botName
    }
}

struct ApprovalQuery: EntityQuery {
    func entities(for identifiers: [String]) async throws -> [ApprovalEntity] {
        try await suggestedEntities().filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [ApprovalEntity] {
        _ = try signedIn()
        return await SiloFeed.fresh().approvals.map(ApprovalEntity.init)
    }
}

// MARK: automations

struct AutomationEntity: AppEntity, Identifiable, Hashable {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Automation"
    static let defaultQuery = AutomationQuery()

    var id: String
    var name: String
    var botName: String

    var botID: String { entityParts(id)?.bot ?? "" }
    var automationID: String { entityParts(id)?.item ?? "" }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)", subtitle: "\(botName)")
    }

    init(_ automation: Silo_V1_Automation, bot: SharedBot) {
        id = entityID(bot.id, automation.id)
        name = automation.name
        botName = bot.name
    }
}

struct AutomationQuery: EntityStringQuery {
    func entities(for identifiers: [String]) async throws -> [AutomationEntity] {
        let (client, state) = try signedIn()
        let wanted = Set(identifiers.compactMap { entityParts($0)?.bot })
        return await eachBot(state.bots.filter { wanted.contains($0.id) }) { bot in
            try await client.listAutomations(botID: bot.id).map { AutomationEntity($0, bot: bot) }
        }.filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [AutomationEntity] {
        let (client, state) = try signedIn()
        return await eachBot(state.bots) { bot in
            try await client.listAutomations(botID: bot.id).map { AutomationEntity($0, bot: bot) }
        }.sorted { ($0.botName, $0.name) < ($1.botName, $1.name) }
    }

    func entities(matching string: String) async throws -> [AutomationEntity] {
        try await suggestedEntities().filter { $0.name.localizedCaseInsensitiveContains(string) }
    }
}

// MARK: the Feed

struct FeedPostEntity: AppEntity, Identifiable {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Feed post"
    static let defaultQuery = FeedPostQuery()

    var id: String
    @Property(title: "Title") var title: String
    @Property(title: "Text") var body: String
    @Property(title: "Bot") var botName: String
    @Property(title: "From") var source: String
    @Property(title: "Posted") var posted: Date?

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(title.isEmpty ? String(body.prefix(60)) : title)", subtitle: "\(botName)")
    }

    init(_ post: Silo_V1_FeedPost, bot: SharedBot) {
        id = entityID(bot.id, post.id)
        title = post.title
        body = post.body
        botName = bot.name
        source = post.sourceName.isEmpty ? post.sourceKind : post.sourceName
        posted = isoDate(post.createdAt)
    }

    /// What Siri says of it: the title, then the start of the post.
    var spoken: String {
        let text = body.replacingOccurrences(of: "\n", with: " ")
        let start = text.count > 280 ? String(text.prefix(280)) + "…" : text
        return title.isEmpty ? start : "\(title). \(start)"
    }
}

struct FeedPostQuery: EntityQuery {
    func entities(for identifiers: [String]) async throws -> [FeedPostEntity] {
        let (client, state) = try signedIn()
        let wanted = Set(identifiers.compactMap { entityParts($0)?.bot })
        return await eachBot(state.bots.filter { wanted.contains($0.id) }) { bot in
            try await client.listFeed(botID: bot.id).map { FeedPostEntity($0, bot: bot) }
        }.filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [FeedPostEntity] {
        let (client, state) = try signedIn()
        guard let bot = state.lastBot else { return [] }
        return try await client.listFeed(botID: bot.id).prefix(20).map { FeedPostEntity($0, bot: bot) }
    }
}

// MARK: Dashboard widgets

struct DashboardWidgetEntity: AppEntity, Identifiable, Hashable {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Dashboard widget"
    static let defaultQuery = DashboardWidgetQuery()

    var id: String
    var title: String
    var botName: String

    var botID: String { entityParts(id)?.bot ?? "" }
    var widgetID: String { entityParts(id)?.item ?? "" }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(title)", subtitle: "\(botName)")
    }

    init(_ widget: Silo_V1_DashboardWidget, bot: SharedBot) {
        id = entityID(bot.id, widget.id)
        title = widget.title.isEmpty ? widget.kind : widget.title
        botName = bot.name
    }
}

struct DashboardWidgetQuery: EntityQuery {
    @IntentParameterDependency<GetWidgetValueIntent>(\.$bot) var chosen

    func entities(for identifiers: [String]) async throws -> [DashboardWidgetEntity] {
        let (client, state) = try signedIn()
        let wanted = Set(identifiers.compactMap { entityParts($0)?.bot })
        return await eachBot(state.bots.filter { wanted.contains($0.id) }) { bot in
            try await client.listDashboard(botID: bot.id).widgets.map { DashboardWidgetEntity($0, bot: bot) }
        }.filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [DashboardWidgetEntity] {
        let (client, state) = try signedIn()
        let pick: BotEntity? = chosen?.bot
        let bots = pick.map { pick in state.bots.filter { $0.id == pick.id } } ?? state.bots
        return await eachBot(bots) { bot in
            try await client.listDashboard(botID: bot.id).widgets.filter { $0.kind != "app" }.map { DashboardWidgetEntity($0, bot: bot) }
        }
    }
}

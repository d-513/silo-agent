import AppIntents
import Foundation
import SiloClient

// The Bot as App Intents know it, apart from the iPhone's intents so the watch and its
// complications share it.

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

enum IntentProblem: Error, CustomLocalizedStringResourceConvertible {
    case noBot
    /// Nothing to give back, said in words: "Ada's Feed is empty."
    case empty(String)

    var localizedStringResource: LocalizedStringResource {
        switch self {
        case .noBot: return "Open Silo and sign in first."
        case .empty(let words): return "\(words)"
        }
    }
}

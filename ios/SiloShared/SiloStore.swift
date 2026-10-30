import Foundation
import SiloClient

/// One Bot as a widget, the lock screen or the share sheet needs it: nothing here is more than a
/// glance, and it is all worked out by the app so an extension never has to ask.
struct SharedBot: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var crest: Int32
    var spaceName = ""
    var look: StatusLook
    /// Its place on the taskboard, "2/5".
    var step = ""
    var needs = 0
    var asks = 0
    var unread = 0
    var mainChatID = ""
    var nextName = ""
    var nextAt: Date?
}

/// An approval a widget or a notification can answer in place.
struct SharedApproval: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var botID: String
    var botName: String
    var crest: Int32
    var title: String
    var summary: String
    var onceOnly = false
}

struct SharedState: Codable, Sendable, Equatable {
    var server = ""
    var email = ""
    var callsOn = false
    var bots: [SharedBot] = []
    var approvals: [SharedApproval] = []
    var at = Date.distantPast
    /// The Bot the app was last on: the default for "call" and "ask".
    var lastBotID = ""

    var signedIn: Bool { !email.isEmpty }
    var lastBot: SharedBot? { bots.first { $0.id == lastBotID } ?? bots.first }
}

enum SiloStore {
    private static let key = "shared.state"

    static func load() -> SharedState {
        guard let data = SiloGroup.defaults.data(forKey: key),
              let state = try? JSONDecoder().decode(SharedState.self, from: data) else { return SharedState() }
        return state
    }

    /// Writes it, and reports whether anything a widget shows changed.
    @discardableResult
    static func save(_ state: SharedState) -> Bool {
        var before = load()
        before.at = state.at
        guard before != state, let data = try? JSONEncoder().encode(state) else { return false }
        SiloGroup.defaults.set(data, forKey: key)
        return true
    }
}

extension SharedBot {
    /// A Bot as the signed-in person sees it now.
    init(_ bot: Silo_V1_Bot, approvals: Int = 0, mainChatID: String = "", spaceName: String = "") {
        id = bot.id
        name = bot.name
        crest = bot.crest
        self.spaceName = spaceName
        look = botLook(bot)
        step = bot.activity.first.map { progress($0) } ?? ""
        needs = approvals
        asks = Int(bot.dashboardAsks)
        unread = Int(bot.feedUnread)
        self.mainChatID = mainChatID
        nextName = bot.nextRunName
        nextAt = isoDate(bot.nextRunAt)
    }
}

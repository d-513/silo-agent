import Foundation
import SiloClient

/// What the extensions and background refreshes ask the server for: the Bots at a glance, and what
/// waits for you. A failure leaves the last state standing.
enum SiloFeed {
    static func fresh(_ base: SharedState = SiloStore.load()) async -> SharedState {
        guard !base.server.isEmpty else { return base }
        let client = SiloClient(host: base.server)
        guard let bots = try? await client.listBots() else { return base }
        var state = base
        var approvals: [SharedApproval] = []
        var rows: [SharedBot] = []
        for bot in bots {
            var row = SharedBot(bot)
            if let old = base.bots.first(where: { $0.id == bot.id }) {
                row.mainChatID = old.mainChatID
                row.spaceName = old.spaceName
            }
            if botLook(bot).lamp == .needsYou, let list = try? await client.listApprovals(botID: bot.id) {
                row.needs = list.count
                approvals += list.map { SharedApproval($0, bot: bot) }
            }
            rows.append(row)
        }
        state.bots = rows
        state.approvals = approvals
        state.at = Date()
        return state
    }
}

extension SharedApproval {
    init(_ approval: Silo_V1_Approval, bot: Silo_V1_Bot) {
        let words = describeApproval(approval)
        id = approval.id
        botID = bot.id
        botName = bot.name
        crest = bot.crest
        title = words.title
        summary = words.summary
        onceOnly = approval.onceOnly || approval.auto
    }
}

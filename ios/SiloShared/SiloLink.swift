import Foundation

/// Where a widget, a quick action, a notification or Spotlight sends you: `silo://…` inside the
/// app. One place to read and write them, so a surface never builds a URL by hand.
enum SiloLink: Equatable, Hashable {
    case chat(bot: String, chat: String)
    case mainChat(bot: String)
    case newChat(bot: String)
    case call(bot: String)
    case dashboard(bot: String)
    case feed(bot: String)
    case needsYou

    var url: URL {
        var parts = URLComponents()
        parts.scheme = "silo"
        switch self {
        case .chat(let bot, let chat):
            parts.host = "bot"
            parts.path = "/\(bot)/chat/\(chat)"
        case .mainChat(let bot):
            parts.host = "bot"
            parts.path = "/\(bot)/main"
        case .newChat(let bot):
            parts.host = "bot"
            parts.path = "/\(bot)/new"
        case .call(let bot):
            parts.host = "bot"
            parts.path = "/\(bot)/call"
        case .dashboard(let bot):
            parts.host = "bot"
            parts.path = "/\(bot)/dashboard"
        case .feed(let bot):
            parts.host = "bot"
            parts.path = "/\(bot)/feed"
        case .needsYou:
            parts.host = "needs-you"
        }
        return parts.url!
    }

    init?(url: URL) {
        guard url.scheme == "silo" else { return nil }
        if url.host == "needs-you" {
            self = .needsYou
            return
        }
        let path = url.pathComponents.filter { $0 != "/" }
        guard url.host == "bot", let bot = path.first else { return nil }
        switch path.dropFirst().first {
        case "chat":
            guard path.count > 2 else { return nil }
            self = .chat(bot: bot, chat: path[2])
        case "main", nil: self = .mainChat(bot: bot)
        case "new": self = .newChat(bot: bot)
        case "call": self = .call(bot: bot)
        case "dashboard": self = .dashboard(bot: bot)
        case "feed": self = .feed(bot: bot)
        default: return nil
        }
    }
}

import Foundation

/// Which of a Bot's chats are tabs. Port of `web/src/bot/tabOrder.ts`.

/// What the tab order reads off a chat.
public protocol TabChat {
    var id: String { get }
    var main: Bool { get }
    var mine: Bool { get }
    var hidden: Bool { get }
    var createdAt: String { get }
}

extension Silo_V1_Chat: TabChat {}

/// The side chats shown as tabs, after Dashboard and the main chat: yours that are not closed, in
/// the order they were made, so a new one opens at the end and a reply never moves a tab. The open
/// chat is a tab for as long as it is open, even a closed one or someone else's (their main chat
/// too); it sits last. Your own main chat has its standing tab.
public func chatTabs<T: TabChat>(_ chats: [T], open openID: String? = nil) -> [T] {
    func made(_ chat: T) -> Double { isoDate(chat.createdAt)?.timeIntervalSince1970 ?? 0 }
    var tabs = chats.enumerated()
        .filter { $0.element.mine && !$0.element.main && !$0.element.hidden }
        .sorted { a, b in
            let x = made(a.element), y = made(b.element)
            return x != y ? x < y : a.offset > b.offset
        }
        .map(\.element)
    if let openID, let open = chats.first(where: { $0.id == openID }),
       !(open.main && open.mine), !tabs.contains(where: { $0.id == open.id }) {
        tabs.append(open)
    }
    return tabs
}

/// Your chats behind the arrow: the ones you closed, as listed (newest activity first).
public func closedChats<T: TabChat>(_ chats: [T]) -> [T] {
    chats.filter { $0.mine && !$0.main && $0.hidden }
}

/// Where closing a tab leads: the tab after it, else the one before, else "" (the main chat).
public func afterClose(_ tabs: [String], closing: String) -> String {
    guard let i = tabs.firstIndex(of: closing) else { return "" }
    if i + 1 < tabs.count { return tabs[i + 1] }
    if i > 0 { return tabs[i - 1] }
    return ""
}

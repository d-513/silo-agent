import Foundation

// The composer's slash commands. The server runs them (`Send`); this is the menu and the words for
// what a command did. Port of `web/src/composer/slash.ts` and `saidFor` in `bot/runActions.ts`.

/// The command name being typed ("cl" for "/cl"), or nil when the text is not a bare "/name".
public func slashQuery(_ text: String) -> String? {
    guard text.hasPrefix("/") else { return nil }
    let name = text.dropFirst()
    guard name.allSatisfy({ $0.isASCII && $0.isLetter }) else { return nil }
    return name.lowercased()
}

/// The commands whose name starts with `query`, in the server's order.
public func matchCommands(_ commands: [Silo_V1_Command], _ query: String) -> [Silo_V1_Command] {
    commands.filter { $0.name.hasPrefix(query) }
}

/// Whether a message would be read as a command: "/name" at the very start, not "//" (escaped),
/// and nothing attached.
public func looksLikeCommand(_ text: String, attachments: Int) -> Bool {
    text.hasPrefix("/") && !text.hasPrefix("//") && attachments == 0
}

/// What a command did, from its name and value; the server's English notice is the fallback.
/// Empty means there is nothing worth saying (a compaction shows in the thread).
public func commandSaid(_ response: Silo_V1_SendResponse) -> String {
    let v = response.value
    switch response.command {
    case "clear":
        return v == "empty" ? "The context is already empty." : "Context cleared. The Bot starts fresh from here; its memories stay."
    case "wipe":
        let n = Int(v) ?? 0
        return n == 0 ? "There is nothing to wipe." : "Wiped \(n) \(n == 1 ? "message" : "messages")."
    case "model":
        return v.isEmpty ? response.notice : "This chat now uses \(v)."
    case "thinking":
        return v.isEmpty ? response.notice : "Thinking: \(v)."
    case "stop":
        return v == "idle" ? "Nothing is running." : "Stopped."
    case "rename":
        return "Renamed to \(v)."
    case "remember":
        let parts = v.split(separator: "/").map { Int($0) ?? 0 }
        let saved = parts[safeAt: 0] ?? 0, updated = parts[safeAt: 1] ?? 0, forgotten = parts[safeAt: 2] ?? 0
        if saved == 0 && updated == 0 && forgotten == 0 { return "Nothing new to remember." }
        return "Memories: \(saved) saved, \(updated) updated, \(forgotten) forgotten."
    case "compact":
        return ""
    default:
        return response.notice
    }
}

/// The question a destroying command asks before it is sent again with `confirmed`.
public func commandConfirm(_ response: Silo_V1_SendResponse) -> String {
    let n = Int(response.value) ?? 0
    if response.command == "wipe" {
        return "Delete \(n) \(n == 1 ? "message" : "messages") for good? Tool output and files shown in them go too; memories stay."
    }
    return response.confirm
}

extension Array {
    subscript(safeAt index: Int) -> Element? { indices.contains(index) ? self[index] : nil }
}

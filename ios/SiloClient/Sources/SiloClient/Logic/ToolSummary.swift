import Foundation

/// Reads the string value of `"key":"…"` from possibly-partial streamed JSON. Returns the text so
/// far when the closing quote has not arrived yet. Mirrors `extractStringField` in the web thread.
public func extractStringField(_ raw: String, _ key: String) -> String? {
    guard let marker = raw.range(of: "\"\(key)\"") else { return nil }
    var index = marker.upperBound
    while index < raw.endIndex, raw[index].isWhitespace { index = raw.index(after: index) }
    guard index < raw.endIndex, raw[index] == ":" else { return nil }
    index = raw.index(after: index)
    while index < raw.endIndex, raw[index].isWhitespace { index = raw.index(after: index) }
    guard index < raw.endIndex, raw[index] == "\"" else { return nil }
    index = raw.index(after: index)
    var out = ""
    while index < raw.endIndex {
        let char = raw[index]
        if char == "\"" { return out }
        if char == "\\" {
            let next = raw.index(after: index)
            guard next < raw.endIndex else { break }
            switch raw[next] {
            case "n": out.append("\n")
            case "t": out.append("\t")
            case "r": out.append("\r")
            case "\"": out.append("\"")
            case "\\": out.append("\\")
            case "/": out.append("/")
            default: out.append(raw[next])
            }
            index = raw.index(after: next)
            continue
        }
        out.append(char)
        index = raw.index(after: index)
    }
    return out
}

/// Tool arguments as flat strings/numbers, tolerating a half-streamed object.
public func parseToolArgs(_ raw: String) -> [String: String] {
    guard !raw.isEmpty else { return [:] }
    var out: [String: String] = [:]
    if let data = raw.data(using: .utf8), let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
        for (key, value) in object {
            if let string = value as? String {
                out[key] = string
            } else if let number = value as? NSNumber {
                out[key] = number.stringValue
            } else if let array = value as? [Any] {
                out[key] = "[\(array.count)]"
            }
        }
        return out
    }
    for key in ["code", "command", "content", "path", "pattern", "old_text", "new_text", "include", "text", "query", "channel", "to", "chat", "name", "model"] {
        if let value = extractStringField(raw, key) { out[key] = value }
    }
    return out
}

private func firstLine(_ text: String) -> String {
    text.split(separator: "\n").map { $0.trimmingCharacters(in: .whitespaces) }.first { !$0.isEmpty } ?? ""
}

/// The one-line subject of a tool row: the file, command, query, or agent it acted on.
public func toolAction(name: String, args raw: String) -> String {
    let a = parseToolArgs(raw)
    let path = a["path"] ?? ""
    switch name {
    case "exec_python": return firstLine(a["code"] ?? "")
    case "terminal":
        let command = firstLine(a["command"] ?? "")
        return command.isEmpty ? "" : "$ \(command)"
    case "read", "write", "patch", "delete": return "\(name) \(path)".trimmingCharacters(in: .whitespaces)
    case "present": return path
    case "grep": return "grep \(a["pattern"] ?? "")".trimmingCharacters(in: .whitespaces)
    case "scroll": return a["dy"].map { "scroll \($0)" } ?? "scroll"
    case "type": return "type"
    case "web_search": return a["query"] ?? ""
    case "skill": return (a["name"]).flatMap { $0.isEmpty ? nil : $0 } ?? path
    case "channel": return (a["channel"]).flatMap { $0.isEmpty ? nil : $0 } ?? (a["to"] ?? "")
    case "list_models": return "list"
    case "switch_model": return a["model"] ?? ""
    case "spawn_agent": return "start \(a["name"] ?? "")".trimmingCharacters(in: .whitespaces)
    case "agent_status": return (a["name"] ?? "").isEmpty ? "check all" : "check \(a["name"]!)"
    case "message_agent": return "message \(a["name"] ?? "")".trimmingCharacters(in: .whitespaces)
    case "stop_agent": return "stop \(a["name"] ?? "")".trimmingCharacters(in: .whitespaces)
    case "sleep": return a["seconds"].map { "\($0)s" } ?? ""
    case "task_list": return "read"
    case "task_reset": return "reset"
    case "click": return a["x"].map { "click \($0),\(a["y"] ?? "")" } ?? "click"
    case "key": return "key \(a["name"] ?? "")".trimmingCharacters(in: .whitespaces)
    default:
        // A connector tool the model called directly: `<slug>__<action>`.
        guard let range = name.range(of: "__"), range.lowerBound != name.startIndex, range.upperBound != name.endIndex else { return "" }
        return name[range.upperBound...].replacingOccurrences(of: "_", with: " ").trimmingCharacters(in: .whitespaces)
    }
}

/// True for `bot/…` (the Bot's scratch space), which `present` shows as a quiet row, not a card.
public func isBotScratch(_ path: String) -> Bool {
    var p = path
    while p.hasPrefix("/") { p.removeFirst() }
    while p.hasPrefix("workspace/") { p.removeFirst("workspace/".count) }
    return p == "bot" || p.hasPrefix("bot/")
}

/// The app a tool belongs to, as the web's `toolMeta`: "Python", "Files", "Desktop", …; a
/// connector tool (`slug__action`) is its connector's slug.
public func toolApp(_ name: String) -> String {
    switch name {
    case "exec_python": return "Python"
    case "terminal": return "Terminal"
    case "read", "write", "patch", "grep", "delete", "present": return "Files"
    case "look", "click", "type", "key", "scroll": return "Desktop"
    case "soul": return "SOUL"
    case "skill": return "Skills"
    case "artifact": return "Artifact"
    case "web_search": return "Web search"
    case "channel": return "Channels"
    case "chats": return "Chats"
    case "feed": return "Feed"
    case "list_models", "switch_model": return "Model"
    case "decide": return "Decision"
    case "spawn_agent", "agent_status", "message_agent", "stop_agent": return "Subagents"
    case "sleep": return "Sleep"
    case "task_add", "task_list", "task_done", "task_reset": return "Taskboard"
    default:
        if let range = name.range(of: "__") { return String(name[..<range.lowerBound]) }
        return name
    }
}

/// The glyph of a tool row, by what the tool works on.
public func toolSymbol(_ name: String) -> String {
    switch name {
    case "exec_python": return "chevron.left.forwardslash.chevron.right"
    case "terminal": return "terminal"
    case "read", "write", "patch", "delete", "grep", "present": return "doc.text"
    case "look", "click", "type", "key", "scroll": return "display"
    case "web_search": return "globe"
    case "soul": return "heart.text.square"
    case "skill": return "book.closed"
    case "artifact": return "shippingbox"
    case "channel": return "antenna.radiowaves.left.and.right"
    case "chats": return "bubble.left.and.bubble.right"
    case "feed": return "tray"
    case "list_models", "switch_model": return "cpu"
    case "decide": return "arrow.triangle.branch"
    case "spawn_agent", "agent_status", "message_agent", "stop_agent": return "person.2"
    case "sleep": return "moon"
    case "task_add", "task_list", "task_done", "task_reset": return "checklist"
    default: return "powerplug"
    }
}

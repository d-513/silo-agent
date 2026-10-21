import Foundation

// What the Dashboard's widgets read from the server's JSON. Port of `web/src/dashboard/widgets.ts`
// and `web/src/dashboardLayout.ts`; keep them in step. The views draw, these decide.
public enum WidgetJSON {
    /// A JSON object, or [:] for anything else.
    public static func object(_ json: String) -> [String: Any] {
        guard !json.isEmpty, let data = json.data(using: .utf8),
              let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return [:] }
        return value
    }

    /// The objects in a list; anything else is skipped.
    public static func items(_ value: Any?) -> [[String: Any]] {
        (value as? [Any])?.compactMap { $0 as? [String: Any] } ?? []
    }

    public static func text(_ value: Any?) -> String {
        switch value {
        case let s as String: return s
        case let n as NSNumber:
            if CFGetTypeID(n) == CFBooleanGetTypeID() { return n.boolValue ? "yes" : "no" }
            return n.doubleValue.isFinite ? format(n.doubleValue) : ""
        default: return ""
        }
    }

    public static func number(_ value: Any?) -> Double? {
        switch value {
        case let n as NSNumber:
            if CFGetTypeID(n) == CFBooleanGetTypeID() { return nil }
            return n.doubleValue.isFinite ? n.doubleValue : nil
        case let s as String:
            let t = s.trimmingCharacters(in: .whitespaces)
            guard !t.isEmpty, let d = Double(t), d.isFinite else { return nil }
            return d
        default: return nil
        }
    }

    /// A number as people read it: no trailing ".0", at most two decimals, grouped.
    public static func format(_ value: Double) -> String {
        value.formatted(.number.precision(.fractionLength(0...2)))
    }

    /// A value shown as written when it is text, formatted when it is a number.
    public static func display(_ value: Any?) -> String {
        if value is String { return text(value) }
        if let n = number(value) { return format(n) }
        return text(value)
    }

    public static func encode(_ value: Any) -> String {
        guard JSONSerialization.isValidJSONObject(value),
              let data = try? JSONSerialization.data(withJSONObject: value, options: [.sortedKeys])
        else { return "{}" }
        return String(decoding: data, as: UTF8.self)
    }
}

/// "now", "4 m", "2 h", "3 d": how old a widget's data is.
public func widgetAge(_ iso: String, now: Date = Date()) -> String {
    guard let date = parseISO(iso) else { return "" }
    let s = max(0, now.timeIntervalSince(date))
    if s < 60 { return "now" }
    if s < 3600 { return "\(Int(s / 60)) m" }
    if s < 86400 { return "\(Int(s / 3600)) h" }
    return "\(Int(s / 86400)) d"
}

/// "15 m", "2 h", "1 d" for a probe interval in seconds.
public func widgetInterval(_ seconds: Int) -> String {
    if seconds <= 0 { return "" }
    if seconds % 86400 == 0 { return "\(seconds / 86400) d" }
    if seconds % 3600 == 0 { return "\(seconds / 3600) h" }
    return "\(Int((Double(seconds) / 60).rounded())) m"
}

public struct WidgetAction: Equatable, Sendable, Identifiable {
    public var id: String
    public var label: String
    /// `run` executes the widget's stored code; `hand` opens a chat about it.
    public var hand: Bool
    /// Shown on each list/table row instead of under the widget.
    public var row: Bool
    public var confirm: Bool
}

/// The buttons a widget's spec declares.
public func widgetActions(_ specJSON: String) -> [WidgetAction] {
    WidgetJSON.items(WidgetJSON.object(specJSON)["actions"]).compactMap { a in
        let id = WidgetJSON.text(a["id"])
        let label = WidgetJSON.text(a["label"])
        guard !id.isEmpty, !label.isEmpty else { return nil }
        return WidgetAction(id: id, label: label, hand: (a["kind"] as? String) == "hand",
                            row: (a["row"] as? Bool) == true, confirm: (a["confirm"] as? Bool) == true)
    }
}

/// The ids the owner checked on a checklist.
public func widgetChecked(_ stateJSON: String) -> [String] {
    (WidgetJSON.object(stateJSON)["checked"] as? [Any])?.map { WidgetJSON.text($0) } ?? []
}

/// The state after the owner checks or unchecks one item.
public func widgetToggle(_ stateJSON: String, id: String) -> String {
    var state = WidgetJSON.object(stateJSON)
    var on = widgetChecked(stateJSON)
    if let i = on.firstIndex(of: id) { on.remove(at: i) } else { on.append(id) }
    state["checked"] = on
    return WidgetJSON.encode(state)
}

public struct WidgetReceipt: Equatable, Sendable {
    public var text: String
    public var ok: Bool
    public var at: String
    public var by: String
}

public func widgetReceipt(_ json: String) -> WidgetReceipt? {
    let r = WidgetJSON.object(json)
    let text = WidgetJSON.text(r["text"])
    guard !text.isEmpty else { return nil }
    let by = WidgetJSON.text(r["by"])
    return WidgetReceipt(text: text, ok: (r["ok"] as? Bool) != false, at: WidgetJSON.text(r["at"]), by: by.isEmpty ? "you" : by)
}

public enum WidgetTone: String, Sendable { case ok, warn, down, off }

public func widgetTone(_ state: Any?) -> WidgetTone {
    WidgetTone(rawValue: (state as? String) ?? "") ?? .ok
}

/// Where the "now" rule sits in a timeline: how many items are past. -1 when no item is today or a
/// time cannot be read.
public func widgetNowSplit(_ times: [String], now: Date = Date(), calendar: Calendar = .current) -> Int {
    var past = 0
    var today = false
    for t in times {
        guard let date = parseISO(t) else { return -1 }
        if calendar.isDate(date, inSameDayAs: now) { today = true }
        if date <= now { past += 1 }
    }
    return today ? past : -1
}

/// A link a widget may open: http(s) only.
public func widgetURL(_ value: Any?) -> URL? {
    guard let s = value as? String, let url = URL(string: s), let scheme = url.scheme?.lowercased(),
          scheme == "http" || scheme == "https", url.host != nil
    else { return nil }
    return url
}

/// A table cell that is a number reads right-aligned in mono.
public func widgetNumeric(_ value: Any?) -> Bool {
    if let n = value as? NSNumber { return CFGetTypeID(n) != CFBooleanGetTypeID() }
    guard let s = value as? String else { return false }
    let t = s.trimmingCharacters(in: .whitespaces)
    return t.range(of: #"^[-+]?[\d.,\s]+\s?(%|[a-zA-Z]{1,3})?$"#, options: .regularExpression) != nil
        && t.rangeOfCharacter(from: .decimalDigits) != nil
}

func parseISO(_ iso: String) -> Date? {
    guard !iso.isEmpty else { return nil }
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return fractional.date(from: iso) ?? ISO8601DateFormatter().date(from: iso)
}

// MARK: - Asks (the Bot's question cards)

/// One display block or form control on an ask. Port of `DashboardBlock`.
public struct AskBlock: Sendable, Identifiable {
    public struct Option: Sendable, Hashable {
        public var value: String
        public var label: String
        public var hint: String
    }

    public struct Pair: Sendable, Hashable {
        public var label: String
        public var value: String
        public var delta = ""
        public var tone = ""
    }

    public var id: Int
    public var type: String
    public var name = ""
    public var label = ""
    public var hint = ""
    public var placeholder = ""
    public var text = ""
    public var path = ""
    public var options: [Option] = []
    public var pairs: [Pair] = []

    /// A control the owner answers (its value goes into the submitted form).
    public var isControl: Bool {
        ["switch", "select", "segmented", "checkboxes", "input", "textarea"].contains(type) && !name.isEmpty
    }
}

public enum AskValue: Sendable, Equatable {
    case bool(Bool)
    case string(String)
    case list([String])

    var json: Any {
        switch self {
        case .bool(let b): return b
        case .string(let s): return s
        case .list(let l): return l
        }
    }
}

public func parseAskBlocks(_ json: String) -> (blocks: [AskBlock], defaults: [String: AskValue]) {
    guard let data = json.data(using: .utf8), let raw = try? JSONSerialization.jsonObject(with: data) as? [Any] else {
        return ([], [:])
    }
    var blocks: [AskBlock] = []
    var defaults: [String: AskValue] = [:]
    for (i, item) in raw.enumerated() {
        guard let b = item as? [String: Any], let type = b["type"] as? String else { continue }
        var block = AskBlock(id: i, type: type)
        block.name = WidgetJSON.text(b["name"])
        block.label = WidgetJSON.text(b["label"])
        block.hint = WidgetJSON.text(b["hint"])
        block.placeholder = WidgetJSON.text(b["placeholder"])
        block.text = WidgetJSON.text(b["text"])
        block.path = WidgetJSON.text(b["path"])
        if type == "diff" {
            let patch = WidgetJSON.text(b["patch"])
            block.text = patch.isEmpty ? WidgetJSON.text(b["after"]) : patch
        }
        block.options = ((b["options"] as? [Any]) ?? []).compactMap { o in
            if let s = o as? String { return .init(value: s, label: s, hint: "") }
            guard let d = o as? [String: Any], let v = d["value"] as? String else { return nil }
            let l = WidgetJSON.text(d["label"])
            return .init(value: v, label: l.isEmpty ? v : l, hint: WidgetJSON.text(d["hint"]))
        }
        block.pairs = WidgetJSON.items(b["items"]).map {
            .init(label: WidgetJSON.text($0["label"]), value: WidgetJSON.text($0["value"]),
                  delta: WidgetJSON.text($0["delta"]), tone: WidgetJSON.text($0["tone"]))
        }
        if block.isControl {
            switch type {
            case "switch": defaults[block.name] = .bool((b["default"] as? Bool) == true)
            case "select", "segmented":
                defaults[block.name] = .string((b["default"] as? String) ?? block.options.first?.value ?? "")
            case "checkboxes": defaults[block.name] = .list(((b["default"] as? [Any]) ?? []).map { WidgetJSON.text($0) })
            default: defaults[block.name] = .string((b["default"] as? String) ?? "")
            }
        }
        blocks.append(block)
    }
    return (blocks, defaults)
}

public func encodeAskValues(_ values: [String: AskValue]) -> String {
    WidgetJSON.encode(values.mapValues(\.json))
}

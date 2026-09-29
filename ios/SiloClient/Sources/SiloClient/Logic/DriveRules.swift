import Foundation

/// Rules for the Drives add form, mirrored from `web/src/BotDrives.tsx`.
public enum DriveRules {
    /// A mount name: lowercase letters, digits, dashes; 1–40 chars, not starting with a dash.
    public static func isValidName(_ name: String) -> Bool {
        guard (1...40).contains(name.count), let first = name.first, first.isLowercase || first.isNumber,
              first.isASCII else { return false }
        return name.allSatisfy { ($0.isASCII && ($0.isLowercase || $0.isNumber)) || $0 == "-" }
    }

    /// "My Drive" → "my-drive", "my-drive-2" when taken.
    public static func uniqueName(_ title: String, taken: Set<String>) -> String {
        var base = title.lowercased().map { $0.isASCII && ($0.isLetter || $0.isNumber) ? String($0) : "-" }.joined()
        while base.contains("--") { base = base.replacingOccurrences(of: "--", with: "-") }
        base = base.trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        if base.isEmpty { base = "drive" }
        base = String(base.prefix(36))
        if !taken.contains(base) { return base }
        var n = 2
        while taken.contains("\(base)-\(n)") { n += 1 }
        return "\(base)-\(n)"
    }

    /// A var shows only when every `visible_if` key currently has (or defaults to) the wanted value.
    public static func isVisible(visibleIf: [String: String], values: [String: String], defaults: [String: String]) -> Bool {
        for (key, want) in visibleIf {
            let current = values[key].flatMap { $0.isEmpty ? nil : $0 } ?? defaults[key] ?? ""
            if current != want { return false }
        }
        return true
    }
}

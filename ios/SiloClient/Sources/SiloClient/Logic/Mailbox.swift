import Foundation

// Pure helpers for the Mail page. Port of `web/src/mailbox.ts`; keep them in step.

/// The name a From header shows in a list: the display name, else the address.
public func mailSenderName(from: String, address: String) -> String {
    let pattern = try! NSRegularExpression(pattern: #"^\s*"?([^"<]*?)"?\s*<[^>]*>\s*$"#)
    if let match = pattern.firstMatch(in: from, range: NSRange(from.startIndex..., in: from)),
       let range = Range(match.range(at: 1), in: from) {
        let name = from[range].trimmingCharacters(in: .whitespaces)
        if !name.isEmpty { return name }
    }
    if !address.isEmpty { return address }
    let trimmed = from.trimmingCharacters(in: .whitespaces)
    return trimmed.isEmpty ? "Unknown sender" : trimmed
}

/// What is wrong with a wake list, in the form's words; nil when it can be saved. The server checks
/// again; this only saves the round trip.
public func mailWakeListError(on: Bool, raw: String) -> String? {
    let entries = raw.split(whereSeparator: { "\n,;".contains($0) })
        .map { $0.trimmingCharacters(in: .whitespaces) }
        .filter { !$0.isEmpty }
    if on && entries.isEmpty { return "Name at least one sender to wake for." }
    for entry in entries {
        let domain = entry.contains("@") ? String(entry[entry.index(after: entry.lastIndex(of: "@")!)...]) : entry
        let ats = entry.filter { $0 == "@" }.count
        if entry.contains(where: \.isWhitespace) || ats > 1 || domain.range(of: #"^[^.\s]+(\.[^.\s]+)+$"#, options: .regularExpression) == nil {
            return "“\(entry)” is not an address or a domain. Write ada@example.com or @example.com."
        }
    }
    return nil
}

/// The Mailbox.state as words, or nil when mail works.
public func mailNotice(_ state: String) -> String? {
    switch state {
    case "off": return "Mail is turned off by the operator (mail.enabled)."
    case "no_domain": return "Mail needs a domain: the operator sets mail.domain, the part of every Bot's address after the @."
    default: return nil
    }
}

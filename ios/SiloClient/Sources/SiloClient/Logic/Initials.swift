import Foundation

/// A person's letters on their circle: the first two letters of the name part of their email
/// ("alice.smith@…" is AS, "bob@…" is BO). Port of `initials` in `web/src/space/people.ts`.
public func personInitials(_ email: String) -> String {
    let name = email.split(separator: "@", omittingEmptySubsequences: false).first.map(String.init) ?? ""
    let parts = name.split(whereSeparator: { "._-+".contains($0) }).map(String.init)
    let pick = parts.count >= 2 ? String(parts[0].prefix(1) + parts[1].prefix(1)) : String(name.prefix(2))
    return pick.isEmpty ? "?" : pick.uppercased()
}

import Foundation

/// Splices dictated text onto the end of a draft, adding a space when it would glue onto a word.
/// (Web splices at the caret; SwiftUI's `TextField` exposes none.)
public func appendDictation(_ draft: String, _ spoken: String) -> String {
    let said = spoken.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !said.isEmpty else { return draft }
    guard let last = draft.last else { return said }
    return last.isWhitespace ? draft + said : draft + " " + said
}

/// "0:07", "12:03".
public func formatClock(_ seconds: Double) -> String {
    let total = max(0, Int(seconds))
    return "\(total / 60):" + String(format: "%02d", total % 60)
}

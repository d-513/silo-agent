import Foundation

// Pure helpers for the Changes page. Port of `web/src/changeset.ts`; keep them in step.

public struct DiffLine: Equatable, Sendable {
    public enum Kind: Sendable { case hunk, add, del, ctx, note }
    public var kind: Kind
    public var text: String
    /// The line's number in the file: the new one, or the old one for a deletion.
    public var number: Int?
}

/// The hunks of one file's unified diff. The worker has already dropped the file header, so the
/// first line is a hunk's "@@ -a,b +c,d @@".
public func parsePatch(_ patch: String) -> [DiffLine] {
    var out: [DiffLine] = []
    var oldNo = 0
    var newNo = 0
    var lines = patch.components(separatedBy: "\n")
    if lines.last == "" { lines.removeLast() }
    let hunk = try! NSRegularExpression(pattern: #"^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$"#)
    for raw in lines {
        if let m = hunk.firstMatch(in: raw, range: NSRange(raw.startIndex..., in: raw)),
           let a = Range(m.range(at: 1), in: raw), let b = Range(m.range(at: 2), in: raw), let c = Range(m.range(at: 3), in: raw) {
            oldNo = Int(raw[a]) ?? 0
            newNo = Int(raw[b]) ?? 0
            out.append(DiffLine(kind: .hunk, text: String(raw[c])))
        } else if raw.hasPrefix("+") {
            out.append(DiffLine(kind: .add, text: String(raw.dropFirst()), number: newNo))
            newNo += 1
        } else if raw.hasPrefix("-") {
            out.append(DiffLine(kind: .del, text: String(raw.dropFirst()), number: oldNo))
            oldNo += 1
        } else if raw.hasPrefix("\\") {
            out.append(DiffLine(kind: .note, text: String(raw.dropFirst(2))))
        } else {
            out.append(DiffLine(kind: .ctx, text: String(raw.dropFirst()), number: newNo))
            newNo += 1
            oldNo += 1
        }
    }
    return out
}

private func sourceNames(_ sources: [Silo_V1_ChangeSource]) -> String {
    sources.map { s in
        let kind = ["chat": "Chat", "automation": "Automation", "channel": "Channel", "subagent": "Subagent"][s.kind] ?? "Run"
        return "\(kind) “\(s.name)”"
    }
    .joined(separator: ", ")
}

/// Who made a change: the runs at work, or nobody's run. A restore is the owner's.
public func changeSourceTitle(_ sources: [Silo_V1_ChangeSource], restore: Bool = false) -> String {
    if restore { return "Restored by you" }
    if sources.isEmpty { return "Outside a run" }
    return sourceNames(sources)
}

/// Why a change has more than one name on it, or none; empty when it needs no word.
public func changeSourceHint(_ sources: [Silo_V1_ChangeSource], restore: Bool = false) -> String {
    if restore {
        let undo = "Files put back from this page. Restore again to bring back what it replaced."
        return sources.isEmpty ? undo : "\(undo) \(sourceNames(sources)) was at work at the time, so its changes may be in here too."
    }
    if sources.isEmpty { return "Changed by you in Files or the Console, or by a process left running on the machine." }
    if sources.count > 1 { return "These runs were at work on the same machine at once, so the change is theirs together." }
    return ""
}

/// A file the history can put back: no content was kept for a file over the size limit or a
/// repository of its own.
public func changeCanRestore(status: String, large: Bool) -> Bool { !large && status != "repo" }

/// What putting a file back does to it, as a button label.
public func changeRestoreLabel(_ status: String) -> String {
    switch status {
    case "added": return "Remove This File"
    case "deleted": return "Bring This File Back"
    case "renamed": return "Move This File Back"
    default: return "Restore This File"
    }
}

/// What a restore did, as a sentence. `path` is the one file asked for, empty for a whole change.
public func changeRestoreNotice(_ res: Silo_V1_RestoreChangeResponse, path: String) -> String {
    let name = path.split(separator: "/").last.map(String.init) ?? ""
    var parts: [String] = []
    if res.restored > 0 {
        parts.append(name.isEmpty
            ? "The files are back as they were before that change. The restore is listed as a change, so it can be undone."
            : "\(name) is back as it was before that change. The restore is listed as a change, so it can be undone.")
    } else if res.skippedTotal == 0 {
        parts.append(name.isEmpty
            ? "Nothing to put back: the files are already as they were before that change."
            : "Nothing to put back: \(name) is already as it was before that change.")
    }
    if res.skippedTotal > 0 {
        let words = ["large": "over the size limit, so what was in it was never kept", "repo": "a git repository of its own", "blocked": "something else is in its place"]
        let named = res.skipped.prefix(3).map { "\($0.path) (\(words[$0.reason] ?? "it could not be replaced"))" }
        let more = Int(res.skippedTotal) - named.count
        let list = more > 0 ? "\(named.joined(separator: ", ")) and \(more) more" : named.joined(separator: ", ")
        parts.append("Left as they are: \(list).")
    }
    return parts.joined(separator: " ")
}

/// What a file row says in place of a diff, when it has none to show; empty otherwise.
public func changeFileNote(_ f: Silo_V1_ChangeFile, maxFileBytes: Int64) -> String {
    if f.status == "repo" { return "A git repository of its own. Only the commit it is on is recorded here, not its files." }
    if f.large {
        return "Over \(ByteCountFormatter.string(fromByteCount: maxFileBytes, countStyle: .file)), so only its size is recorded, not what is in it."
    }
    if f.binary { return "Not text, so there are no lines to compare." }
    return ""
}

/// A file's sizes on the two sides; -1 is the side where it does not exist.
public func changeSizes(old: Int64, new: Int64) -> String {
    let fmt = { ByteCountFormatter.string(fromByteCount: $0, countStyle: .file) }
    if old < 0 && new < 0 { return "" }
    if old < 0 { return fmt(new) }
    if new < 0 { return "was \(fmt(old))" }
    return old == new ? fmt(new) : "\(fmt(old)) → \(fmt(new))"
}

/// The ListChanges state as words, or nil when changes can be shown. `reset` offers a container reset.
public func changesNotice(_ state: String) -> (text: String, reset: Bool)? {
    switch state {
    case "off": return ("Change tracking is turned off by the operator (changes.enabled).", false)
    case "outdated": return ("This Bot's machine was made before change tracking. Reset its container to start recording what changes.", true)
    default: return nil
    }
}

/// The line under a drive journal row: what happened, how big, and whose run it was.
public func driveChangeLine(_ e: Silo_V1_DriveChangeEntry) -> String {
    let op = ["added": "Added", "modified": "Changed", "deleted": "Deleted", "renamed": "Renamed"][e.op] ?? "Changed"
    var parts = [e.op == "renamed" && !e.oldPath.isEmpty ? "Renamed from \(e.oldPath)" : op]
    if e.size > 0 { parts.append(ByteCountFormatter.string(fromByteCount: e.size, countStyle: .file)) }
    parts.append(changeSourceTitle(e.sources))
    return parts.joined(separator: " · ")
}

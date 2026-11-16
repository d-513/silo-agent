import Foundation

// Pure helpers for Workspaces: which one the app is in, what a role may see, the overview's order,
// and the words of the People, Activity and Move pages. Ports of `web/src/space/pick.ts`,
// `people.ts`, `spaces.ts`, `Overview.tsx`, `Activity.tsx` and `NewBotPage.tsx`; keep them in step.

// MARK: - Which Workspace

/// The Workspace to show: the one the open Bot is in, else the one last opened if it is still
/// listed, else the first. Empty while the list has not come.
public func pickSpace(ids: [String], named: String?, last: String?) -> String {
    if let named, !named.isEmpty { return named }
    if let last, ids.contains(last) { return last }
    return ids.first ?? ""
}

/// The letters a tile shows for a name, as the server makes them (space.Mark): the given letters,
/// else the initials of the first two words, else the first two letters.
public func spaceMark(name: String, given: String = "") -> String {
    func letters(_ s: String, _ n: Int) -> String {
        String(s.filter { $0.isLetter || $0.isNumber }.prefix(n)).uppercased()
    }
    let own = letters(given, 2)
    if !own.isEmpty { return own }
    let words = name.split(whereSeparator: \.isWhitespace).map(String.init)
    if words.count >= 2 {
        let two = letters(words[0], 1) + letters(words[1], 1)
        if !two.isEmpty { return two }
    }
    let first = letters(name, 2)
    return first.isEmpty ? "W" : first
}

// MARK: - Roles

/// The role may rename the Workspace, see its Rules and Activity, and manage its people.
public func canAdminSpace(_ role: String) -> Bool { role == "owner" || role == "admin" }

/// The role may make Bots and add connectors, skills and drives (everyone but a guest).
public func canBuildSpace(_ role: String) -> Bool { !role.isEmpty && role != "guest" }

/// A Workspace role as a reader says it; the owner's goes unsaid.
public func spaceRoleWord(_ role: String) -> String {
    switch role {
    case "admin": return "Admin"
    case "member": return "Member"
    case "guest": return "Guest"
    default: return ""
    }
}

/// Whether a caller with a Workspace role may change or remove a person with another: the owner
/// anyone but themselves, an admin members and guests. The server holds the same rule.
public func mayChangePerson(caller: String, theirs: String) -> Bool {
    if theirs == "owner" { return false }
    if caller == "owner" { return true }
    return caller == "admin" && (theirs == "member" || theirs == "guest")
}

/// A person's letters on their circle: the first two letters of the name part of their email
/// ("alice.smith@…" is AS, "bob@…" is BO).
public func personInitials(_ email: String) -> String {
    let name = email.split(separator: "@", omittingEmptySubsequences: false).first.map(String.init) ?? ""
    let parts = name.split(whereSeparator: { "._-+".contains($0) }).map(String.init)
    let pick = parts.count >= 2 ? String(parts[0].prefix(1)) + String(parts[1].prefix(1)) : String(name.prefix(2))
    let upper = pick.uppercased()
    return upper.isEmpty ? "?" : upper
}

// MARK: - The Workspace page's tabs

/// The Workspace page's tabs, in the web's order. A guest sees only People and Settings.
public enum SpaceTab: String, CaseIterable, Sendable, Hashable, Identifiable {
    case connectors, skills, drives, people, rules, activity, settings

    public var id: String { rawValue }

    /// Who the tab is for: everyone, builders (not guests), or admins.
    public enum Need: Sendable { case view, build, admin }

    public var need: Need {
        switch self {
        case .connectors, .skills, .drives: return .build
        case .rules, .activity: return .admin
        case .people, .settings: return .view
        }
    }

    public var title: String {
        switch self {
        case .connectors: return "Connectors"
        case .skills: return "Skills"
        case .drives: return "Drives"
        case .people: return "People"
        case .rules: return "Rules"
        case .activity: return "Activity"
        case .settings: return "Settings"
        }
    }

    /// The tabs a role sees.
    public static func visible(role: String) -> [SpaceTab] {
        allCases.filter {
            switch $0.need {
            case .view: return true
            case .build: return canBuildSpace(role)
            case .admin: return canAdminSpace(role)
            }
        }
    }
}

// MARK: - The Bot list

/// Bots by their newest message, newest first. A Bot nobody has written to keeps the order it came
/// in (newest made first), after the ones with a conversation. Web: `nav/botOrder.ts`.
public func byLastMessage<T>(_ items: [T], at: (T) -> String) -> [T] {
    func time(_ item: T) -> Double { isoDate(at(item))?.timeIntervalSince1970 ?? 0 }
    return items.enumerated()
        .sorted { a, b in
            let ta = time(a.element), tb = time(b.element)
            return ta == tb ? a.offset < b.offset : ta > tb
        }
        .map(\.element)
}

/// A time as a list row has room for it: 14:05 today, Mon this week, 3 Nov, or 3 Nov 2024.
public func shortWhen(_ iso: String, now: Date = Date(), calendar: Calendar = .current, locale: Locale = .current) -> String {
    guard let date = isoDate(iso) else { return "" }
    let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: date), to: calendar.startOfDay(for: now)).day ?? 0
    let format = DateFormatter()
    format.calendar = calendar
    format.timeZone = calendar.timeZone
    format.locale = locale
    if days <= 0 {
        format.setLocalizedDateFormatFromTemplate("jjmm")
    } else if days < 7 {
        format.setLocalizedDateFormatFromTemplate("EEE")
    } else if calendar.component(.year, from: date) == calendar.component(.year, from: now) {
        format.setLocalizedDateFormatFromTemplate("dMMM")
    } else {
        format.setLocalizedDateFormatFromTemplate("dMMMyyyy")
    }
    return format.string(from: date)
}

/// Where a live run is, as the strip and the Bot list say it.
public func activitySource(kind: String, title: String) -> String {
    switch kind {
    case "main": return "Main chat"
    case "automation": return "Automation “\(title)”"
    case "channel": return "Channel “\(title)”"
    case "subagent": return title.isEmpty ? "Subagent" : title
    case "hidden": return "Someone else's chat"
    default: return title.isEmpty ? "New chat" : title
    }
}

/// The tool a live run is in, as one word (the web's `toolMeta(name).app`).
public func activityStepApp(_ step: String) -> String {
    switch step {
    case "exec_python": return "Python"
    case "terminal": return "Terminal"
    case "read", "write", "patch", "grep", "delete", "present": return "Files"
    case "look", "click", "type", "key", "scroll": return "Desktop"
    case "soul": return "SOUL"
    case "skill": return "Skills"
    case "artifact": return "Artifact"
    case "web_search": return "Web search"
    case "web_read": return "Web page"
    case "channel": return "Channels"
    case "chats": return "Chats"
    case "feed": return "Feed"
    case "list_models", "switch_model": return "Model"
    case "decide": return "Decision"
    case "spawn_agent", "agent_status", "message_agent", "stop_agent": return "Subagents"
    case "sleep": return "Sleep"
    case "task_add", "task_list", "task_done", "task_reset": return "Taskboard"
    default:
        // A connector's completion tool is `<slug>__<action>`.
        if let range = step.range(of: "__") { return String(step[..<range.lowerBound]) }
        return step
    }
}

// MARK: - Activity

/// What a Workspace activity line says it was.
public func spaceActivityLine(kind: String, subject: String, detail: String, botName: String) -> String {
    switch kind {
    case "joined": return "\(subject) joined"
    case "left": return "\(subject) left"
    case "removed": return "\(subject) was removed"
    case "role_changed": return "\(subject) is now \(detail)"
    case "bot_created": return "Bot \(subject) made"
    case "bot_deleted": return "Bot \(subject) deleted"
    case "granted": return "\(subject) given to \(botName)"
    case "revoked": return "\(subject) taken from \(botName)"
    case "lent": return detail == "shared" ? "\(subject) shared with the workspace" : "\(subject) made personal"
    case "signed_in": return "Signed in to \(subject)"
    default: return kind
    }
}

// MARK: - Removing a person

/// What happens to one Bot whose last manager is going: a new manager, the remover takes it
/// over, or it is deleted.
public struct Handoff: Equatable, Sendable {
    public var action: String
    public var userID: String

    public init(action: String, userID: String = "") {
        self.action = action
        self.userID = userID
    }
}

/// The choice a removal starts with: the first other person who may manage it, else (leaving)
/// delete it, else (removing someone) take it over.
public func defaultHandoff(heirs: [String], leaving: Bool) -> Handoff {
    if let heir = heirs.first { return Handoff(action: "manager", userID: heir) }
    return Handoff(action: leaving ? "delete" : "take")
}

/// The choices a Bot whose last manager is going offers.
public func handoffActions(hasHeirs: Bool, leaving: Bool) -> [String] {
    (hasHeirs ? ["manager"] : []) + (leaving ? [] : ["take"]) + ["delete"]
}

// MARK: - New Bot and moving one

/// Why a personal sign-in cannot go to a new Bot, or "" when it can: someone else's never, mine
/// only while nobody else is picked to use the Bot.
public func lendBlocked(lentBy: String, shared: Bool, me: String, othersPicked: Bool) -> String {
    if lentBy.isEmpty || shared { return "" }
    if lentBy != me { return "Personal to \(lentBy)" }
    return othersPicked ? "Personal: others will use this Bot" : ""
}

/// What a Bot's move to another Workspace costs it, in one or two sentences.
public func moveSummary(connectors: [String], drives: [String], skills: [String], people: [String]) -> String {
    let losses = connectors + drives + skills
    var parts = [losses.isEmpty
        ? "It takes nothing of its workspace's along."
        : "It loses what was its workspace's: \(losses.joined(separator: ", "))."]
    if !people.isEmpty {
        parts.append("These people are not in the new one and lose it: \(people.joined(separator: ", ")).")
    }
    return parts.joined(separator: " ")
}

/// A name for a new copy of a library connector that no connection in the Workspace has yet:
/// "GitHub", then "GitHub 2", "GitHub 3", … (case-insensitive).
public func nextCopyName(_ base: String, taken: [String]) -> String {
    let used = Set(taken.map { $0.trimmingCharacters(in: .whitespaces).lowercased() })
    let stem = base.trimmingCharacters(in: .whitespaces)
    if !used.contains(stem.lowercased()) { return stem }
    for n in 2..<10000 {
        let candidate = "\(stem) \(n)"
        if !used.contains(candidate.lowercased()) { return candidate }
    }
    return "\(stem) \(taken.count + 1)"
}

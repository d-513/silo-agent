import Foundation

// How each kind of thing shows its state: the shared lamp, the word beside it (a lamp is never
// alone), and that word's color. Port of `web/src/statusLook.ts`; keep the two in step. Every view
// reads state through here, so a word or a color changes in one place, and none compares
// `bot.status` itself.

/// Green is for what is happening now: working breathes, online is a live connection. `none` is
/// no lamp: a Bot at rest shows nothing lit.
public enum LampStatus: String, Sendable, Codable {
    case online, working, needsYou = "needs_you", starting, stopped, none
}

public enum WordTone: String, Sendable, Codable {
    case emerald, vermilion, ink2, ink3
}

public struct StatusLook: Hashable, Sendable, Codable {
    public var lamp: LampStatus
    public var word: String
    public var tone: WordTone

    public init(lamp: LampStatus, word: String, tone: WordTone) {
        self.lamp = lamp
        self.word = word
        self.tone = tone
    }
}

private func needsYou() -> StatusLook { StatusLook(lamp: .needsYou, word: "Needs you", tone: .vermilion) }
private func onCall() -> StatusLook { StatusLook(lamp: .online, word: "On a call", tone: .emerald) }

/// What a run is about, for the status lines.
public struct Doing: Equatable, Sendable {
    public var state: String
    public var waitingOn: String
    public var task: String
    public var step: String

    public init(state: String = "working", waitingOn: String = "", task: String = "", step: String = "") {
        self.state = state
        self.waitingOn = waitingOn
        self.task = task
        self.step = step
    }

    public init(_ row: Silo_V1_BotActivity) {
        self.init(state: row.state, waitingOn: row.waitingOn, task: row.task, step: row.step)
    }

    /// A run with nothing known about it yet: a reply just sent.
    public static let started = Doing()
}

/// What one live run or call is doing, first match wins: waiting for you, for someone else, the
/// task in hand, the tool, else Thinking. The words are the thread's: a tool row says "Used Python".
public func runLook(_ a: Doing) -> StatusLook {
    switch a.state {
    case "needs_you":
        return needsYou()
    case "waiting":
        let who = a.waitingOn.split(separator: "@").first.map(String.init) ?? ""
        return StatusLook(lamp: .stopped, word: who.isEmpty ? "Waiting for a manager" : "Waiting on \(who)", tone: .ink3)
    case "on_call":
        return onCall()
    default:
        let word: String
        if !a.task.isEmpty {
            word = a.task
        } else if !a.step.isEmpty {
            word = "Using \(toolApp(a.step))"
        } else {
            word = "Thinking"
        }
        return StatusLook(lamp: .working, word: word, tone: .emerald)
    }
}

/// A run's place on its taskboard, "2/5"; empty with no current task.
public func progress(_ a: Silo_V1_BotActivity?) -> String {
    guard let a, !a.task.isEmpty, a.taskTotal > 0 else { return "" }
    return "\(a.taskDone)/\(a.taskTotal)"
}

// How pressing a row is for the chat it shows on: a chat's own run outranks the subagents it
// leads, and a call is only the backdrop to both.
private func rank(_ a: Silo_V1_BotActivity, _ chatID: String) -> Int {
    if a.state == "needs_you" { return 0 }
    if a.kind == "call" { return 3 }
    return a.chatID == chatID ? 1 : 2
}

/// The row that speaks for a chat: its own run, a subagent it leads, its call.
public func chatDoing(_ bot: Silo_V1_Bot, _ chatID: String?) -> Silo_V1_BotActivity? {
    guard let chatID, !chatID.isEmpty else { return nil }
    return bot.activity
        .filter { $0.chatID == chatID || $0.leadChatID == chatID }
        .enumerated()
        .min { (rank($0.element, chatID), $0.offset) < (rank($1.element, chatID), $1.offset) }?
        .element
}

/// The one line for a Bot: what needs you, what it is doing, else when it last worked. The box is
/// not part of it at rest: a message or an automation starts a stopped Bot.
public func botLook(_ bot: Silo_V1_Bot, now: Date = Date()) -> StatusLook {
    let rows = bot.activity.filter { $0.state == bot.status }
    switch bot.status {
    case "needs_you":
        return needsYou()
    case "working":
        if rows.count > 1 { return StatusLook(lamp: .working, word: "Working on \(rows.count) things", tone: .emerald) }
        return runLook(rows.first.map(Doing.init) ?? .started)
    case "waiting":
        return runLook(rows.first.map(Doing.init) ?? Doing(state: "waiting"))
    case "on_call":
        return onCall()
    case "starting":
        return StatusLook(lamp: .starting, word: "Starting", tone: .ink2)
    default:
        return StatusLook(lamp: .none, word: lastActive(bot.lastActiveAt, now: now), tone: .ink3)
    }
}

/// "Last active 2 h ago", or that it never has.
public func lastActive(_ iso: String, now: Date = Date()) -> String {
    guard let date = isoDate(iso) else { return "Has not worked yet" }
    return "Last active \(agoWords(date, now: now))"
}

/// "just now", "12 min ago", "3 h ago", "2 d ago": the web's `ago`.
public func agoWords(_ date: Date, now: Date = Date()) -> String {
    let seconds = Int(now.timeIntervalSince(date))
    if seconds < 60 { return "just now" }
    if seconds < 3600 { return "\(seconds / 60) min ago" }
    if seconds < 86_400 { return "\(seconds / 3600) h ago" }
    return "\(seconds / 86_400) d ago"
}

/// An RFC 3339 time, with or without fractions.
public func isoDate(_ iso: String) -> Date? {
    guard !iso.isEmpty else { return nil }
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return fractional.date(from: iso) ?? ISO8601DateFormatter().date(from: iso)
}

/// The word on Start/Stop, wherever the machine has one.
public func powerWord(machine: String) -> String {
    switch machine {
    case "online": return "Stop Bot"
    case "starting": return "Starting…"
    default: return "Start Bot"
    }
}

/// A Bot's box, for the places that are about the machine.
public func machineLook(_ machine: String) -> StatusLook {
    switch machine {
    case "online": return containerLook("running")
    case "starting": return StatusLook(lamp: .starting, word: "Starting", tone: .ink2)
    default: return containerLook("stopped")
    }
}

/// One container.
public func containerLook(_ state: String) -> StatusLook {
    switch state {
    case "running": return StatusLook(lamp: .online, word: "Running", tone: .emerald)
    case "stopped": return StatusLook(lamp: .stopped, word: "Stopped", tone: .ink3)
    default: return StatusLook(lamp: .stopped, word: "Not created", tone: .ink3)
    }
}

/// A subagent that has finished.
public func finishedLook(_ status: String) -> StatusLook {
    switch status {
    case "done": return StatusLook(lamp: .none, word: "Done", tone: .emerald)
    case "error": return StatusLook(lamp: .none, word: "Failed", tone: .vermilion)
    default: return StatusLook(lamp: .stopped, word: "Stopped", tone: .ink3)
    }
}

/// A channel adapter's connection.
public func channelLook(_ status: String) -> StatusLook {
    switch status {
    case "connected": return StatusLook(lamp: .online, word: "Connected", tone: .ink3)
    case "starting": return StatusLook(lamp: .starting, word: "Starting…", tone: .ink3)
    case "pending_auth": return StatusLook(lamp: .needsYou, word: "Needs linking", tone: .vermilion)
    case "error": return StatusLook(lamp: .needsYou, word: "Error", tone: .vermilion)
    case "stopped": return StatusLook(lamp: .stopped, word: "Stopped", tone: .ink3)
    default: return StatusLook(lamp: .stopped, word: status.replacingOccurrences(of: "_", with: " "), tone: .ink3)
    }
}

/// A connector attached to a Bot; a built-in one signs in with its own settings.
public func connectorLook(_ status: String, builtin: Bool = false) -> StatusLook {
    switch status {
    case "authorized": return StatusLook(lamp: .online, word: builtin ? "Signed in" : "Authorized", tone: .ink3)
    case "needs_auth": return StatusLook(lamp: .needsYou, word: builtin ? "Needs setup" : "Needs authorization", tone: .vermilion)
    case "initializing": return StatusLook(lamp: .starting, word: "Initializing", tone: .ink3)
    case "error": return StatusLook(lamp: .needsYou, word: "Error", tone: .vermilion)
    default: return StatusLook(lamp: .stopped, word: "Ready", tone: .ink3)
    }
}

/// A mounted drive.
public func driveLook(_ state: String) -> StatusLook {
    switch state {
    case "mounted": return StatusLook(lamp: .online, word: "Mounted", tone: .emerald)
    case "mounting": return StatusLook(lamp: .starting, word: "Connecting…", tone: .ink2)
    case "needs_auth": return StatusLook(lamp: .needsYou, word: "Reconnect needed", tone: .vermilion)
    case "needs_setup": return StatusLook(lamp: .needsYou, word: "Waiting for an admin", tone: .vermilion)
    case "needs_input": return StatusLook(lamp: .needsYou, word: "Needs details", tone: .vermilion)
    case "error": return StatusLook(lamp: .needsYou, word: "Error", tone: .vermilion)
    default: return StatusLook(lamp: .stopped, word: "Stopped", tone: .ink3)
    }
}

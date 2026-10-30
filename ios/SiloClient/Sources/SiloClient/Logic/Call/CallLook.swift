import Foundation

// How a call shows what is going on, whichever way it is drawn: the bar's orb, the screen's rings,
// the Dynamic Island and the lock screen read one tone and say the same words. Port of
// `web/src/call/look.ts`.

public enum CallState: String, Sendable, Codable {
    case connecting, listening, hearing, thinking, working, needsYou = "needs_you", speaking, ended
}

public enum EndReason: String, Sendable, Codable {
    case hangup, idle, limit, access, replaced, lost, error
}

/// Whose moment it is: you, the Bot's voice, what waits for you, then what goes on behind the silence.
public enum CallTone: String, Sendable, Codable {
    case ended, you, bot, needsYou, muted, working, thinking, quiet
}

public struct CallSnapshot: Equatable, Sendable {
    public var state: CallState = .connecting
    /// The tool the Bot is in while it works.
    public var tool = ""
    /// Your last turn as it was heard, and the sentence the Bot is saying.
    public var heard = ""
    /// Your words as they are being heard, while a turn streams live.
    public var hearing = ""
    public var saying = ""
    public var muted = false
    /// Hold to talk: no voice detection, a turn is what is said while held.
    public var ptt = false
    public var holding = false
    /// 0...1: yours while you speak, the Bot's while it speaks.
    public var level: Float = 0
    public var chatID = ""
    public var callID = ""
    public var error = ""
    public var ended: EndReason?
    public var startedAt = Date()

    public init(chatID: String = "") { self.chatID = chatID }
}

public func callTone(_ s: CallSnapshot) -> CallTone {
    switch s.state {
    case .ended: return .ended
    case .hearing: return .you
    case .speaking: return .bot
    case .needsYou: return .needsYou
    default: break
    }
    if s.muted { return .muted }
    if s.state == .working { return .working }
    return s.state == .thinking ? .thinking : .quiet
}

/// What the Bot is on while a call waits, in the status lines' words: its task if it has one, else
/// the tool the call last heard of.
public func callWork(_ s: CallSnapshot, doing: Doing? = nil) -> String {
    runLook(Doing(task: doing?.task ?? "", step: s.tool)).word
}

public func stateWords(_ s: CallSnapshot, botName: String, doing: Doing? = nil) -> String {
    switch s.state {
    case .connecting: return "Connecting…"
    case .hearing: return s.holding ? "Talking…" : "Hearing you…"
    case .thinking: return "\(botName) is thinking…"
    case .working: return callWork(s, doing: doing)
    case .needsYou: return "Needs you on screen"
    case .speaking: return "\(botName) is speaking"
    case .ended: return s.error.isEmpty ? endWords(s.ended) : s.error
    case .listening:
        if s.muted { return "Muted" }
        return s.ptt ? "Hold to talk" : "Listening"
    }
}

public func endWords(_ reason: EndReason?) -> String {
    switch reason {
    case .idle: return "Call ended after a quiet while"
    case .limit: return "Call ended: it reached the longest call allowed"
    case .access: return "Call ended: you no longer have this Bot"
    case .replaced: return "The call moved to another device"
    case .lost: return "The call's connection dropped"
    case .error: return "The call could not go on"
    default: return "Call ended"
    }
}

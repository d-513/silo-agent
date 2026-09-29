import Foundation

/// Thinking levels mirror `internal/llm/thinking.go` and `web/src/thinking.ts`: the CP lists what
/// each model accepts (`ModelOption.thinkingLevels`) and fits a chat's level on every turn, so the
/// composer shows the level that will actually be sent.
public let thinkingOrder = ["off", "minimal", "low", "medium", "high", "xhigh", "max"]

public func thinkingLabel(_ level: String) -> String {
    switch level {
    case "": return "Default"
    case "off": return "Off"
    case "minimal": return "Minimal"
    case "low": return "Low"
    case "medium": return "Medium"
    case "high": return "High"
    case "xhigh": return "Extra high"
    case "max": return "Max"
    default: return level
    }
}

/// `llm.NearestThinking`: the level itself when the model takes it, else the closest one (ties
/// go to the cheaper), else "" (the model default).
public func fitThinking(_ level: String, _ levels: [String]) -> String {
    guard let want = thinkingOrder.firstIndex(of: level), !levels.isEmpty else { return "" }
    var best = ""
    var bestRank = Int.max
    var bestDist = Int.max
    for candidate in levels {
        guard let rank = thinkingOrder.firstIndex(of: candidate) else { continue }
        let dist = abs(rank - want)
        if dist < bestDist || (dist == bestDist && rank < bestRank) {
            best = candidate
            bestRank = rank
            bestDist = dist
        }
    }
    return best
}

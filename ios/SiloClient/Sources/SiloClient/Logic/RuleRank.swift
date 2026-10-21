import Foundation

// How much a mode holds a call back (allow, auto, ask, deny) and how much an output mode hides
// (pass, flag, check, withhold). Port of `strictness` in `web/src/rules/model.ts` and
// `internal/security/layers.go`. An unknown mode ranks as ask or check, so a bad value can never
// loosen a lock.
public enum RuleSide: String, Sendable { case decision, output }

public func ruleStrictness(_ mode: String, side: RuleSide = .decision) -> Int {
    let ranks = side == .output
        ? ["pass": 0, "flag": 1, "check": 2, "withhold": 3]
        : ["allow": 0, "auto": 1, "ask": 2, "deny": 3]
    return ranks[mode] ?? 2
}

/// Whether `mode` is offered for a rule the operator locked at `floor`: the owner may tighten a
/// locked rule, never loosen it.
public func ruleModeAllowed(_ mode: String, floor: String, side: RuleSide) -> Bool {
    floor.isEmpty || ruleStrictness(mode, side: side) >= ruleStrictness(floor, side: side)
}

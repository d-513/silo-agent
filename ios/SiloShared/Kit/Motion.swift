import SwiftUI

/// The two curves of `DESIGN.md` §5 and its durations. No bounce, no overshoot past the settle.
enum Motion {
    static func quiet(_ duration: Double = 0.24) -> Animation { .timingCurve(0.32, 0.72, 0, 1, duration: duration) }
    static func settle(_ duration: Double = 0.42) -> Animation { .timingCurve(0.22, 1, 0.36, 1, duration: duration) }
    static let press = Animation.easeOut(duration: 0.07)
    static let fold = quiet(0.32)
    static let state = quiet(0.24)
    static let arrival = settle(0.42)
    static let glide = settle(0.52)
}

/// Things arriving: rise 8pt and fade in. `when` is false for what was already there (a replay).
private struct RiseIn: ViewModifier {
    let when: Bool
    let from: CGFloat
    @State private var shown = false
    @Environment(\.accessibilityReduceMotion) private var still

    func body(content: Content) -> some View {
        content
            .opacity(when && !shown ? 0 : 1)
            .offset(y: when && !shown && !still ? from : 0)
            .onAppear {
                guard when, !shown else { return }
                withAnimation(still ? .linear(duration: 0.12) : Motion.arrival) { shown = true }
            }
    }
}

extension View {
    func rise(when: Bool = true, from: CGFloat = 8) -> some View { modifier(RiseIn(when: when, from: from)) }
}

extension AnyTransition {
    /// What arrives rises and fades; what leaves only fades.
    static var rise: AnyTransition {
        .asymmetric(insertion: .opacity.combined(with: .offset(y: 8)), removal: .opacity)
    }

    /// The slip: in from below, out with a fade.
    static var slip: AnyTransition {
        .asymmetric(insertion: .opacity.combined(with: .offset(y: 28)), removal: .opacity.combined(with: .offset(y: 20)))
    }
}

/// The only ambient loop in the product: opacity 1 → .4 → 1 over 2.4s.
struct Breathe: ViewModifier {
    var on = true
    @Environment(\.accessibilityReduceMotion) private var still

    func body(content: Content) -> some View {
        if on && !still {
            content.phaseAnimator([1.0, 0.4]) { view, phase in
                view.opacity(phase)
            } animation: { _ in .easeInOut(duration: 1.2) }
        } else {
            content
        }
    }
}

extension View {
    func breathing(_ on: Bool = true) -> some View { modifier(Breathe(on: on)) }
}

/// A word that is happening now: a highlight moves through it, 1.6s, like the web's Thinking.
struct Shimmer: ViewModifier {
    var on = true
    @Environment(\.accessibilityReduceMotion) private var still

    func body(content: Content) -> some View {
        if on && !still {
            TimelineView(.animation) { timeline in
                let t = timeline.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 1.6) / 1.6
                content.overlay {
                    LinearGradient(stops: [
                        .init(color: .clear, location: max(0, t * 1.6 - 0.45)),
                        .init(color: Theme.ink, location: max(0.001, t * 1.6 - 0.2)),
                        .init(color: .clear, location: min(1, t * 1.6 + 0.05)),
                    ], startPoint: .leading, endPoint: .trailing)
                    .mask(content)
                    .allowsHitTesting(false)
                }
            }
        } else {
            content
        }
    }
}

extension View {
    func shimmering(_ on: Bool = true) -> some View { modifier(Shimmer(on: on)) }
}

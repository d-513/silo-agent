import SwiftUI
import UIKit

private extension ControlSize {
    var small: Bool { self == .small || self == .mini }
}

/// Press down: scale .97 over 70ms, on every button.
struct PressStyle: ButtonStyle {
    var scale: CGFloat = 0.97
    @Environment(\.accessibilityReduceMotion) private var still

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed && !still ? scale : 1)
            .animation(Motion.press, value: configuration.isPressed)
    }
}

/// The primary action: an ink fill (never a blue one).
struct InkButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var enabled
    @Environment(\.controlSize) private var size

    func makeBody(configuration: Configuration) -> some View {
        let small = size.small
        configuration.label
            .font(.silo(small ? 13 : 15, .medium))
            .foregroundStyle(enabled ? Theme.onInk : Theme.ink3)
            .padding(.horizontal, small ? 12 : 16)
            .frame(minHeight: small ? 32 : 44)
            .background(enabled ? (configuration.isPressed ? Theme.inkDeep : Theme.ink) : Theme.pressed,
                        in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(Motion.press, value: configuration.isPressed)
    }
}

/// The secondary action: surface with an inset ring, or a tint such as vermilion.
struct QuietButtonStyle: ButtonStyle {
    var tint: Color = Theme.ink
    @Environment(\.isEnabled) private var enabled
    @Environment(\.controlSize) private var size

    func makeBody(configuration: Configuration) -> some View {
        let small = size.small
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        configuration.label
            .font(.silo(small ? 13 : 15, .medium))
            .foregroundStyle(enabled ? tint : Theme.ink3)
            .padding(.horizontal, small ? 12 : 16)
            .frame(minHeight: small ? 32 : 44)
            .background(configuration.isPressed ? Theme.well : Theme.surface, in: shape)
            .overlay(shape.strokeBorder(Theme.lineStrong, lineWidth: 1))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(Motion.press, value: configuration.isPressed)
    }
}

/// A ghost: no ring, a `well` under a press. Deny is one in vermilion.
struct GhostButtonStyle: ButtonStyle {
    var tint: Color = Theme.ink2
    @Environment(\.isEnabled) private var enabled
    @Environment(\.controlSize) private var size

    func makeBody(configuration: Configuration) -> some View {
        let small = size.small
        configuration.label
            .font(.silo(small ? 13 : 15, .medium))
            .foregroundStyle(enabled ? tint : Theme.ink3)
            .padding(.horizontal, small ? 10 : 14)
            .frame(minHeight: small ? 32 : 44)
            .background(configuration.isPressed ? Theme.well : .clear,
                        in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(Motion.press, value: configuration.isPressed)
    }
}

/// A glyph with a 40pt hit area, `ink2` at rest.
struct GlyphButtonStyle: ButtonStyle {
    var tint: Color = Theme.ink2
    var side: CGFloat = 40
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 16, weight: .regular))
            .foregroundStyle(enabled ? tint : Theme.ink3)
            .frame(width: side, height: side)
            .background(configuration.isPressed ? Theme.well : .clear,
                        in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .contentShape(Rectangle())
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .animation(Motion.press, value: configuration.isPressed)
    }
}

extension ButtonStyle where Self == InkButtonStyle {
    static var ink: InkButtonStyle { InkButtonStyle() }
}

extension ButtonStyle where Self == QuietButtonStyle {
    static var quiet: QuietButtonStyle { QuietButtonStyle() }
    static func quiet(tint: Color) -> QuietButtonStyle { QuietButtonStyle(tint: tint) }
}

extension ButtonStyle where Self == GhostButtonStyle {
    static var ghost: GhostButtonStyle { GhostButtonStyle() }
    static func ghost(tint: Color) -> GhostButtonStyle { GhostButtonStyle(tint: tint) }
}

extension ButtonStyle where Self == GlyphButtonStyle {
    static var glyph: GlyphButtonStyle { GlyphButtonStyle() }
    static func glyph(tint: Color, side: CGFloat = 40) -> GlyphButtonStyle { GlyphButtonStyle(tint: tint, side: side) }
}

extension ButtonStyle where Self == PressStyle {
    /// A row or a card that presses and otherwise draws itself.
    static var press: PressStyle { PressStyle() }
}

/// A destructive action, with no dialog: the first press arms it (a vermilion fill and a bar that
/// drains over 3s), the second does it, and if the bar runs out it disarms.
struct ArmedButton: View {
    let title: String
    var armedTitle: String?
    var symbol: String?
    var small = false
    /// Fills the width, for the Dangerous panel and a slip.
    var wide = false
    let action: () -> Void
    @State private var armed = false
    @State private var drained = false
    @State private var timer: Task<Void, Never>?

    var body: some View {
        Button {
            guard armed else { return arm() }
            timer?.cancel()
            armed = false
            action()
        } label: {
            HStack(spacing: 6) {
                if let symbol { Image(systemName: symbol).font(.system(size: small ? 12 : 13, weight: .medium)) }
                Text(armed ? (armedTitle ?? "Tap again to \(title.lowercased())") : title).lineLimit(1)
            }
            .font(.silo(small ? 13 : 15, .medium))
            .foregroundStyle(armed ? Theme.onAccent : Theme.vermilion)
            .padding(.horizontal, small ? 10 : 14)
            .frame(minHeight: small ? 32 : 44)
            .frame(maxWidth: wide ? .infinity : nil)
            .background(armed ? Theme.vermilion : .clear)
            .overlay(alignment: .bottomLeading) {
                if armed {
                    GeometryReader { box in
                        Theme.onAccent.frame(width: drained ? 0 : box.size.width, height: 3)
                            .frame(maxHeight: .infinity, alignment: .bottom)
                    }
                }
            }
            .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
        .sensoryFeedback(.warning, trigger: armed) { _, on in on }
        .accessibilityHint(armed ? "Press again to confirm" : "")
    }

    private func arm() {
        drained = false
        withAnimation(Motion.state) { armed = true }
        withAnimation(.linear(duration: 3)) { drained = true }
        timer?.cancel()
        timer = Task {
            try? await Task.sleep(for: .seconds(3))
            guard !Task.isCancelled else { return }
            withAnimation(Motion.state) { armed = false }
        }
    }
}

/// The button is the feedback: Saving… with a spinner, Saved with a check, then Save again.
struct SaveButton: View {
    var title = "Save"
    var enabled = true
    let action: () async -> Bool
    @State private var phase = 0

    var body: some View {
        Button {
            guard phase == 0 else { return }
            phase = 1
            Task {
                let ok = await action()
                withAnimation(Motion.settle(0.28)) { phase = ok ? 2 : 0 }
                guard ok else { return }
                try? await Task.sleep(for: .seconds(1.7))
                withAnimation(Motion.state) { phase = 0 }
            }
        } label: {
            HStack(spacing: 6) {
                switch phase {
                case 1:
                    Spinner(size: 13, color: Theme.onInk)
                    Text("Saving…")
                case 2:
                    Image(systemName: "checkmark").font(.system(size: 12, weight: .semibold)).transition(.scale(scale: 0.6).combined(with: .opacity))
                    Text("Saved")
                default:
                    Text(title)
                }
            }
        }
        .buttonStyle(.ink)
        .disabled(!enabled)
    }
}

/// The copy glyph morphs into an emerald check for 1.5s, then back.
struct CopyButton: View {
    let text: String
    var size: CGFloat = 14
    @State private var copied = false

    var body: some View {
        Button {
            UIPasteboard.general.string = text
            withAnimation(Motion.settle(0.26)) { copied = true }
            Task {
                try? await Task.sleep(for: .seconds(1.5))
                withAnimation(Motion.state) { copied = false }
            }
        } label: {
            ZStack {
                Image(systemName: "doc.on.doc").opacity(copied ? 0 : 1).rotationEffect(.degrees(copied ? 30 : 0)).scaleEffect(copied ? 0.5 : 1)
                Image(systemName: "checkmark").foregroundStyle(Theme.emerald).opacity(copied ? 1 : 0).rotationEffect(.degrees(copied ? 0 : -30)).scaleEffect(copied ? 1 : 0.5)
            }
            .font(.system(size: size, weight: .regular))
        }
        .buttonStyle(.glyph)
        .sensoryFeedback(.success, trigger: copied) { _, on in on }
        .accessibilityLabel("Copy")
    }
}

/// The switch: 44×26, ink when on; while pressed the knob stretches toward where it is going.
struct SiloToggleStyle: ToggleStyle {
    func makeBody(configuration: Configuration) -> some View {
        Track(configuration: configuration)
    }

    private struct Track: View {
        let configuration: ToggleStyleConfiguration
        @State private var pressing = false
        @Environment(\.isEnabled) private var enabled
        @Environment(\.labelsVisibility) private var labels

        var body: some View {
            HStack(spacing: 12) {
                // A hidden label takes no room: the switch sits at the end of someone else's row.
                if labels != .hidden {
                    configuration.label
                        .font(.silo(15))
                        .foregroundStyle(Theme.ink)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                knob
            }
            .contentShape(Rectangle())
            .onTapGesture { flip() }
        }

        private var knob: some View {
            let on = configuration.isOn
            return ZStack(alignment: on ? .trailing : .leading) {
                Capsule().fill(on ? Theme.ink : Theme.pressed)
                    .overlay(Capsule().strokeBorder(Theme.line, lineWidth: on ? 0 : 1))
                Capsule()
                    .fill(Color.white)
                    .overlay(Capsule().strokeBorder(Color.black.opacity(0.06), lineWidth: 0.5))
                    .shadow(color: .black.opacity(0.15), radius: 1.5, y: 1)
                    .frame(width: pressing ? 26 : 20, height: 20)
                    .padding(3)
            }
            .frame(width: 44, height: 26)
            .opacity(enabled ? 1 : 0.5)
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { _ in if !pressing { withAnimation(Motion.quiet(0.16)) { pressing = true } } }
                    .onEnded { _ in
                        withAnimation(Motion.quiet(0.26)) { pressing = false }
                        flip()
                    }
            )
            .sensoryFeedback(.selection, trigger: on)
            .accessibilityElement()
            .accessibilityAddTraits(.isButton)
            .accessibilityValue(on ? "On" : "Off")
            .accessibilityAction { flip() }
        }

        private func flip() {
            guard enabled else { return }
            withAnimation(Motion.quiet(0.26)) { configuration.$isOn.wrappedValue.toggle() }
        }
    }
}

extension ToggleStyle where Self == SiloToggleStyle {
    static var silo: SiloToggleStyle { SiloToggleStyle() }
}

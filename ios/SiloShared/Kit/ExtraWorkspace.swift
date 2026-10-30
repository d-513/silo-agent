import SwiftUI

extension View {
    /// A flat `surface` folio with its padding, and the 2pt vermilion ribbon when it needs you.
    func folio(ribbon: Bool = false, padding: CGFloat = 16) -> some View {
        self.padding(padding).machineFolio(ribbon: ribbon)
    }
}

/// A folio that is one press as a whole, and still holds rows with a press of their own: what only
/// reads sits under `.allowsHitTesting(false)`, so a tap there falls through to the card.
struct PressCard<Content: View>: View {
    /// What VoiceOver calls the card.
    let label: String
    var ribbon = false
    var padding: CGFloat = 16
    let action: () -> Void
    @ViewBuilder let content: Content
    @State private var pressed = false
    @Environment(\.accessibilityReduceMotion) private var still

    var body: some View {
        content
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                Button(action: action) { Color.clear.contentShape(Rectangle()) }
                    .buttonStyle(Signal(pressed: $pressed))
                    .accessibilityLabel(label)
            }
            .folio(ribbon: ribbon, padding: 0)
            .scaleEffect(pressed && !still ? 0.985 : 1)
            .animation(Motion.press, value: pressed)
    }

    /// Tells the card it is held, so the whole card presses and not the empty layer behind it.
    private struct Signal: ButtonStyle {
        @Binding var pressed: Bool

        func makeBody(configuration: Configuration) -> some View {
            configuration.label.onChange(of: configuration.isPressed) { _, held in pressed = held }
        }
    }
}

/// One numbered step of a form: a later step waits at reduced opacity until the one before it is
/// done, and a done step's number becomes a check.
struct StepCard<Content: View>: View {
    let number: Int
    let title: String
    var note: String?
    var done = false
    var locked = false
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top, spacing: 12) {
                ZStack {
                    Circle().fill(done ? Theme.ink : Theme.well)
                    if done {
                        Image(systemName: "checkmark")
                            .font(.system(size: 11, weight: .bold))
                            .foregroundStyle(Theme.onInk)
                            .transition(.scale(scale: 0.6).combined(with: .opacity))
                    } else {
                        Text("\(number)").font(.silo(12, .semibold)).monospacedDigit().foregroundStyle(Theme.ink2)
                    }
                }
                .frame(width: 24, height: 24)
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).font(.silo(15, .semibold)).foregroundStyle(Theme.ink)
                    if let note, !note.isEmpty {
                        Text(note).font(.silo(12.5)).foregroundStyle(Theme.ink2).fixedSize(horizontal: false, vertical: true)
                    }
                }
                .padding(.top, 2)
            }
            content.disabled(locked)
        }
        .folio()
        .opacity(locked ? 0.55 : 1)
        .animation(Motion.quiet(0.2), value: locked)
        .animation(Motion.state, value: done)
    }
}

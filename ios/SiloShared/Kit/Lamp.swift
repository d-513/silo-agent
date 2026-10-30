import SiloClient
import SwiftUI

/// The status lamp: 7pt, or 9 on a crest's corner. Working breathes, a live connection is solid,
/// Needs you fires one ping and never loops, starting and stopped are hollow, and a Bot at rest has
/// no lamp at all. A lamp is never the only signal: it sits beside a word or a ribbon.
struct Lamp: View {
    let status: LampStatus
    var size: CGFloat = 7
    /// A 2pt ring in the ground's color, for a crest's corner.
    var ground: Color?
    @State private var ping = false
    @Environment(\.accessibilityReduceMotion) private var still

    var body: some View {
        Group {
            switch status {
            case .none:
                EmptyView()
            case .online:
                dot(Theme.lamp)
            case .working:
                dot(Theme.lamp).breathing()
            case .needsYou:
                dot(Theme.vermilion)
                    .background {
                        Circle()
                            .fill(Theme.vermilion)
                            .scaleEffect(ping ? 4.2 : 1)
                            .opacity(ping ? 0 : 0.55)
                    }
                    .onAppear {
                        guard !still else { return }
                        withAnimation(.easeOut(duration: 0.9)) { ping = true }
                    }
            case .starting:
                Circle().strokeBorder(Theme.cobalt, lineWidth: 1.5).frame(width: size, height: size)
            case .stopped:
                Circle().strokeBorder(Theme.ink3, lineWidth: 1.5).frame(width: size, height: size)
            }
        }
        .padding(ground == nil ? 0 : 2)
        .background { if let ground { Circle().fill(ground) } }
        .animation(Motion.quiet(0.32), value: status)
    }

    private func dot(_ color: Color) -> some View {
        Circle().fill(color).frame(width: size, height: size)
    }
}

/// A lamp and its word; the word crossfades, the new one rising 4pt.
struct StatusWord: View {
    let look: StatusLook
    var font: Font = .silo(12.5, .medium)

    var body: some View {
        HStack(spacing: 6) {
            Lamp(status: look.lamp)
            Text(look.word)
                .font(font)
                .foregroundStyle(Theme.color(look.tone))
                .lineLimit(1)
                .id(look.word)
                .transition(.asymmetric(insertion: .opacity.combined(with: .offset(y: 4)), removal: .opacity))
        }
        .animation(Motion.state, value: look.word)
        .accessibilityElement(children: .combine)
    }
}

/// What it is on: the status word, then its place on the taskboard in mono.
struct DoingLine: View {
    let look: StatusLook
    var step = ""
    var font: Font = .silo(12.5, .medium)

    var body: some View {
        HStack(spacing: 6) {
            StatusWord(look: look, font: font)
            if !step.isEmpty {
                Text(step).font(.siloMono(11.5)).foregroundStyle(Theme.ink3).monospacedDigit().layoutPriority(1)
            }
        }
    }
}

/// The spinner, for the few places a skeleton or a morph cannot go: a ring on a `pressed` track.
struct Spinner: View {
    var size: CGFloat = 16
    var color: Color = Theme.ink2
    @State private var turned = false

    var body: some View {
        ZStack {
            Circle().stroke(Theme.pressed, lineWidth: 2)
            Circle().trim(from: 0, to: 0.28)
                .stroke(color, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                .rotationEffect(.degrees(turned ? 360 : 0))
        }
        .frame(width: size, height: size)
        .onAppear {
            withAnimation(.linear(duration: 0.8).repeatForever(autoreverses: false)) { turned = true }
        }
        .accessibilityLabel("Loading")
    }
}

/// Loading rows: `well` bars with a slow shimmer. Reserve the room so nothing shifts.
struct SkeletonRows: View {
    var count = 4
    var height: CGFloat = 44

    var body: some View {
        VStack(spacing: 8) {
            ForEach(0..<count, id: \.self) { index in
                RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    .fill(Theme.well)
                    .frame(maxWidth: .infinity)
                    .frame(height: height)
                    .opacity(1 - Double(index) * 0.14)
                    .breathing()
            }
        }
        .accessibilityLabel("Loading")
    }
}

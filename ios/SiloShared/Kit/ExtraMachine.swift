import SwiftUI

/// The chevron of a row that opens in place: it turns 90° over 260ms.
struct MachineChevron: View {
    let open: Bool
    var size: CGFloat = 11

    var body: some View {
        Image(systemName: "chevron.right")
            .font(.system(size: size, weight: .semibold))
            .foregroundStyle(Theme.ink3)
            .rotationEffect(.degrees(open ? 90 : 0))
            .animation(Motion.quiet(0.26), value: open)
            .accessibilityHidden(true)
    }
}

/// One `well` sentence: how a page stands, what a restore did, why something is not shown.
struct MachineNote: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.silo(12.5))
            .lineSpacing(2)
            .foregroundStyle(Theme.ink2)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}

/// A long text field: a `TextEditor` in the input's frame, with the label above and a placeholder
/// in `ink3`, for SOUL, a policy, a prompt.
struct MachineEditor: View {
    var label: String?
    var prompt = ""
    @Binding var text: String
    var mono = false
    var minHeight: CGFloat = 120
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let label { Text(label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3) }
            ZStack(alignment: .topLeading) {
                if text.isEmpty {
                    Text(prompt)
                        .font(face)
                        .foregroundStyle(Theme.ink3)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 11)
                        .allowsHitTesting(false)
                }
                TextEditor(text: $text)
                    .font(face)
                    .foregroundStyle(Theme.ink)
                    .tint(Theme.cobalt)
                    .scrollContentBackground(.hidden)
                    .focused($focused)
                    .padding(.horizontal, 7)
                    .padding(.vertical, 3)
            }
            .frame(minHeight: minHeight)
            .siloInputFrame(focused: focused)
        }
    }

    private var face: Font { mono ? .siloMono(13.5) : .silo(15) }
}

/// A switch at the end of a row, with no label of its own. The row says what it does.
struct MachineSwitch: View {
    let label: String
    @Binding var isOn: Bool

    var body: some View {
        Toggle(isOn: $isOn) { EmptyView() }
            .toggleStyle(.silo)
            .fixedSize()
            .accessibilityLabel(label)
    }
}

/// A glyph on a `well` tile, the mark of a row with no logo of its own.
struct MachineTile: View {
    let symbol: String
    var side: CGFloat = 40

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: side * 0.42))
            .foregroundStyle(Theme.ink2)
            .frame(width: side, height: side)
            .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            .accessibilityHidden(true)
    }
}

extension View {
    /// A flat `surface` folio; `ribbon` is the 2pt vermilion edge of one that needs you.
    func machineFolio(ribbon: Bool = false) -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        return frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.surface)
            .overlay(alignment: .leading) {
                if ribbon { Theme.vermilion.frame(width: 2) }
            }
            .clipShape(shape)
            .overlay(shape.stroke(Theme.line, lineWidth: 1))
    }
}

/// What was on a page when it first loaded, so that only what arrives later rises (`.rise(when:)`
/// needs an answer that never changes for a row).
struct FirstSeen {
    private var first: Set<String>?

    mutating func note(_ ids: some Sequence<String>) {
        if first == nil { first = Set(ids) }
    }

    func isNew(_ id: String) -> Bool { first?.contains(id) == false }
}

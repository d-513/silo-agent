import SwiftUI

extension Shape {
    /// A surface card's fill and ring as one background.
    func card(_ fill: Color = Theme.surface) -> some View {
        self.fill(fill).overlay(self.stroke(Theme.line, lineWidth: 1))
    }
}

extension View {
    /// `shadow-float`: the composer, a lifted card.
    func floatShadow() -> some View {
        shadow(color: Theme.shade.opacity(0.04), radius: 1, y: 1).shadow(color: Theme.shade.opacity(0.08), radius: 10, y: 6)
    }

    /// `shadow-slip`: the slip and menus.
    func slipShadow() -> some View {
        shadow(color: Theme.shade.opacity(0.05), radius: 4, y: 2).shadow(color: Theme.shade.opacity(0.2), radius: 28, y: 18)
    }
}

/// A `label-caps` section heading in `ink3`.
struct SectionLabel: View {
    let text: String
    var body: some View {
        Text(text.uppercased())
            .font(.silo(11, .medium))
            .tracking(0.88)
            .foregroundStyle(Theme.ink3)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// A flat `surface` card of rows with hairlines between them: the phone's answer to a grouped list.
struct Panel<Content: View>: View {
    var padded = false
    @ViewBuilder let content: Content

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        VStack(alignment: .leading, spacing: 0) {
            Group(subviews: content) { rows in
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                    if index > 0 { Theme.line.frame(height: 1).padding(.leading, padded ? 14 : 0) }
                    row
                }
            }
        }
        .padding(padded ? 14 : 0)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: shape)
        .clipShape(shape)
        .overlay(shape.stroke(Theme.line, lineWidth: 1))
    }
}

/// One labelled panel on a page: a caps heading, its rows, and a quiet footnote.
struct PanelSection<Content: View>: View {
    var title: String?
    var footer: String?
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let title { SectionLabel(text: title).padding(.horizontal, 4) }
            Panel { content }
            if let footer {
                Text(footer).font(.silo(12.5)).foregroundStyle(Theme.ink3).padding(.horizontal, 4).fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

/// A panel's row: a glyph, title and detail, whatever sits at the end. Tappable with `action`.
struct PanelRow<Trailing: View>: View {
    var symbol: String?
    let title: String
    var detail: String?
    var mono = false
    var chevron = false
    var action: (() -> Void)?
    @ViewBuilder var trailing: Trailing

    init(symbol: String? = nil, title: String, detail: String? = nil, mono: Bool = false, chevron: Bool = false,
         action: (() -> Void)? = nil, @ViewBuilder trailing: () -> Trailing = { EmptyView() }) {
        self.symbol = symbol
        self.title = title
        self.detail = detail
        self.mono = mono
        self.chevron = chevron
        self.action = action
        self.trailing = trailing()
    }

    private var row: some View {
        HStack(spacing: 12) {
            if let symbol {
                Image(systemName: symbol).font(.system(size: 15)).foregroundStyle(Theme.ink2).frame(width: 22)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.silo(15, .medium)).foregroundStyle(Theme.ink).lineLimit(2)
                if let detail, !detail.isEmpty {
                    Text(detail)
                        .font(mono ? .siloMono(12.5) : .silo(12.5))
                        .foregroundStyle(Theme.ink3)
                        .lineLimit(2)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            trailing
            if chevron {
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frame(minHeight: 48)
        .contentShape(Rectangle())
    }

    var body: some View {
        if let action {
            Button(action: action) { row }.buttonStyle(RowPressStyle())
        } else {
            row
        }
    }
}

/// A row presses by toning, not by scaling.
struct RowPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? Theme.well : .clear)
            .animation(Motion.press, value: configuration.isPressed)
    }
}

/// A small `well` chip: a role, a kind, a state with no lamp of its own.
struct Chip: View {
    let text: String
    var tint: Color = Theme.ink2
    var fill: Color = Theme.well

    var body: some View {
        Text(text)
            .font(.silo(11.5, .medium))
            .foregroundStyle(tint)
            .padding(.horizontal, 7)
            .frame(height: 20)
            .background(fill, in: RoundedRectangle(cornerRadius: Theme.Radius.xs, style: .continuous))
    }
}

/// A one-of-many choice as chips: resting surface with a ring, picked `cobaltPale` with a cobalt ring.
struct ChipRadio<Value: Hashable>: View {
    let options: [(value: Value, label: String)]
    @Binding var selection: Value

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(options, id: \.value) { option in
                    let on = option.value == selection
                    let shape = RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous)
                    Button {
                        withAnimation(Motion.quiet(0.2)) { selection = option.value }
                    } label: {
                        Text(option.label)
                            .font(.silo(13, .medium))
                            .foregroundStyle(on ? Theme.ink : Theme.ink2)
                            .padding(.horizontal, 12)
                            .frame(height: 34)
                            .background(on ? Theme.cobaltPale : Theme.surface, in: shape)
                            .overlay(shape.strokeBorder(on ? Theme.cobalt : Theme.lineStrong, lineWidth: 1))
                    }
                    .buttonStyle(.press)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
        }
        .sensoryFeedback(.selection, trigger: selection)
    }
}

/// A segmented control whose lit pill slides: Calls | Results, Private | Public, the rule's four ways.
struct Segmented<Value: Hashable>: View {
    let options: [(value: Value, label: String)]
    @Binding var selection: Value
    @Namespace private var pill

    var body: some View {
        HStack(spacing: 2) {
            ForEach(options, id: \.value) { option in
                let on = option.value == selection
                Button {
                    withAnimation(Motion.quiet(0.28)) { selection = option.value }
                } label: {
                    Text(option.label)
                        .font(.silo(13, .medium))
                        .foregroundStyle(on ? Theme.ink : Theme.ink3)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .frame(maxWidth: .infinity)
                        .frame(height: 32)
                        .background {
                            if on {
                                RoundedRectangle(cornerRadius: Theme.Radius.sm - 1, style: .continuous)
                                    .fill(Theme.surface)
                                    .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm - 1, style: .continuous).stroke(Theme.line))
                                    .matchedGeometryEffect(id: "pill", in: pill)
                            }
                        }
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .padding(3)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
        .sensoryFeedback(.selection, trigger: selection)
    }
}

/// Nothing here yet: one sentence, and the one next step.
struct EmptyWell<Action: View>: View {
    var symbol: String?
    let title: String
    var message: String?
    @ViewBuilder var action: Action

    init(symbol: String? = nil, title: String, message: String? = nil, @ViewBuilder action: () -> Action = { EmptyView() }) {
        self.symbol = symbol
        self.title = title
        self.message = message
        self.action = action()
    }

    var body: some View {
        VStack(spacing: 8) {
            if let symbol {
                Image(systemName: symbol).font(.system(size: 20)).foregroundStyle(Theme.ink3).padding(.bottom, 4)
            }
            Text(title).font(.silo(15, .semibold)).foregroundStyle(Theme.ink).multilineTextAlignment(.center)
            if let message {
                Text(message).font(.silo(14)).foregroundStyle(Theme.ink2).multilineTextAlignment(.center)
            }
            action.padding(.top, 8)
        }
        .padding(24)
        .frame(maxWidth: .infinity)
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}

/// A failure in place, a sentence in vermilion.
struct ErrorLine: View {
    let text: String
    var body: some View {
        Text(text).font(.silo(13)).foregroundStyle(Theme.vermilion).frame(maxWidth: .infinity, alignment: .leading).fixedSize(horizontal: false, vertical: true)
    }
}

/// An input: `surface`, an inset ring, the label above it always in view; focus is cobalt.
struct SiloField: View {
    var label: String?
    var prompt = ""
    @Binding var text: String
    var mono = false
    var secure = false
    var axis: Axis = .horizontal
    var lines: ClosedRange<Int> = 1...1
    var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let label { Text(label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3) }
            Group {
                if secure {
                    SecureField("", text: $text, prompt: placeholder)
                } else {
                    TextField("", text: $text, prompt: placeholder, axis: axis).lineLimit(lines)
                }
            }
            .font(mono ? .siloMono(15) : .silo(16))
            .foregroundStyle(Theme.ink)
            .tint(Theme.cobalt)
            .focused($focused)
            .padding(.horizontal, 12)
            .padding(.vertical, 11)
            .frame(minHeight: 44)
            .siloInputFrame(focused: focused, error: error != nil)
            if let error { Text(error).font(.silo(12)).foregroundStyle(Theme.vermilion) }
        }
    }

    private var placeholder: Text { Text(prompt).foregroundStyle(Theme.ink3) }
}

extension View {
    /// The input's ground and rings, for a field that lays itself out (a `TextEditor`, a picker).
    func siloInputFrame(focused: Bool = false, error: Bool = false) -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        return background(Theme.surface, in: shape)
            .overlay(shape.strokeBorder(error ? Theme.vermilion : focused ? Theme.cobalt : Theme.lineStrong, lineWidth: 1))
            .overlay {
                if focused { shape.inset(by: -3).stroke(Theme.cobalt, lineWidth: 2).opacity(0.9) }
            }
            .animation(Motion.quiet(0.16), value: focused)
    }
}

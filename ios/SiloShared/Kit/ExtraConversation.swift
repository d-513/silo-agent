import SiloClient
import SwiftUI

// What the conversation-side pages (Dashboard, Automations, Artifacts, Memories, Knowledge, Feed,
// Subagents) share beyond the kit.

/// `ArmedButton` reduced to a glyph, for a row with no room for a word: quiet at rest, the first
/// press opens it into a vermilion "Tap again" with the 3s bar, the second does it.
struct ArmedGlyph: View {
    let symbol: String
    let label: String
    var armedTitle = "Tap again"
    let action: () -> Void
    @State private var armed = false
    @State private var drained = false
    @State private var timer: Task<Void, Never>?

    var body: some View {
        Button {
            guard armed else { return arm() }
            timer?.cancel()
            withAnimation(Motion.state) { armed = false }
            action()
        } label: {
            HStack(spacing: 5) {
                Image(systemName: symbol).font(.system(size: 13))
                if armed {
                    Text(armedTitle).font(.silo(12.5, .medium)).lineLimit(1).fixedSize().transition(.opacity)
                }
            }
            .foregroundStyle(armed ? Theme.onAccent : Theme.ink3)
            .padding(.horizontal, armed ? 10 : 0)
            .frame(minWidth: 34, minHeight: 34)
            .background(armed ? Theme.vermilion : .clear)
            .overlay(alignment: .bottomLeading) {
                if armed {
                    GeometryReader { box in
                        Theme.onAccent.frame(width: drained ? 0 : box.size.width, height: 3)
                            .frame(maxHeight: .infinity, alignment: .bottom)
                    }
                }
            }
            .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            .contentShape(Rectangle())
        }
        .buttonStyle(.press)
        .sensoryFeedback(.warning, trigger: armed) { _, on in on }
        .accessibilityLabel(label)
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

/// A fold's chevron: it turns 90° over 260ms, whatever the fold's own animation.
struct FoldChevron: View {
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

/// The search field of a page: the system's would need the bar Silo took away.
struct SearchBox: View {
    var prompt = "Search"
    @Binding var text: String
    var searching = false
    let onSearch: () -> Void
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: 8) {
            Group {
                if searching {
                    Spinner(size: 14)
                } else {
                    Image(systemName: "magnifyingglass").font(.system(size: 14)).foregroundStyle(Theme.ink3)
                }
            }
            .frame(width: 18)
            TextField("", text: $text, prompt: Text(prompt).foregroundStyle(Theme.ink3))
                .font(.silo(16))
                .foregroundStyle(Theme.ink)
                .tint(Theme.cobalt)
                .focused($focused)
                .submitLabel(.search)
                .autocorrectionDisabled()
                .onSubmit(onSearch)
            if !text.isEmpty {
                Button { text = "" } label: {
                    Image(systemName: "xmark").font(.system(size: 12, weight: .medium))
                }
                .buttonStyle(.glyph(tint: Theme.ink3, side: 30))
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, 6)
        .frame(minHeight: 44)
        .siloInputFrame(focused: focused)
    }
}

/// The Taskboard's square check, which a checklist widget shares: an emerald fill when done.
struct TaskCheck: View {
    let done: Bool
    var size: CGFloat = 15

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 4, style: .continuous)
        ZStack {
            if done {
                shape.fill(Theme.emerald)
                Image(systemName: "checkmark")
                    .font(.system(size: size * 0.6, weight: .heavy))
                    .foregroundStyle(Theme.onAccent)
                    .transition(.scale(scale: 0.6).combined(with: .opacity))
            } else {
                shape.strokeBorder(Theme.lineStrong, lineWidth: 1)
            }
        }
        .frame(width: size, height: size)
        .animation(Motion.quiet(0.2), value: done)
        .accessibilityLabel(done ? "Done" : "Open")
    }
}

/// Chips and buttons that wrap, as `flex-wrap` does on the web.
struct FlowRow: Layout {
    var spacing: CGFloat = 6
    var centered = false

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        lines(subviews, width: proposal.width ?? .infinity).size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let flow = lines(subviews, width: bounds.width)
        var y = bounds.minY
        for line in flow.lines {
            var x = bounds.minX + (centered ? max(0, (bounds.width - line.width) / 2) : 0)
            for index in line.items {
                let size = fit(subviews[index], width: bounds.width)
                subviews[index].place(at: CGPoint(x: x, y: y + (line.height - size.height) / 2), proposal: ProposedViewSize(size))
                x += size.width + spacing
            }
            y += line.height + spacing
        }
    }

    /// As wide as it likes, up to the row: a long label wraps rather than running off the edge.
    private func fit(_ view: LayoutSubview, width: CGFloat) -> CGSize {
        view.sizeThatFits(ProposedViewSize(width: width.isFinite ? width : nil, height: nil))
    }

    private struct Line {
        var items: [Int] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    private func lines(_ subviews: Subviews, width: CGFloat) -> (lines: [Line], size: CGSize) {
        var out: [Line] = []
        var line = Line()
        for index in subviews.indices {
            let size = fit(subviews[index], width: width)
            if !line.items.isEmpty, line.width + spacing + size.width > width {
                out.append(line)
                line = Line()
            }
            line.width += (line.items.isEmpty ? 0 : spacing) + size.width
            line.height = max(line.height, size.height)
            line.items.append(index)
        }
        if !line.items.isEmpty { out.append(line) }
        let height = out.map(\.height).reduce(0, +) + spacing * CGFloat(max(0, out.count - 1))
        return (out, CGSize(width: out.map(\.width).max() ?? 0, height: height))
    }
}

/// A body that scrolls only past `maxHeight`, and otherwise takes just the room it needs.
struct CappedScroll<Content: View>: View {
    let maxHeight: CGFloat
    @ViewBuilder let content: Content

    var body: some View {
        ViewThatFits(in: .vertical) {
            content
            ScrollView { content }
        }
        .frame(maxHeight: maxHeight)
    }
}

/// A number from a short range, chosen from a Silo menu: a minute, an hour, a day of the month.
struct NumberMenu: View {
    let range: ClosedRange<Int>
    @Binding var value: Int
    /// "09", for a clock.
    var padded = false

    private func word(_ n: Int) -> String { padded ? String(format: "%02d", n) : String(n) }

    var body: some View {
        SiloMenu {
            ForEach(Array(range), id: \.self) { n in
                MenuRow(title: word(n), checked: n == value) { value = n }
            }
        } label: {
            HStack(spacing: 6) {
                Text(word(value)).font(.siloMono(15)).monospacedDigit().foregroundStyle(Theme.ink)
                Image(systemName: "chevron.up.chevron.down").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 12)
            .frame(height: 44)
            .siloInputFrame()
        }
    }
}

private struct ConfirmItem<Item>: ViewModifier {
    @Binding var item: Item?
    let title: (Item) -> String
    let message: String?
    let action: (Item) -> String
    let perform: (Item) -> Void
    /// What the slip is about: `item` is gone by the time the slip has slid away, and its words stay.
    @State private var held: Item?

    func body(content: Content) -> some View {
        content
            .confirmSlip(
                Binding(get: { item != nil }, set: { if !$0 { item = nil } }),
                title: held.map(title) ?? "", message: message, action: held.map(action) ?? ""
            ) {
                if let held { perform(held) }
            }
            .onChange(of: item != nil) { if let item { held = item } }
    }
}

extension View {
    /// `confirmSlip` for a thing: shown while it is set, and handed to `perform` once confirmed.
    func confirmSlip<Item>(item: Binding<Item?>, title: @escaping (Item) -> String, message: String? = nil,
                           action: @escaping (Item) -> String, perform: @escaping (Item) -> Void) -> some View {
        modifier(ConfirmItem(item: item, title: title, message: message, action: action, perform: perform))
    }
}

/// When things happened, in the words the web uses (`web/src/format.ts`).
enum When {
    static func clock(_ date: Date) -> String { date.formatted(date: .omitted, time: .shortened) }

    static func clock(_ iso: String) -> String { isoDate(iso).map { clock($0) } ?? "" }

    /// A post's or an ask's time: the clock today, the date and clock before.
    static func stamp(_ iso: String) -> String {
        guard let date = isoDate(iso) else { return "" }
        if Calendar.current.isDateInToday(date) { return clock(date) }
        return "\(date.formatted(.dateTime.month(.abbreviated).day())), \(clock(date))"
    }

    /// A run's time: "Today 09:12", "Yesterday 18:00", "Tue 07:30", "Mar 4 07:30".
    static func run(_ iso: String) -> String {
        guard let date = isoDate(iso) else { return "" }
        let calendar = Calendar.current
        let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: date), to: calendar.startOfDay(for: Date())).day ?? 0
        switch days {
        case 0: return "Today \(clock(date))"
        case 1: return "Yesterday \(clock(date))"
        case 2..<7: return "\(date.formatted(.dateTime.weekday(.abbreviated))) \(clock(date))"
        default: return "\(date.formatted(.dateTime.month(.abbreviated).day())) \(clock(date))"
        }
    }

    /// When something fires next: "Tue, Oct 4 09:12".
    static func next(_ iso: String) -> String {
        guard let date = isoDate(iso) else { return "" }
        return "\(date.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day())) \(clock(date))"
    }

    /// How long a subagent has been at it: "12s", "4m", "1h05m".
    static func elapsed(_ iso: String, now: Date) -> String {
        guard let date = isoDate(iso) else { return "" }
        let s = max(0, Int(now.timeIntervalSince(date).rounded()))
        if s < 60 { return "\(s)s" }
        if s < 3600 { return "\(s / 60)m" }
        return "\(s / 3600)h\(String(format: "%02d", (s % 3600) / 60))m"
    }
}

import SiloClient
import SwiftUI

// What the Bot puts in a chat besides words: a question to answer in place, setups it offers,
// and the marks a slash command or the interview leave.

/// A question the Bot asked in this chat (ask_owner), or one asked from elsewhere while the owner
/// was away. The upper half is the Bot speaking: a meta line, the question in its serif, its context.
/// The lower half is the owner's reply tray on `well`, from the first control down, with one submit.
/// It is a question, not an alarm: cobalt, never vermilion. Answered or put away the tray goes, the
/// question turns `ink2`, and the answer is the owner's next turn in the thread.
struct ThreadAskCard: View {
    @Environment(AppModel.self) private var model
    @FocusState private var typing: Bool
    let card: Silo_V1_DashboardCard
    let asked: Bool
    @State private var values: [String: AskValue]
    @State private var note = ""
    @State private var busy = Busy.idle

    private enum Busy { case idle, submit, dismiss }

    init(card: Silo_V1_DashboardCard, asked: Bool) {
        self.card = card
        self.asked = asked
        _values = State(initialValue: parseAskBlocks(card.blocksJson).defaults)
    }

    private var closed: Bool { card.status != "open" }
    private var answered: Bool { card.status == "submitted" }
    private var question: String { card.summary.trimmingCharacters(in: .whitespacesAndNewlines) }
    /// ask_owner titles a card with its question's first line when the Bot gave it none; shown above
    /// the question, it would only say it twice.
    private var heading: String {
        let title = card.title.trimmingCharacters(in: .whitespaces)
        return question.hasPrefix(title) ? "" : title
    }

    var body: some View {
        let blocks = parseAskBlocks(card.blocksJson).blocks
        // An answered card keeps what it showed; its controls would read as the owner's answer.
        let shown = closed ? blocks.filter { !$0.isControl } : blocks
        let cut = shown.firstIndex(where: \.isControl) ?? shown.count
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)

        VStack(alignment: .leading, spacing: 0) {
            speaking(Array(shown[..<cut]))
            if !closed {
                tray(Array(shown[cut...])).transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface)
        .clipShape(shape)
        .overlay(shape.strokeBorder(Theme.line, lineWidth: 1))
        .animation(Motion.fold, value: card.status)
        .onChange(of: card.blocksJson) { _, json in
            values = parseAskBlocks(json).defaults.merging(values) { _, mine in mine }
        }
        .sensoryFeedback(.success, trigger: answered) { _, now in now }
    }

    // MARK: The Bot speaking

    private func speaking(_ context: [AskBlock]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            meta
            if !heading.isEmpty {
                Text(heading)
                    .font(.silo(15, .semibold))
                    .tracking(-0.15)
                    .foregroundStyle(Theme.ink)
                    .padding(.top, 12)
            }
            if !question.isEmpty {
                ProseView(text: question, size: 16, leading: 26, tone: closed ? Theme.ink2 : Theme.ink)
                    .padding(.top, heading.isEmpty ? 12 : 4)
            }
            if !context.isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    ForEach(context) { AskContext(block: $0) }
                }
                .padding(.top, 12)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var meta: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                state
                Spacer(minLength: 8)
                Text(askStamp(card.updatedAt.isEmpty ? card.createdAt : card.updatedAt))
                    .font(.siloMono(11.5))
                    .foregroundStyle(Theme.ink3)
            }
            if asked {
                Text(whereAsked).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
            }
        }
    }

    @ViewBuilder
    private var state: some View {
        if closed {
            HStack(spacing: 5) {
                if answered {
                    Image(systemName: "checkmark").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.emerald)
                }
                Text(answered ? "Answered" : "Put away").font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
            }
            .transition(.opacity)
        } else {
            HStack(spacing: 5) {
                Image(systemName: "questionmark.bubble").font(.system(size: 13))
                Text("Question for you").font(.silo(12, .medium))
            }
            .foregroundStyle(Theme.cobalt)
            .transition(.opacity)
        }
    }

    /// The Bot asked from elsewhere and the question was put into this chat: only then is its source
    /// worth naming.
    private var whereAsked: String {
        let source: String
        switch card.sourceKind {
        case "main": source = "Main chat"
        case "automation": source = "Automation “\(card.sourceName)”"
        case "channel": source = "Channel “\(card.sourceName)”"
        case "chat": source = card.sourceName.isEmpty ? "Chat" : card.sourceName
        case "hidden": source = "Someone else's chat"
        default: source = ""
        }
        return source.isEmpty ? "Asked while you were away" : "Asked while you were away · \(source)"
    }

    // MARK: The reply tray

    private func tray(_ form: [AskBlock]) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            // The controls lock during a send; Send itself keeps its ink, so its spinner shows.
            VStack(alignment: .leading, spacing: 16) {
                ForEach(form) { AskControl(block: $0, values: $values) }
                SiloField(prompt: "Add a note (optional)", text: $note, axis: .vertical, lines: 1...5)
            }
            .focused($typing)
            .disabled(busy != .idle)
            HStack(spacing: 6) {
                Button { send() } label: {
                    HStack(spacing: 6) {
                        ZStack {
                            if busy == .submit {
                                Spinner(size: 12, color: Theme.onInk).transition(.opacity)
                            } else {
                                Image(systemName: "paperplane").font(.system(size: 12)).transition(.opacity)
                            }
                        }
                        .frame(width: 14)
                        Text(card.submitLabel.isEmpty ? "Send" : card.submitLabel)
                    }
                }
                .buttonStyle(.ink)
                .controlSize(.small)
                // ⌘Return answers from inside the tray, as in the composer. Only from inside it, so
                // a card left open up the thread never catches what was meant for the composer.
                .keyboardShortcut(typing ? KeyboardShortcut(.return, modifiers: .command) : nil)
                Button("Not now") { put() }
                    .buttonStyle(TrayButtonStyle())
                    .disabled(busy != .idle)
            }
            .animation(Motion.state, value: busy)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well)
        .overlay(alignment: .top) { Theme.line.frame(height: 1) }
    }

    private func send() {
        guard busy == .idle else { return }
        busy = .submit
        Task {
            _ = await model.answerCard(card, valuesJSON: encodeAskValues(values), note: note)
            busy = .idle
        }
    }

    private func put() {
        guard busy == .idle else { return }
        busy = .dismiss
        Task {
            await model.dismissCard(card)
            busy = .idle
        }
    }
}

/// A quiet button that still answers a press on the `well` of the tray: `pressed`, not `well`.
private struct TrayButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.silo(12.5, .medium))
            .foregroundStyle(configuration.isPressed ? Theme.ink : enabled ? Theme.ink2 : Theme.ink3)
            .padding(.horizontal, 10)
            .frame(minHeight: 32)
            .background(configuration.isPressed ? Theme.pressed : .clear,
                        in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(Motion.press, value: configuration.isPressed)
    }
}

/// The web's `feedStamp`: the time today, else the day and the time.
private func askStamp(_ iso: String) -> String {
    guard let date = parseDate(iso) else { return "" }
    if Calendar.current.isDateInToday(date) { return date.formatted(date: .omitted, time: .shortened) }
    return date.formatted(.dateTime.month(.abbreviated).day().hour().minute())
}

/// The Bot's context above the tray: markdown, labelled rows, metrics, a diff.
private struct AskContext: View {
    let block: AskBlock

    var body: some View {
        switch block.type {
        case "markdown":
            if !block.text.isEmpty { ProseView(text: block.text, size: 13.5, leading: 21) }
        case "kv":
            if !block.pairs.isEmpty { rows }
        case "metrics":
            if !block.pairs.isEmpty { metrics }
        case "diff":
            diff
        default:
            EmptyView()
        }
    }

    private var rows: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(block.pairs.enumerated()), id: \.offset) { index, pair in
                if index > 0 { Theme.lineStrong.frame(height: 1) }
                HStack(alignment: .firstTextBaseline, spacing: 16) {
                    Text(pair.label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
                    Text(pair.value)
                        .font(.siloMono(12.5))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .frame(maxWidth: .infinity, alignment: .trailing)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }
        }
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }

    private var metrics: some View {
        LazyVGrid(columns: [GridItem(.flexible(), spacing: 8), GridItem(.flexible(), spacing: 8)], alignment: .leading, spacing: 8) {
            ForEach(Array(block.pairs.enumerated()), id: \.offset) { _, pair in
                VStack(alignment: .leading, spacing: 2) {
                    Text(pair.label).font(.silo(11.5, .medium)).foregroundStyle(Theme.ink3).lineLimit(1)
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(pair.value).font(.siloMono(16, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                        if !pair.delta.isEmpty {
                            Text(pair.delta).font(.siloMono(11, .medium)).foregroundStyle(tone(pair.tone)).lineLimit(1)
                        }
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            }
        }
    }

    private func tone(_ name: String) -> Color {
        switch name {
        case "good": Theme.emerald
        case "bad": Theme.vermilion
        case "warn": Theme.ink2
        default: Theme.ink3
        }
    }

    private var diff: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !block.path.isEmpty {
                Text(block.path)
                    .font(.siloMono(11.5, .medium))
                    .foregroundStyle(Theme.ink2)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
            }
            ScrollView([.horizontal, .vertical], showsIndicators: false) {
                Text(block.text)
                    .font(.siloMono(12))
                    .foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: 224)
            .fixedSize(horizontal: false, vertical: true)
        }
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

/// One control in the tray. Its value goes into the submitted form.
private struct AskControl: View {
    let block: AskBlock
    @Binding var values: [String: AskValue]

    private var flag: Binding<Bool> {
        Binding(get: { if case .bool(let on) = values[block.name] { return on }; return false },
                set: { values[block.name] = .bool($0) })
    }

    private var word: Binding<String> {
        Binding(get: { if case .string(let text) = values[block.name] { return text }; return "" },
                set: { values[block.name] = .string($0) })
    }

    private var list: [String] {
        if case .list(let list) = values[block.name] { return list }
        return []
    }

    var body: some View {
        switch block.type {
        case "switch":
            Toggle(isOn: flag) {
                VStack(alignment: .leading, spacing: 1) {
                    Text(block.label).font(.silo(13, .medium)).foregroundStyle(Theme.ink)
                    if !block.hint.isEmpty {
                        Text(block.hint).font(.silo(12)).foregroundStyle(Theme.ink3)
                    }
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).strokeBorder(Theme.line, lineWidth: 1))
        case "select":
            labelled(inside: true) {
                SiloPicker(label: block.label.isEmpty ? nil : block.label,
                           options: block.options.map { (value: $0.value, label: $0.label) },
                           selection: word)
            }
        case "segmented":
            labelled {
                AskChoices(options: block.options, picked: [word.wrappedValue]) { word.wrappedValue = $0 }
            }
        case "checkboxes":
            labelled {
                AskChoices(options: block.options, picked: list, many: true) { value in
                    var next = list
                    if next.contains(value) { next.removeAll { $0 == value } } else { next.append(value) }
                    values[block.name] = .list(next)
                }
            }
        case "input", "textarea":
            labelled(inside: true) {
                SiloField(label: block.label.isEmpty ? nil : block.label,
                          prompt: block.placeholder,
                          text: word,
                          axis: block.type == "textarea" ? .vertical : .horizontal,
                          lines: block.type == "textarea" ? 3...8 : 1...1)
            }
        default:
            EmptyView()
        }
    }

    /// A control with its label above and its hint below. `inside`: the control draws its own label.
    private func labelled(inside: Bool = false, @ViewBuilder control: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if !inside, !block.label.isEmpty {
                Text(block.label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3)
            }
            control()
            if !block.hint.isEmpty {
                Text(block.hint).font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

/// A question's options, one or many. Short plain labels sit as chips that wrap; a hint or a long
/// label makes them rows with room for a sentence. Picked is `cobaltPale` on a cobalt ring: it is
/// yours now.
private struct AskChoices: View {
    let options: [AskBlock.Option]
    let picked: [String]
    var many = false
    let pick: (String) -> Void

    private var chips: Bool {
        options.count <= 12 && options.allSatisfy { $0.hint.isEmpty && $0.label.count <= 28 }
    }

    var body: some View {
        Group {
            if chips {
                WrapLayout(spacing: 6) { choices }
            } else {
                VStack(spacing: 6) { choices }
            }
        }
        .sensoryFeedback(.selection, trigger: picked)
    }

    private var choices: some View {
        ForEach(options, id: \.value) { option in
            let on = picked.contains(option.value)
            let shape = RoundedRectangle(cornerRadius: chips ? Theme.Radius.sm : Theme.Radius.control, style: .continuous)
            Button {
                withAnimation(Motion.quiet(0.2)) { pick(option.value) }
            } label: {
                HStack(alignment: chips ? .center : .top, spacing: chips ? 8 : 10) {
                    ChoiceMark(many: many, on: on).padding(.top, chips ? 0 : 1)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(option.label)
                            .font(.silo(13, .medium))
                            .foregroundStyle(on ? Theme.ink : Theme.ink2)
                            .multilineTextAlignment(.leading)
                        if !chips, !option.hint.isEmpty {
                            Text(option.hint)
                                .font(.silo(12))
                                .foregroundStyle(Theme.ink3)
                                .multilineTextAlignment(.leading)
                        }
                    }
                    if !chips { Spacer(minLength: 0) }
                }
                .padding(.leading, chips ? 10 : 12)
                .padding(.trailing, 12)
                .padding(.vertical, chips ? 0 : 10)
                .frame(minHeight: chips ? 34 : nil)
                .background(on ? Theme.cobaltPale : Theme.surface, in: shape)
                .overlay(shape.strokeBorder(on ? Theme.cobalt : Theme.lineStrong, lineWidth: 1))
                .contentShape(shape)
            }
            .buttonStyle(.press)
            .accessibilityAddTraits(on ? .isSelected : [])
        }
    }
}

/// A choice's 14pt box (many) or dot (one). Picked, it fills cobalt and its tick settles in.
private struct ChoiceMark: View {
    let many: Bool
    let on: Bool

    var body: some View {
        let fill = on ? Theme.cobalt : Theme.surface
        ZStack {
            if many {
                let box = RoundedRectangle(cornerRadius: 4, style: .continuous)
                box.fill(fill).overlay(box.strokeBorder(on ? Theme.cobalt : Theme.lineStrong, lineWidth: 1))
            } else {
                Circle().fill(fill).overlay(Circle().strokeBorder(on ? Theme.cobalt : Theme.lineStrong, lineWidth: 1))
            }
            if on {
                Group {
                    if many {
                        Image(systemName: "checkmark").font(.system(size: 8.5, weight: .bold))
                    } else {
                        Circle().frame(width: 6, height: 6)
                    }
                }
                .foregroundStyle(Theme.onAccent)
                .transition(.scale(scale: 0.4).combined(with: .opacity))
            }
        }
        .frame(width: 14, height: 14)
        .animation(Motion.settle(0.2), value: on)
    }
}

/// Lines of chips, wrapping at the width it is given.
private struct WrapLayout: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let lines = lines(subviews, width: proposal.width ?? .infinity)
        let width = lines.map(\.width).max() ?? 0
        let height = lines.reduce(0) { $0 + $1.height } + spacing * CGFloat(max(lines.count - 1, 0))
        return CGSize(width: proposal.width ?? width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for line in lines(subviews, width: bounds.width) {
            var x = bounds.minX
            for index in line.items {
                let size = subviews[index].sizeThatFits(.unspecified)
                subviews[index].place(at: CGPoint(x: x, y: y + (line.height - size.height) / 2), proposal: ProposedViewSize(size))
                x += size.width + spacing
            }
            y += line.height + spacing
        }
    }

    private struct Line {
        var items: [Int] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    private func lines(_ subviews: Subviews, width: CGFloat) -> [Line] {
        var out: [Line] = []
        var line = Line()
        for index in subviews.indices {
            let size = subviews[index].sizeThatFits(.unspecified)
            let grown = line.items.isEmpty ? size.width : line.width + spacing + size.width
            if !line.items.isEmpty, grown > width {
                out.append(line)
                line = Line(items: [index], width: size.width, height: size.height)
            } else {
                line.items.append(index)
                line.width = grown
                line.height = max(line.height, size.height)
            }
        }
        if !line.items.isEmpty { out.append(line) }
        return out
    }
}

// MARK: - Setup offers

/// What the Bot offered to add in one turn: each offer with why and what it filled in. Set up adds
/// a library connector here; a channel or a drive is finished on the web client, whose forms they
/// need. Not now tells the Bot once the turn's offers are all answered.
struct OfferGroup: View {
    @Environment(AppModel.self) private var model
    let offers: [Silo_V1_SetupOffer]

    var body: some View {
        let open = offers.filter { $0.status == "open" }
        VStack(alignment: .leading, spacing: 8) {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 300), spacing: 10, alignment: .top)], alignment: .leading, spacing: 10) {
                ForEach(offers, id: \.id) { offer in OfferCard(offer: offer) }
            }
            .frame(maxWidth: offers.count > 1 ? .infinity : 460, alignment: .leading)
            if open.count > 1 {
                Button("Not now for these") {
                    Task { await model.resolveOffer(open[0], outcome: "skipped", rest: true) }
                }
                .buttonStyle(.ghost)
                .controlSize(.small)
                .transition(.opacity)
            }
        }
        .animation(Motion.state, value: open.count)
    }
}

private struct OfferCard: View {
    @Environment(AppModel.self) private var model
    let offer: Silo_V1_SetupOffer
    @State private var setting = false

    private var symbol: String {
        switch offer.kind {
        case "channel": "dot.radiowaves.left.and.right"
        case "drive": "externaldrive"
        default: "puzzlepiece.extension"
        }
    }

    private var kindLabel: String {
        switch offer.kind {
        case "channel": "Channel"
        case "drive": "Drive"
        default: "Connector"
        }
    }

    /// What the Bot filled in, the name first.
    private var filled: [(key: String, value: String)] {
        offer.options.sorted { $0.key == "name" ? $1.key != "name" : $1.key == "name" ? false : $0.key < $1.key }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: symbol)
                    .font(.system(size: 18))
                    .foregroundStyle(Theme.ink2)
                    .frame(width: 44, height: 44)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                VStack(alignment: .leading, spacing: 1) {
                    Text(kindLabel.uppercased())
                        .font(.silo(11.5, .medium))
                        .foregroundStyle(Theme.ink3)
                    Text(offer.label.isEmpty ? offer.target : offer.label)
                        .font(.silo(15, .medium))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(2)
                }
                Spacer(minLength: 0)
            }
            .padding(14)
            if !offer.why.isEmpty {
                Text(offer.why)
                    .font(.silo(13.5))
                    .lineHeight(.multiple(factor: 20 / 17.55))
                    .foregroundStyle(Theme.ink2)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 14)
                    .padding(.bottom, 10)
            }
            if !filled.isEmpty {
                Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 12, verticalSpacing: 4) {
                    ForEach(filled, id: \.key) { key, value in
                        GridRow {
                            Text(optionWord(key)).font(.silo(12.5)).foregroundStyle(Theme.ink3)
                            Text(shownValue(value))
                                .font(.silo(12.5))
                                .foregroundStyle(Theme.ink)
                                .lineLimit(1)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                .padding(.horizontal, 14)
                .padding(.bottom, 12)
            }
            foot
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).card())
        .sheet(isPresented: $setting) { OfferSetupSheet(offer: offer) }
        .sensoryFeedback(.success, trigger: offer.status) { _, new in new == "done" }
    }

    private var foot: some View {
        HStack(spacing: 6) {
            switch offer.status {
            case "open":
                Button { setting = true } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "plus").font(.system(size: 12, weight: .medium))
                        Text("Set up")
                    }
                }
                .buttonStyle(.ink)
                .controlSize(.small)
                .transition(.opacity)
                Button("Not now") { Task { await model.resolveOffer(offer, outcome: "skipped") } }
                    .buttonStyle(.ghost)
                    .controlSize(.small)
                    .transition(.opacity)
            case "done":
                HStack(spacing: 5) {
                    Image(systemName: "checkmark").font(.system(size: 12, weight: .semibold))
                    Text("Added").font(.silo(12.5, .medium))
                }
                .foregroundStyle(Theme.emerald)
                .frame(minHeight: 32)
                .transition(.opacity)
            default:
                Text("Not now")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .frame(minHeight: 32)
                    .transition(.opacity)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .overlay(alignment: .top) { Theme.line.frame(height: 1) }
        .animation(Motion.state, value: offer.status)
    }

    /// What the offer card calls each of the add form's own options (`setup/prefill.ts`).
    private func optionWord(_ key: String) -> String {
        switch key {
        case "name": "Name"
        case "default_mode": "Default rule"
        case "expose_completion": "Also chat tools"
        case "inbound": "Deliver messages to the Bot"
        case "enabled": "Enabled"
        case "prompt": "Prompt"
        case "read_only": "Read-only"
        default:
            key.replacingOccurrences(of: "_", with: " ").prefix(1).uppercased() + key.replacingOccurrences(of: "_", with: " ").dropFirst()
        }
    }

    private func shownValue(_ value: String) -> String {
        switch value {
        case "true": "Yes"
        case "false": "No"
        default: value
        }
    }
}

/// Setting an offer up. A library connector is added to the Bot from here (its sign-in, if it has
/// one, stays on the web); a channel or a drive opens the web client, where its form lives.
private struct OfferSetupSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let offer: Silo_V1_SetupOffer
    @State private var busy = false
    @State private var error: String?
    @State private var added: Silo_V1_BotConnector?
    @State private var web: IdentifiedURL?

    private var name: String { offer.label.isEmpty ? offer.target : offer.label }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: name, cancel: added == nil ? "Cancel" : "Done", onCancel: { dismiss() })
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    if offer.kind == "connector" {
                        connector
                    } else {
                        elsewhere
                    }
                    if let error {
                        ErrorLine(text: error).transition(.opacity)
                    }
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .disabled(busy)
        }
        .animation(Motion.state, value: added != nil)
        .animation(Motion.state, value: error)
        .sheet(item: $web) { item in SafariView(url: item.url).ignoresSafeArea() }
        .sensoryFeedback(.success, trigger: added != nil) { _, now in now }
        .siloSheet(detents: [.medium, .large])
    }

    @ViewBuilder
    private var connector: some View {
        if let added {
            HStack(spacing: 8) {
                Image(systemName: "checkmark").font(.system(size: 13, weight: .semibold)).foregroundStyle(Theme.emerald)
                Text("Added to \(model.selectedBot?.name ?? "the Bot")").font(.silo(15, .medium)).foregroundStyle(Theme.ink)
            }
            .transition(.rise)
            if added.authStatus == "needs_auth" {
                Text("It still needs its sign-in or settings. Finish that on the web client.")
                    .font(.silo(13))
                    .foregroundStyle(Theme.ink2)
                    .transition(.rise)
            }
        } else {
            Text("Adds \(name) from the library to \(model.selectedBot?.name ?? "this Bot") with its default settings.")
                .font(.silo(14))
                .foregroundStyle(Theme.ink2)
                .fixedSize(horizontal: false, vertical: true)
            Text("A connector that signs in or needs a key asks for it on the web client.")
                .font(.silo(12.5))
                .foregroundStyle(Theme.ink3)
                .fixedSize(horizontal: false, vertical: true)
            Button { Task { await addConnector() } } label: {
                HStack(spacing: 8) {
                    if busy { Spinner(size: 13, color: Theme.onInk).transition(.opacity) }
                    Text(busy ? "Adding…" : "Add to Bot")
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.ink)
            .animation(Motion.state, value: busy)
        }
    }

    @ViewBuilder
    private var elsewhere: some View {
        Text(offer.kind == "channel"
             ? "A channel is set up with the web client's form: it signs in and picks its conversation there."
             : "A drive is set up with the web client's form: it signs in to the provider there.")
            .font(.silo(14))
            .foregroundStyle(Theme.ink2)
            .fixedSize(horizontal: false, vertical: true)
        if let url = webURL {
            Button { web = IdentifiedURL(url: url) } label: {
                HStack(spacing: 8) {
                    Text("Open on the web")
                    Image(systemName: "arrow.up.right").font(.system(size: 12, weight: .medium))
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.quiet)
        }
        Button {
            Task {
                await model.resolveOffer(offer, outcome: "done")
                dismiss()
            }
        } label: {
            Text("I set it up").frame(maxWidth: .infinity)
        }
        .buttonStyle(.ink)
    }

    /// The chat on the web client, where the same offer card opens Silo's own form.
    private var webURL: URL? {
        guard let botID = model.selectedBotID else { return nil }
        var path = "\(model.serverURL)/bots/\(botID)/run"
        if !offer.chatID.isEmpty { path += "/\(offer.chatID)" }
        return URL(string: path)
    }

    private func addConnector() async {
        guard let botID = model.selectedBotID else { return }
        busy = true
        error = nil
        defer { busy = false }
        do {
            let library = try await model.client.listConnectors()
            guard let preset = library.first(where: { $0.identifier == offer.target }) else {
                error = "\(offer.label.isEmpty ? offer.target : offer.label) is not available on this server any more."
                return
            }
            added = try await model.client.attachConnector(botID: botID, connectorID: preset.id)
            await model.resolveOffer(offer, outcome: "done")
        } catch {
            self.error = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}

// MARK: - Marks in the thread

/// A hairline with a word on it: where something changed about the conversation.
private struct ThreadMark: View {
    let symbol: String
    let label: String
    var hint: String?
    var top: CGFloat = 8

    var body: some View {
        HStack(spacing: 12) {
            Theme.line.frame(height: 1)
            HStack(spacing: 6) {
                Image(systemName: symbol).font(.system(size: 12))
                Text(label).font(.silo(12, .medium))
            }
            .foregroundStyle(Theme.ink3)
            .fixedSize()
            Theme.line.frame(height: 1)
        }
        .padding(.top, top)
        .padding(.bottom, 4)
        .accessibilityElement(children: .combine)
        .accessibilityHint(hint ?? "")
    }
}

/// A slash command's mark: the context started over here, or messages were deleted for good.
struct BoundaryRow: View {
    let block: Block

    var body: some View {
        if block.source == "wiped" {
            ThreadMark(symbol: "trash",
                       label: "\(block.count) \(block.count == 1 ? "message" : "messages") wiped",
                       hint: "Deleted for good by you.",
                       top: 16)
        } else {
            ThreadMark(symbol: "eraser",
                       label: "Context cleared",
                       hint: "The Bot does not see anything above this line.",
                       top: 16)
        }
    }
}

/// Where the interview starts: its opening turn is Silo's prompt, not the owner's words.
struct InterviewMark: View {
    var body: some View {
        ThreadMark(symbol: "sparkles", label: "Getting to know you")
    }
}

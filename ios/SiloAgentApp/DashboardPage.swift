import SafariServices
import SiloClient
import SwiftUI

/// The owner's board: one line on when the Bot last looked, what waits on the owner, what it posted
/// since the last look, and the widgets it keeps current. The owner acts in place (checks, buttons);
/// changing the board itself is a request to the Bot through the ask bar, never a new chat.
struct DashboardPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var board: Silo_V1_ListDashboardResponse?
    /// The unread posts as they were when the page opened (reading marks them read at once).
    @State private var since: [Silo_V1_FeedPost] = []
    @State private var asking: Silo_V1_DashboardCard?
    @State private var focus: Silo_V1_DashboardWidget?
    @State private var removing: Silo_V1_DashboardWidget?
    @State private var app: IdentifiedURL?
    /// Widgets that changed keep their dot for the visit, though the server counts them read.
    @State private var changed: Set<String> = []
    /// What the last poll added: only that rises in.
    @State private var arrived: Set<String> = []
    @State private var draft = ""

    private static let suggestions: [(symbol: String, label: String)] = [
        ("envelope", "Mail that needs a reply"),
        ("calendar", "Today's calendar"),
        ("arrow.left.arrow.right", "What changed in /workspace"),
        ("globe", "Is my site up"),
    ]

    private var name: String { model.selectedBot?.name ?? "The Bot" }

    private var columns: [GridItem] {
        let column = GridItem(.flexible(), spacing: 10, alignment: .top)
        return sizeClass == .regular ? [column, column] : [column]
    }

    private var openCards: [Silo_V1_DashboardCard] {
        (board?.cards ?? []).filter { $0.status == "open" }.sorted { $0.priority < $1.priority }
    }

    private func blank(_ board: Silo_V1_ListDashboardResponse) -> Bool {
        board.widgets.isEmpty && openCards.isEmpty && model.approvals.isEmpty && since.isEmpty && !board.curating
    }

    var body: some View {
        PageScroll(spacing: 10) {
            if let board, !blank(board) { metaLine(board) }
            // Approvals wait here with the Bot's asks, as on the web: the ones paused in the
            // open chat also sit in its composer.
            if !model.approvals.isEmpty || !openCards.isEmpty {
                LazyVGrid(columns: columns, alignment: .leading, spacing: 10) {
                    ForEach(model.approvals) { approval in
                        ApprovalSlip(approval: approval)
                    }
                    ForEach(openCards, id: \.id) { card in
                        Button { asking = card } label: { AskRow(card: card) }
                            .buttonStyle(.press)
                            .rise(when: arrived.contains(card.id))
                    }
                }
            }
            if let board {
                if blank(board) {
                    emptyBoard
                } else {
                    if !since.isEmpty { SinceLooked(posts: since) }
                    // Until the Bot writes its own ideas, a few things to ask it.
                    if !board.widgets.contains(where: { $0.kind == "ideas" }) && board.widgets.count < 3 {
                        StarterIdeas()
                    }
                    widgets(board.widgets)
                }
            } else {
                SkeletonRows(count: 3, height: 132)
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            AskBar(focus: $focus, text: $draft, prompt: askPrompt) { text in await ask(text) }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) {
            await load(first: true)
            // Probes and the curator run on the server; poll while the page is open.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(board?.widgets.contains(where: \.busy) == true || board?.curating == true ? 3 : 15))
                guard !Task.isCancelled else { return }
                await load()
            }
        }
        .sheet(item: $asking) { card in
            AskSheet(card: card) { await load() }
        }
        .sheet(item: $app) { item in
            SafariView(url: item.url).ignoresSafeArea()
        }
        .confirmSlip(item: $removing, title: { _ in "Remove this widget?" }, message: "The Bot can put it back if you ask it to.",
                     action: { "Remove \($0.title)" }) { w in
            Task { await remove(w) }
        }
    }

    private var askPrompt: String {
        if focus != nil { return "What should change about it?" }
        if let board, blank(board) { return "What should \(name) watch for you?" }
        return "Change the board…"
    }

    // MARK: sections

    /// When the Bot last looked and when it looks next; while it works, the breathing lamp.
    private func metaLine(_ board: Silo_V1_ListDashboardResponse) -> some View {
        HStack(spacing: 8) {
            if board.curating {
                Lamp(status: .working)
                Text("\(name) is working on the board").lineLimit(1).shimmering()
                if !board.automationID.isEmpty {
                    Button("Watch") { model.path.append(.page(.automations)) }
                        .font(.silo(12.5, .medium))
                        .foregroundStyle(Theme.ink2)
                        .buttonStyle(.press)
                }
            } else {
                Text(board.curatedAt.isEmpty ? "Not curated yet" : "Curated \(When.run(board.curatedAt))").lineLimit(1)
                if !board.automationID.isEmpty {
                    Text("·").accessibilityHidden(true)
                    Button { model.path.append(.page(.automations)) } label: {
                        Text(board.nextAt.isEmpty ? "no schedule" : "next \(When.next(board.nextAt))")
                            .foregroundStyle(Theme.ink2)
                            .underline(pattern: .dot, color: Theme.lineStrong)
                            .lineLimit(1)
                    }
                    .buttonStyle(.press)
                    .accessibilityHint("Change when the Bot looks again")
                }
            }
            Spacer(minLength: 0)
        }
        .font(.silo(12.5))
        .foregroundStyle(Theme.ink3)
        .frame(minHeight: 28)
        .padding(.horizontal, 4)
    }

    private var emptyBoard: some View {
        VStack(spacing: 8) {
            Text("Nothing on the board yet")
                .font(.silo(30, .medium))
                .tracking(-0.6)
                .foregroundStyle(Theme.ink)
                .multilineTextAlignment(.center)
            Text("Say what you want to keep an eye on. \(name) builds it once and keeps it current.")
                .font(.silo(14))
                .foregroundStyle(Theme.ink2)
                .multilineTextAlignment(.center)
            FlowRow(spacing: 8, centered: true) {
                ForEach(Self.suggestions, id: \.label) { suggestion in
                    let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    Button {
                        draft = suggestion.label
                    } label: {
                        HStack(spacing: 8) {
                            Image(systemName: suggestion.symbol)
                                .font(.system(size: 10))
                                .frame(width: 18, height: 18)
                                .background(Theme.well, in: RoundedRectangle(cornerRadius: 5, style: .continuous))
                            Text(suggestion.label).font(.silo(13, .medium)).lineLimit(1)
                        }
                        .foregroundStyle(Theme.ink2)
                        .padding(.horizontal, 10)
                        .frame(height: 34)
                        .background(Theme.surface, in: shape)
                        .overlay(shape.stroke(Theme.line))
                    }
                    .buttonStyle(.press)
                }
            }
            .padding(.top, 14)
            StarterIdeas().padding(.top, 24)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 36)
    }

    private func widgets(_ list: [Silo_V1_DashboardWidget]) -> some View {
        LazyVGrid(columns: columns, alignment: .leading, spacing: 10) {
            ForEach(list.sorted { $0.position < $1.position }, id: \.id) { w in
                WidgetCard(
                    widget: w,
                    fresh: changed.contains(w.id),
                    focused: focus?.id == w.id,
                    onRefresh: { await refresh(w) },
                    onState: { await setState(w, $0) },
                    onAction: { await run(w, $0, $1) },
                    onOpenApp: { await openApp(w) },
                    onAsk: { focus = w },
                    onRemove: { removing = w }
                )
                .rise(when: arrived.contains(w.id))
            }
        }
    }

    // MARK: actions

    private func load(first: Bool = false) async {
        guard let botID = model.selectedBotID else { return }
        do {
            let fresh = try await model.client.listDashboard(botID: botID)
            guard botID == model.selectedBotID else { return }
            if let board {
                let known = Set(board.widgets.map(\.id) + board.cards.map(\.id))
                arrived = Set(fresh.widgets.map(\.id) + fresh.cards.map(\.id)).subtracting(known)
            }
            changed.formUnion(fresh.widgets.filter(\.fresh).map(\.id))
            board = fresh
            if first {
                since = fresh.posts
                if !fresh.posts.isEmpty || fresh.widgets.contains(where: \.fresh) || fresh.cards.contains(where: { !$0.read }) {
                    try? await model.client.markDashboardRead(botID: botID)
                    await model.refreshBots()
                }
            }
        } catch { await model.report(error) }
    }

    private func replace(_ w: Silo_V1_DashboardWidget) {
        guard let i = board?.widgets.firstIndex(where: { $0.id == w.id }) else { return }
        board?.widgets[i] = w
    }

    private func refresh(_ w: Silo_V1_DashboardWidget) async {
        guard let botID = model.selectedBotID else { return }
        do { replace(try await model.client.refreshWidget(botID: botID, id: w.id)) } catch { await model.report(error) }
    }

    private func setState(_ w: Silo_V1_DashboardWidget, _ state: String) async {
        guard let botID = model.selectedBotID else { return }
        var optimistic = w
        optimistic.stateJson = state
        replace(optimistic)
        do { replace(try await model.client.setWidgetState(botID: botID, id: w.id, stateJSON: state)) } catch {
            replace(w)
            await model.report(error)
        }
    }

    private func run(_ w: Silo_V1_DashboardWidget, _ action: WidgetAction, _ args: [String: Any]) async {
        guard let botID = model.selectedBotID else { return }
        do {
            let out = try await model.client.runWidgetAction(botID: botID, id: w.id, action: action.id, argsJSON: WidgetJSON.encode(args))
            if out.hasWidget { replace(out.widget) }
            if out.hasChat, !out.chat.id.isEmpty { model.openChat(out.chat) }
        } catch { await model.report(error) }
    }

    private func remove(_ w: Silo_V1_DashboardWidget) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.removeWidget(botID: botID, id: w.id)
            withAnimation(Motion.state) { board?.widgets.removeAll { $0.id == w.id } }
        } catch { await model.report(error) }
    }

    private func openApp(_ w: Silo_V1_DashboardWidget) async {
        guard let botID = model.selectedBotID else { return }
        do {
            let link = try await model.client.openArtifactApp(botID: botID, artifactID: w.artifactID)
            if let url = URL(string: link) { app = IdentifiedURL(url: url) }
        } catch { await model.report(error) }
    }

    private func ask(_ text: String) async -> Bool {
        guard let botID = model.selectedBotID else { return false }
        do {
            _ = try await model.client.askDashboard(botID: botID, text: text, widgetID: focus?.id ?? "")
            focus = nil
            await load()
            return true
        } catch {
            await model.report(error)
            return false
        }
    }
}

extension Silo_V1_DashboardCard: @retroactive Identifiable {}

struct IdentifiedURL: Identifiable {
    let url: URL
    var id: String { url.absoluteString }
}

/// In-app Safari for a live app artifact or a tunnel.
struct SafariView: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController { SFSafariViewController(url: url) }
    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}

extension View {
    /// A widget's surface: a flat card on its ring.
    func widgetCard() -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        return padding(.horizontal, 14)
            .padding(.top, 10)
            .padding(.bottom, 12)
            .background(Theme.surface, in: shape)
            .overlay(shape.stroke(Theme.line))
    }
}

// MARK: - Since you last looked

/// The Feed's new posts, for the visit that read them. A row opens in place.
private struct SinceLooked: View {
    @Environment(AppModel.self) private var model
    let posts: [Silo_V1_FeedPost]
    @State private var open = ""

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Text("Since you last looked").font(.silo(13, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                Text(posts.count == 1 ? "1 post" : "\(posts.count) posts").font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                Spacer(minLength: 8)
                Button("All in Feed") { model.path.append(.page(.feed)) }
                    .font(.silo(12.5, .medium))
                    .foregroundStyle(Theme.cobalt)
                    .buttonStyle(.press)
                    .frame(minHeight: 34)
            }
            .padding(.horizontal, 10)
            ForEach(posts, id: \.id) { post in
                row(post)
            }
        }
        .padding(6)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(Theme.line))
        .rise()
        .onAppear { if posts.count == 1 { open = posts[0].id } }
    }

    private func row(_ post: Silo_V1_FeedPost) -> some View {
        let on = open == post.id
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        return VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(Motion.fold) { open = on ? "" : post.id }
            } label: {
                HStack(alignment: .top, spacing: 10) {
                    Circle().fill(Theme.cobalt).frame(width: 6, height: 6).padding(.top, 6).accessibilityLabel("New")
                    VStack(alignment: .leading, spacing: 1) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(post.title.isEmpty ? "Post" : post.title).font(.silo(13, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                            Spacer(minLength: 4)
                            Text(When.stamp(post.createdAt)).font(.siloMono(11.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                        }
                        if !on {
                            Text(preview(post)).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    FoldChevron(open: on).padding(.top, 4)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            if on {
                VStack(alignment: .leading, spacing: 6) {
                    if !post.sourceName.isEmpty {
                        Text(post.sourceName).font(.siloMono(11.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                    }
                    ProseView(text: post.body)
                }
                .padding(.leading, 26)
                .padding(.trailing, 10)
                .padding(.bottom, 10)
                .transition(.opacity)
            }
        }
        .background(on ? Theme.well : .clear, in: shape)
    }

    private func preview(_ post: Silo_V1_FeedPost) -> String {
        String(post.body.filter { !"#*_`>".contains($0) }.prefix(200)).replacingOccurrences(of: "\n", with: " ")
    }
}

// MARK: - Ask bar

/// The owner's words to the Dashboard automation, optionally about one widget: the composer's card
/// and Send, pinned to the foot over a canvas fade.
private struct AskBar: View {
    @Binding var focus: Silo_V1_DashboardWidget?
    @Binding var text: String
    let prompt: String
    let send: (String) async -> Bool
    @State private var sending = false
    @FocusState private var typing: Bool

    private var ready: Bool { !sending && !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.composer, style: .continuous)
        VStack(alignment: .leading, spacing: 6) {
            if let focus { picked(focus) }
            HStack(alignment: .bottom, spacing: 8) {
                TextField("", text: $text, prompt: Text(prompt).foregroundStyle(Theme.ink3), axis: .vertical)
                    .lineLimit(1...4)
                    .font(.silo(16))
                    .foregroundStyle(Theme.ink)
                    .tint(Theme.cobalt)
                    .focused($typing)
                    .submitLabel(.send)
                    .onSubmit { submit() }
                    .padding(.vertical, 6)
                Button { submit() } label: {
                    ZStack {
                        RoundedRectangle(cornerRadius: 11, style: .continuous).fill(ready ? Theme.ink : Theme.pressed)
                        if sending {
                            Spinner(size: 14, color: Theme.ink2)
                        } else {
                            Image(systemName: "arrow.up").font(.system(size: 15, weight: .semibold)).foregroundStyle(ready ? Theme.onInk : Theme.ink3)
                        }
                    }
                    .frame(width: 34, height: 34)
                    .animation(Motion.state, value: ready)
                }
                .buttonStyle(PressStyle(scale: 0.94))
                .disabled(!ready)
                .accessibilityLabel("Send")
            }
        }
        .padding(.leading, 14)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(typing ? Theme.lineStrong : Theme.line))
        .floatShadow()
        .padding(.horizontal, 12)
        .padding(.top, 20)
        .padding(.bottom, 8)
        .background {
            LinearGradient(stops: [.init(color: Theme.canvas.opacity(0), location: 0), .init(color: Theme.canvas, location: 0.3)],
                           startPoint: .top, endPoint: .bottom)
                .ignoresSafeArea(edges: .bottom)
                .allowsHitTesting(false)
        }
        .sensoryFeedback(.impact(weight: .light), trigger: sending) { _, on in on }
        .onChange(of: focus?.id) { _, id in if id != nil { typing = true } }
    }

    private func picked(_ widget: Silo_V1_DashboardWidget) -> some View {
        HStack(spacing: 2) {
            Text(widget.title).font(.silo(12.5, .medium)).lineLimit(1)
            Button { focus = nil } label: { Image(systemName: "xmark").font(.system(size: 10, weight: .semibold)) }
                .buttonStyle(.glyph(tint: Theme.cobalt, side: 24))
                .accessibilityLabel("Stop asking about \(widget.title)")
        }
        .foregroundStyle(Theme.cobalt)
        .padding(.leading, 8)
        .padding(.trailing, 2)
        .frame(height: 26)
        .background(Theme.cobaltPale, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
    }

    private func submit() {
        let clean = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !clean.isEmpty, !sending else { return }
        sending = true
        Task {
            if await send(clean) {
                text = ""
                typing = false
            }
            sending = false
        }
    }
}

// MARK: - Widget

private struct WidgetCard: View {
    let widget: Silo_V1_DashboardWidget
    let fresh: Bool
    let focused: Bool
    let onRefresh: () async -> Void
    let onState: (String) async -> Void
    let onAction: (WidgetAction, [String: Any]) async -> Void
    let onOpenApp: () async -> Void
    let onAsk: () -> Void
    let onRemove: () -> Void
    @State private var refreshing = false
    @State private var running = ""
    @State private var confirming: (WidgetAction, [String: Any])?
    @State private var failureOpen = false
    @State private var acted = 0

    private var data: [String: Any] { WidgetJSON.object(widget.dataJson) }
    private var acts: [WidgetAction] { widgetActions(widget.specJson) }
    private var busy: Bool { widget.busy || refreshing }

    var body: some View {
        Group {
            if widget.kind == "app" {
                VStack(alignment: .leading, spacing: 8) {
                    hatch
                    if hasFooter { footer.padding(.horizontal, 4) }
                }
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    header
                    content
                    if hasFooter { footer.padding(.top, 4) }
                }
                .frame(maxWidth: .infinity, alignment: .topLeading)
                .widgetCard()
            }
        }
        .background {
            if focused {
                RoundedRectangle(cornerRadius: Theme.Radius.card + 3, style: .continuous).fill(Theme.cobaltPale).padding(-3)
            }
        }
        .overlay {
            if focused {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.cobalt, lineWidth: 1)
            }
        }
        .animation(Motion.state, value: focused)
        .sensoryFeedback(.success, trigger: acted)
        .confirmSlip(item: $confirming, title: { $0.0.label }, action: { $0.0.label }) { pending in
            perform(pending.0, pending.1)
        }
    }

    private var header: some View {
        HStack(spacing: 6) {
            if fresh { Circle().fill(Theme.cobalt).frame(width: 6, height: 6).accessibilityLabel("Changed") }
            Text(widget.title).font(.silo(13, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
            Spacer(minLength: 6)
            if busy {
                Text("Refreshing").font(.silo(11.5)).foregroundStyle(Theme.ink3).shimmering()
            } else if widget.kind == "checklist" {
                checklistCount
            } else {
                Text(metaWords).font(.siloMono(11.5)).foregroundStyle(Theme.ink3).lineLimit(1)
            }
            tools(tint: Theme.ink3)
        }
        .frame(height: 24)
    }

    private var metaWords: String {
        [widget.source, widgetAge(widget.dataAt)].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// Refresh, and the rest of what the widget offers in one menu.
    private func tools(tint: Color) -> some View {
        HStack(spacing: 0) {
            if widget.hasProbe_p {
                Button { refresh() } label: {
                    Image(systemName: "arrow.clockwise").font(.system(size: 12, weight: .medium))
                }
                .buttonStyle(.glyph(tint: tint, side: 32))
                .disabled(busy)
                .accessibilityLabel("Refresh")
            }
            SiloMenu {
                if widget.hasProbe_p { MenuRow(title: "Refresh", symbol: "arrow.clockwise", enabled: !busy) { refresh() } }
                MenuRow(title: "Ask about this", symbol: "bubble.left") { onAsk() }
                MenuRow(title: "Remove", symbol: "trash", destructive: true) { onRemove() }
            } label: {
                Image(systemName: "ellipsis")
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(tint)
                    .frame(width: 32, height: 32)
            }
            .accessibilityLabel("More for \(widget.title)")
        }
        .padding(.trailing, -8)
    }

    private func refresh() {
        refreshing = true
        Task {
            await onRefresh()
            refreshing = false
        }
    }

    /// An app is a live artifact in the hatch, the dark window that says "this is the Bot's machine".
    private var hatch: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Text(widget.title).font(.siloMono(11.5)).foregroundStyle(Theme.onHatch.opacity(0.75)).lineLimit(1)
                Spacer(minLength: 6)
                tools(tint: Theme.onHatch.opacity(0.75))
            }
            .frame(height: 34)
            .padding(.horizontal, 6)
            Button { Task { await onOpenApp() } } label: {
                VStack(spacing: 8) {
                    Image(systemName: "macwindow").font(.system(size: 18)).foregroundStyle(Theme.onHatch.opacity(0.75))
                    Text("Open app").font(.silo(13, .medium)).foregroundStyle(Theme.onHatch)
                }
                .frame(maxWidth: .infinity)
                .frame(height: 132)
                .background(Theme.matte, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
            }
            .buttonStyle(.press)
            .disabled(widget.artifactID.isEmpty)
        }
        .padding([.horizontal, .bottom], 8)
        .background(Theme.hatch, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }

    @ViewBuilder
    private var checklistCount: some View {
        let all = WidgetJSON.items(data["items"])
        let on = Set(widgetChecked(widget.stateJson))
        let done = all.enumerated().filter { on.contains(itemID($0.element, $0.offset)) }.count
        if !all.isEmpty { Text("\(done)/\(all.count)").font(.siloMono(11.5)).foregroundStyle(Theme.ink3) }
    }

    @ViewBuilder
    private var content: some View {
        if widget.dataAt.isEmpty && data.isEmpty {
            quiet(widget.hasProbe_p ? "Waiting for the first refresh." : "Nothing here yet.")
        } else {
            switch widget.kind {
            case "note": ProseView(text: WidgetJSON.text(data["text"]))
            case "stat": stat
            case "chart": chart
            case "list": list
            case "table": table
            case "timeline": timeline
            case "status": status
            case "checklist": checklist
            case "progress": progress
            case "ideas": IdeaList(ideas: ideasOf(data))
            default: EmptyView()
            }
        }
    }

    private func quiet(_ text: String) -> some View {
        Text(text).font(.silo(12.5)).foregroundStyle(Theme.ink3)
    }

    private var hairline: some View { Theme.line.frame(height: 1) }

    // MARK: kinds

    private var stat: some View {
        let series = ((data["series"] as? [Any]) ?? []).compactMap { WidgetJSON.number($0) }
        return VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(WidgetJSON.display(data["value"]))
                    .font(.silo(34, .medium))
                    .tracking(-0.85)
                    .foregroundStyle(Theme.ink)
                    .monospacedDigit()
                    .minimumScaleFactor(0.5)
                    .lineLimit(1)
                unit
            }
            caption
            if series.count > 1 { Sparkline(values: series, height: 36).padding(.top, 8) }
        }
    }

    @ViewBuilder
    private var unit: some View {
        let u = WidgetJSON.text(data["unit"])
        if !u.isEmpty { Text(u).font(.siloMono(13)).foregroundStyle(Theme.ink3) }
    }

    @ViewBuilder
    private var caption: some View {
        let c = WidgetJSON.text(data["caption"])
        if !c.isEmpty { Text(c).font(.silo(12.5)).foregroundStyle(Theme.ink2) }
    }

    private struct Point: Identifiable {
        let id: Int
        let label: String
        let value: Double
    }

    private var chart: some View {
        let points = WidgetJSON.items(data["points"]).enumerated().map {
            Point(id: $0.offset, label: WidgetJSON.text($0.element["label"]), value: WidgetJSON.number($0.element["value"]) ?? 0)
        }
        let bar = WidgetJSON.text(WidgetJSON.object(widget.specJson)["type"]) == "bar"
        return VStack(alignment: .leading, spacing: 4) {
            if let last = points.last {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(WidgetJSON.format(last.value)).font(.silo(20, .medium)).tracking(-0.3).foregroundStyle(Theme.ink).monospacedDigit()
                    unit
                    Spacer(minLength: 6)
                    Text(last.label).font(.siloMono(11.5)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
            }
            caption
            if bar {
                bars(points).padding(.top, 8)
            } else if points.count > 1 {
                Sparkline(values: points.map(\.value), height: 88).padding(.top, 8)
            }
            if points.count > 1 {
                HStack {
                    Text(points.first?.label ?? "")
                    Spacer(minLength: 6)
                    Text(points.last?.label ?? "")
                }
                .font(.siloMono(11))
                .foregroundStyle(Theme.ink3)
                .padding(.top, 2)
            }
        }
    }

    /// `ink3` bars with the last in `ink`.
    private func bars(_ points: [Point]) -> some View {
        let height: CGFloat = 84
        let top = max(0, points.map(\.value).max() ?? 0)
        let shape = UnevenRoundedRectangle(topLeadingRadius: 3, topTrailingRadius: 3, style: .continuous)
        return HStack(alignment: .bottom, spacing: 4) {
            ForEach(points) { point in
                shape
                    .fill(point.id == points.count - 1 ? Theme.ink : Theme.ink3.opacity(0.45))
                    .frame(maxWidth: .infinity)
                    .frame(height: max(2, top > 0 ? CGFloat(max(0, point.value) / top) * height : 0))
            }
        }
        .frame(height: height, alignment: .bottom)
        .accessibilityLabel("Chart")
    }

    private var list: some View {
        let rows = WidgetJSON.items(data["items"])
        let rowActs = acts.filter(\.row)
        return VStack(alignment: .leading, spacing: 0) {
            if rows.isEmpty { quiet("Nothing here.") }
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                HStack(spacing: 8) {
                    VStack(alignment: .leading, spacing: 1) {
                        if let url = widgetURL(row["url"]) {
                            Link(WidgetJSON.text(row["title"]), destination: url)
                                .font(.silo(13))
                                .tint(Theme.cobalt)
                                .foregroundStyle(Theme.cobalt)
                                .lineLimit(1)
                        } else {
                            Text(WidgetJSON.text(row["title"])).font(.silo(13)).foregroundStyle(Theme.ink).lineLimit(1)
                        }
                        let detail = WidgetJSON.text(row["detail"])
                        if !detail.isEmpty { Text(detail).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1) }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    let meta = WidgetJSON.text(row["meta"])
                    if !meta.isEmpty { Text(meta).font(.siloMono(11.5)).foregroundStyle(Theme.ink3) }
                    rowMenu(rowActs, row)
                }
                .padding(.vertical, 6)
                if index < rows.count - 1 { hairline }
            }
        }
    }

    private var table: some View {
        let columns = ((data["columns"] as? [Any]) ?? []).map { WidgetJSON.text($0) }
        let rows = ((data["rows"] as? [Any]) ?? []).compactMap { $0 as? [Any] }
        let rowActs = acts.filter(\.row)
        return Group {
            if rows.isEmpty {
                quiet("Nothing here.")
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 7) {
                        if !columns.isEmpty {
                            GridRow {
                                ForEach(Array(columns.enumerated()), id: \.offset) { i, c in
                                    Text(c.uppercased())
                                        .font(.silo(11, .medium))
                                        .tracking(0.88)
                                        .foregroundStyle(Theme.ink3)
                                        .gridColumnAlignment(widgetNumeric(rows.first?[safe: i]) ? .trailing : .leading)
                                }
                                if !rowActs.isEmpty { Color.clear.frame(width: 1, height: 1) }
                            }
                        }
                        ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                            if index > 0 || !columns.isEmpty { hairline.gridCellUnsizedAxes(.horizontal) }
                            GridRow {
                                ForEach(Array(row.enumerated()), id: \.offset) { _, v in
                                    Text(WidgetJSON.text(v))
                                        .font(widgetNumeric(v) ? .siloMono(12) : .silo(12.5))
                                        .foregroundStyle(widgetNumeric(v) ? Theme.ink2 : Theme.ink)
                                        .monospacedDigit()
                                        .lineLimit(1)
                                }
                                if !rowActs.isEmpty {
                                    rowMenu(rowActs, Dictionary(uniqueKeysWithValues: row.enumerated().map { (columns[safe: $0.offset] ?? String($0.offset), $0.element) }))
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    private var timeline: some View {
        let rows = WidgetJSON.items(data["items"])
        let split = widgetNowSplit(rows.map { WidgetJSON.text($0["time"]) })
        return VStack(alignment: .leading, spacing: 0) {
            if rows.isEmpty { quiet("Nothing here.") }
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                if index == split { nowRule }
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Text(clock(WidgetJSON.text(row["time"]))).font(.siloMono(12)).foregroundStyle(Theme.ink3).frame(width: 56, alignment: .leading)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(WidgetJSON.text(row["title"]))
                            .font(.silo(13, .medium))
                            .foregroundStyle(split >= 0 && index < split ? Theme.ink3 : Theme.ink)
                        let detail = WidgetJSON.text(row["detail"])
                        if !detail.isEmpty { Text(detail).font(.silo(12)).foregroundStyle(Theme.ink3) }
                    }
                }
                .padding(.vertical, 5)
            }
            if split == rows.count, !rows.isEmpty { nowRule }
        }
    }

    /// The board's one "you are here".
    private var nowRule: some View {
        HStack(spacing: 6) {
            Text(When.clock(Date())).font(.siloMono(12, .medium)).frame(width: 56, alignment: .leading)
            Circle().frame(width: 7, height: 7)
            Rectangle().frame(height: 1)
        }
        .foregroundStyle(Theme.cobalt)
        .accessibilityLabel("Now")
    }

    private func clock(_ s: String) -> String {
        parseDate(s).map { When.clock($0) } ?? s
    }

    private var status: some View {
        let rows = WidgetJSON.items(data["items"])
        return VStack(spacing: 0) {
            if rows.isEmpty { quiet("Nothing here.") }
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                let tone = widgetTone(row["state"])
                HStack(spacing: 10) {
                    statusDot(tone)
                    Text(WidgetJSON.text(row["label"])).font(.siloMono(12.5)).foregroundStyle(Theme.ink).lineLimit(1)
                    Spacer(minLength: 6)
                    let word = WidgetJSON.text(row["word"])
                    Text(word.isEmpty ? toneWord(tone) : word).font(.silo(12, .medium)).foregroundStyle(toneColor(tone))
                    let meta = WidgetJSON.text(row["meta"])
                    if !meta.isEmpty { Text(meta).font(.siloMono(11.5)).foregroundStyle(Theme.ink3) }
                }
                .frame(minHeight: 30)
                if index < rows.count - 1 { hairline }
            }
        }
    }

    /// Green is alive, vermilion needs you, and nothing else gets a colour.
    @ViewBuilder
    private func statusDot(_ tone: WidgetTone) -> some View {
        switch tone {
        case .ok: Circle().fill(Theme.lamp).frame(width: 7, height: 7)
        case .down: Circle().fill(Theme.vermilion).frame(width: 7, height: 7)
        case .warn: Circle().fill(Theme.ink2).frame(width: 7, height: 7)
        case .off: Circle().strokeBorder(Theme.ink3, lineWidth: 1.5).frame(width: 7, height: 7)
        }
    }

    private func toneColor(_ tone: WidgetTone) -> Color {
        switch tone {
        case .ok: return Theme.emerald
        case .down: return Theme.vermilion
        case .warn: return Theme.ink2
        case .off: return Theme.ink3
        }
    }

    private func toneWord(_ tone: WidgetTone) -> String {
        switch tone {
        case .ok: return "Up"
        case .warn: return "Degraded"
        case .down: return "Down"
        case .off: return "Off"
        }
    }

    private func itemID(_ row: [String: Any], _ index: Int) -> String {
        let id = WidgetJSON.text(row["id"])
        return id.isEmpty ? String(index) : id
    }

    private var checklist: some View {
        let rows = WidgetJSON.items(data["items"])
        let on = Set(widgetChecked(widget.stateJson))
        return VStack(spacing: 0) {
            if rows.isEmpty { quiet("Nothing here.") }
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                let id = itemID(row, index)
                let done = on.contains(id)
                Button {
                    Task { await onState(widgetToggle(widget.stateJson, id: id)) }
                } label: {
                    HStack(spacing: 10) {
                        TaskCheck(done: done)
                        Text(WidgetJSON.text(row["text"]))
                            .font(.silo(13))
                            .strikethrough(done, color: Theme.ink3)
                            .foregroundStyle(done ? Theme.ink3 : Theme.ink)
                            .multilineTextAlignment(.leading)
                        Spacer(minLength: 0)
                    }
                    .padding(.vertical, 6)
                    .frame(minHeight: 36)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.press)
                .sensoryFeedback(.selection, trigger: done)
                .accessibilityAddTraits(done ? .isSelected : [])
                if index < rows.count - 1 { hairline }
            }
        }
    }

    private var progress: some View {
        let v = WidgetJSON.number(data["value"]) ?? 0
        let max = WidgetJSON.number(data["max"]) ?? 100
        let fraction = max > 0 ? Swift.max(0, Swift.min(1, v / max)) : 0
        let c = WidgetJSON.text(data["caption"])
        return VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text("\(Int((fraction * 100).rounded()))").font(.silo(34, .medium)).tracking(-0.85).foregroundStyle(Theme.ink).monospacedDigit()
                Text("%").font(.siloMono(13)).foregroundStyle(Theme.ink3)
            }
            Text(c.isEmpty ? "\(WidgetJSON.format(v)) / \(WidgetJSON.format(max))" : c).font(.silo(12.5)).foregroundStyle(Theme.ink2)
            GeometryReader { box in
                ZStack(alignment: .leading) {
                    Capsule().fill(Theme.pressed)
                    Capsule().fill(Theme.ink).frame(width: box.size.width * fraction)
                }
            }
            .frame(height: 3)
            .padding(.top, 8)
            .animation(Motion.arrival, value: fraction)
            .accessibilityValue("\(Int((fraction * 100).rounded())) percent")
        }
    }

    // MARK: actions and footer

    @ViewBuilder
    private func rowMenu(_ rowActs: [WidgetAction], _ row: [String: Any]) -> some View {
        if !rowActs.isEmpty {
            SiloMenu {
                ForEach(rowActs) { action in
                    MenuRow(title: action.label, symbol: action.hand ? "bubble.left" : nil) { trigger(action, row) }
                }
            } label: {
                Image(systemName: "ellipsis")
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(Theme.ink3)
                    .frame(width: 30, height: 30)
            }
            .disabled(!running.isEmpty)
            .accessibilityLabel("Row actions")
        }
    }

    private func trigger(_ action: WidgetAction, _ args: [String: Any]) {
        if action.confirm { confirming = (action, args) } else { perform(action, args) }
    }

    private func perform(_ action: WidgetAction, _ args: [String: Any]) {
        running = action.id
        Task {
            await onAction(action, args)
            running = ""
            acted += 1
        }
    }

    private var hasFooter: Bool {
        acts.contains { !$0.row } || !widget.error.isEmpty || widgetReceipt(widget.receiptJson) != nil
    }

    private var footer: some View {
        VStack(alignment: .leading, spacing: 6) {
            let own = acts.filter { !$0.row }
            if !own.isEmpty {
                FlowRow(spacing: 6) {
                    ForEach(Array(own.enumerated()), id: \.element.id) { index, action in
                        actionButton(action, primary: index == 0 && !action.hand)
                    }
                }
            }
            if !widget.error.isEmpty { failure }
            if let rc = widgetReceipt(widget.receiptJson) { receipt(rc) }
        }
    }

    /// The first run action secondary, hand actions ghost with a speech glyph, `confirm` ones armed.
    @ViewBuilder
    private func actionButton(_ action: WidgetAction, primary: Bool) -> some View {
        if action.confirm {
            ArmedButton(title: action.label, armedTitle: "Tap again to run", small: true) { perform(action, [:]) }
                .disabled(!running.isEmpty)
        } else if primary {
            Button { perform(action, [:]) } label: { actionLabel(action) }
                .buttonStyle(.quiet)
                .controlSize(.small)
                .disabled(!running.isEmpty)
        } else {
            Button { perform(action, [:]) } label: { actionLabel(action) }
                .buttonStyle(.ghost)
                .controlSize(.small)
                .disabled(!running.isEmpty)
        }
    }

    private func actionLabel(_ action: WidgetAction) -> some View {
        HStack(spacing: 5) {
            if running == action.id {
                Spinner(size: 12)
            } else if action.hand {
                Image(systemName: "bubble.left").font(.system(size: 12))
            }
            Text(action.label).lineLimit(1)
        }
    }

    /// "Couldn't refresh", and the error a press further.
    private var failure: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button {
                withAnimation(Motion.fold) { failureOpen.toggle() }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "exclamationmark.circle").font(.system(size: 12))
                    Text("Couldn't refresh").font(.silo(12))
                    FoldChevron(open: failureOpen, size: 9)
                    Spacer(minLength: 0)
                }
                .foregroundStyle(Theme.ink3)
                .frame(minHeight: 28)
                .contentShape(Rectangle())
            }
            .buttonStyle(.press)
            if failureOpen {
                Text(widget.error)
                    .font(.siloMono(11.5))
                    .foregroundStyle(Theme.ink2)
                    .textSelection(.enabled)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous))
                    .transition(.opacity)
            }
        }
    }

    private func receipt(_ rc: WidgetReceipt) -> some View {
        HStack(spacing: 8) {
            Image(systemName: rc.ok ? "checkmark" : "xmark")
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(rc.ok ? Theme.emerald : Theme.ink3)
            Text(rc.text).font(.silo(12, rc.ok ? .medium : .regular)).foregroundStyle(rc.ok ? Theme.ink : Theme.ink3).lineLimit(1)
            Theme.line.frame(minWidth: 16, maxWidth: .infinity).frame(height: 1)
            Text(["by \(rc.by)", When.clock(rc.at)].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.silo(12))
                .foregroundStyle(Theme.ink3)
                .fixedSize()
        }
        .rise()
    }
}

/// A hairline in ink with its last point marked: no fill, no grid.
private struct Sparkline: View {
    let values: [Double]
    let height: CGFloat

    var body: some View {
        Canvas { context, size in
            let inset: CGFloat = 3
            let low = values.min() ?? 0
            let span = (values.max() ?? 0) - low
            let step = (size.width - inset * 2) / CGFloat(max(1, values.count - 1))
            let points = values.enumerated().map { index, value in
                CGPoint(x: inset + CGFloat(index) * step,
                        y: inset + (size.height - inset * 2) * CGFloat(1 - (value - low) / (span == 0 ? 1 : span)))
            }
            var base = Path()
            base.move(to: CGPoint(x: 0, y: size.height - 0.5))
            base.addLine(to: CGPoint(x: size.width, y: size.height - 0.5))
            context.stroke(base, with: .color(Theme.line), lineWidth: 1)
            var line = Path()
            line.addLines(points)
            context.stroke(line, with: .color(Theme.ink), style: StrokeStyle(lineWidth: 1.3, lineJoin: .round))
            if let last = points.last {
                let dot = CGRect(x: last.x - 2.5, y: last.y - 2.5, width: 5, height: 5)
                context.fill(Path(ellipseIn: dot), with: .color(Theme.ink))
            }
        }
        .frame(height: height)
        .accessibilityLabel("Trend")
    }
}

extension Array {
    subscript(safe index: Int) -> Element? { indices.contains(index) ? self[index] : nil }
}

// MARK: - Asks

/// The upper half of the Ask card on the board; a press opens its reply tray.
private struct AskRow: View {
    let card: Silo_V1_DashboardCard

    private var question: String { card.summary.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    Image(systemName: "questionmark.bubble").font(.system(size: 13)).foregroundStyle(Theme.cobalt)
                    Text("Question for you").font(.silo(12, .medium)).foregroundStyle(Theme.cobalt).lineLimit(1).layoutPriority(1)
                    if !card.sourceName.isEmpty {
                        Text("· \(card.sourceName)").font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                    }
                    Spacer(minLength: 6)
                    Text(When.stamp(card.updatedAt.isEmpty ? card.createdAt : card.updatedAt))
                        .font(.siloMono(11.5))
                        .foregroundStyle(Theme.ink3)
                        .lineLimit(1)
                }
                if askTitled(card) {
                    Text(card.title).font(.silo(15, .semibold)).tracking(-0.15).foregroundStyle(Theme.ink).multilineTextAlignment(.leading)
                }
                if !question.isEmpty {
                    Text(question)
                        .font(.reply(16))
                        .lineSpacing(4)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(4)
                        .multilineTextAlignment(.leading)
                }
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 6) {
                Text("Answer").font(.silo(12.5, .medium)).foregroundStyle(Theme.ink2)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.ink3)
            }
            .padding(.horizontal, 14)
            .frame(height: 38)
            .background(Theme.well)
            .overlay(alignment: .top) { Theme.line.frame(height: 1) }
        }
        .background(Theme.surface, in: shape)
        .clipShape(shape)
        .overlay(shape.stroke(Theme.line))
        .contentShape(shape)
    }
}

/// `ask_owner` titles a card with its question's first line when the Bot gave it no title; shown
/// above the question, that would only say it twice.
private func askTitled(_ card: Silo_V1_DashboardCard) -> Bool {
    let title = card.title.trimmingCharacters(in: .whitespacesAndNewlines)
    return !title.isEmpty && !card.summary.trimmingCharacters(in: .whitespacesAndNewlines).hasPrefix(title)
}

/// One ask as a sheet, the Ask card whole: the Bot speaking above, the owner's reply tray below with
/// its controls and one submit that opens a chat with the answers. "Not now" puts it away; "Don't
/// suggest this again" also tells the Bot.
struct AskSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let card: Silo_V1_DashboardCard
    /// Shown in a chat: the answer is the owner's turn there, and there is no "don't suggest".
    var inThread = false
    let changed: () async -> Void
    @State private var blocks: [AskBlock] = []
    @State private var values: [String: AskValue] = [:]
    @State private var note = ""
    @State private var busy = ""

    /// From the first control down is the owner's reply; what comes before it reads as the Bot's.
    private var cut: Int { blocks.firstIndex(where: \.isControl) ?? blocks.count }

    var body: some View {
        VStack(spacing: 0) {
            SheetHead(title: "Question for you", cancel: "Close", onCancel: { dismiss() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    speaking
                    tray
                }
            }
            .scrollDismissesKeyboard(.interactively)
            .scrollBounceBehavior(.basedOnSize)
            .background(Theme.well)
            .disabled(!busy.isEmpty)
        }
        .background(Theme.surface)
        .siloSheet()
        .sensoryFeedback(.impact(weight: .light), trigger: busy) { _, now in !now.isEmpty }
        .onAppear {
            let parsed = parseAskBlocks(card.blocksJson)
            blocks = parsed.blocks
            values = parsed.defaults
        }
    }

    private var speaking: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 6) {
                if !card.sourceName.isEmpty {
                    Text(card.sourceName).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
                }
                Spacer(minLength: 0)
                Text(When.stamp(card.updatedAt.isEmpty ? card.createdAt : card.updatedAt)).font(.siloMono(11.5)).foregroundStyle(Theme.ink3)
            }
            if askTitled(card) {
                Text(card.title).font(.silo(15, .semibold)).tracking(-0.15).foregroundStyle(Theme.ink)
            }
            if !card.summary.isEmpty { ProseView(text: card.summary) }
            ForEach(blocks[..<cut]) { block in
                blockView(block)
            }
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface)
    }

    private var tray: some View {
        VStack(alignment: .leading, spacing: 16) {
            ForEach(blocks[cut...]) { block in
                blockView(block)
            }
            SiloField(prompt: "Add a note (optional)", text: $note, axis: .vertical, lines: 1...5)
            FlowRow(spacing: 6) {
                Button { Task { await submit() } } label: {
                    HStack(spacing: 6) {
                        if busy == "submit" { Spinner(size: 13, color: Theme.onInk) } else { Image(systemName: "paperplane").font(.system(size: 12)) }
                        Text(card.submitLabel.isEmpty ? "Send" : card.submitLabel)
                    }
                }
                .buttonStyle(.ink)
                .controlSize(.small)
                Button("Not now") { Task { await dismissCard(suppress: false) } }
                    .buttonStyle(.ghost)
                    .controlSize(.small)
                if !inThread {
                    Button("Don't suggest this again") { Task { await dismissCard(suppress: true) } }
                        .buttonStyle(.ghost(tint: Theme.ink3))
                        .controlSize(.small)
                }
            }
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.well)
        .overlay(alignment: .top) { Theme.line.frame(height: 1) }
    }

    @ViewBuilder
    private func blockView(_ b: AskBlock) -> some View {
        switch b.type {
        case "markdown":
            if !b.text.isEmpty { ProseView(text: b.text) }
        case "kv":
            pairs(b)
        case "metrics":
            metrics(b)
        case "diff":
            diff(b)
        case "switch":
            switchRow(b)
        case "select":
            field(b) {
                SiloPicker(options: b.options.map { (value: $0.value, label: $0.label) }, selection: stringBinding(b.name))
            }
        case "segmented":
            field(b) {
                AskChoices(options: b.options, picked: [stringBinding(b.name).wrappedValue]) { values[b.name] = .string($0) }
            }
        case "checkboxes":
            field(b) {
                AskChoices(options: b.options, picked: listValue(b.name), multi: true) { value in
                    var list = listValue(b.name)
                    if list.contains(value) { list.removeAll { $0 == value } } else { list.append(value) }
                    values[b.name] = .list(list)
                }
            }
        case "input", "textarea":
            field(b) {
                SiloField(prompt: b.placeholder, text: stringBinding(b.name), axis: b.type == "textarea" ? .vertical : .horizontal,
                          lines: b.type == "textarea" ? 3...8 : 1...1)
            }
        default:
            EmptyView()
        }
    }

    /// A control with its label above and its hint below, always in view.
    private func field(_ b: AskBlock, @ViewBuilder control: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if !b.label.isEmpty { Text(b.label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3) }
            control()
            if !b.hint.isEmpty {
                Text(b.hint).font(.silo(12)).foregroundStyle(Theme.ink3).fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private func pairs(_ b: AskBlock) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(b.pairs.enumerated()), id: \.offset) { index, pair in
                if index > 0 { Theme.lineStrong.frame(height: 1) }
                HStack(alignment: .firstTextBaseline, spacing: 16) {
                    Text(pair.label).font(.silo(12, .medium)).foregroundStyle(Theme.ink3).fixedSize()
                    Spacer(minLength: 0)
                    Text(pair.value).font(.siloMono(12.5)).foregroundStyle(Theme.ink).lineLimit(1)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }
        }
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }

    private func metrics(_ b: AskBlock) -> some View {
        LazyVGrid(columns: [GridItem(.flexible(), spacing: 8), GridItem(.flexible(), spacing: 8)], spacing: 8) {
            ForEach(b.pairs, id: \.self) { pair in
                VStack(alignment: .leading, spacing: 2) {
                    Text(pair.label).font(.silo(11.5, .medium)).foregroundStyle(Theme.ink3).lineLimit(1)
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(pair.value).font(.siloMono(16, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                        if !pair.delta.isEmpty {
                            Text(pair.delta).font(.siloMono(11, .medium)).foregroundStyle(deltaColor(pair.tone))
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

    private func deltaColor(_ tone: String) -> Color {
        switch tone {
        case "good": return Theme.emerald
        case "bad": return Theme.vermilion
        case "warn": return Theme.ink2
        default: return Theme.ink3
        }
    }

    private func diff(_ b: AskBlock) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            if !b.path.isEmpty {
                Text(b.path)
                    .font(.siloMono(11.5, .medium))
                    .foregroundStyle(Theme.ink2)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .overlay(alignment: .bottom) { Theme.line.frame(height: 1) }
            }
            ScrollView([.horizontal, .vertical]) {
                Text(b.text).font(.siloMono(12)).foregroundStyle(Theme.ink).textSelection(.enabled).padding(12)
            }
            .frame(maxHeight: 224)
        }
        .background(Theme.well, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }

    private func switchRow(_ b: AskBlock) -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
        return HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 1) {
                Text(b.label).font(.silo(13, .medium)).foregroundStyle(Theme.ink)
                if !b.hint.isEmpty { Text(b.hint).font(.silo(12)).foregroundStyle(Theme.ink3) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Toggle("", isOn: boolBinding(b.name)).labelsHidden().fixedSize().accessibilityLabel(b.label)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(Theme.line))
    }

    private func boolBinding(_ name: String) -> Binding<Bool> {
        Binding(get: { if case .bool(let b) = values[name] { return b }; return false }, set: { values[name] = .bool($0) })
    }

    private func stringBinding(_ name: String) -> Binding<String> {
        Binding(get: { if case .string(let s) = values[name] { return s }; return "" }, set: { values[name] = .string($0) })
    }

    private func listValue(_ name: String) -> [String] {
        if case .list(let l) = values[name] { return l }
        return []
    }

    private func submit() async {
        guard let botID = model.selectedBotID else { return }
        busy = "submit"
        defer { busy = "" }
        do {
            let out = try await model.client.submitDashboardCard(botID: botID, id: card.id, valuesJSON: encodeAskValues(values), note: note)
            dismiss()
            await changed()
            if out.hasChat, !out.chat.id.isEmpty { model.openChat(out.chat) }
        } catch { await model.report(error) }
    }

    private func dismissCard(suppress: Bool) async {
        guard let botID = model.selectedBotID else { return }
        busy = "dismiss"
        defer { busy = "" }
        do {
            try await model.client.dismissDashboardCard(botID: botID, id: card.id, suppressKind: suppress)
            dismiss()
            await changed()
            await model.refreshBots()
        } catch { await model.report(error) }
    }
}

/// A question's options, one or many: chips that wrap while every label is short and plain, else
/// rows with room for a hint. A picked one is `cobaltPale` on a cobalt ring: it is yours now.
private struct AskChoices: View {
    let options: [AskBlock.Option]
    let picked: [String]
    var multi = false
    let onPick: (String) -> Void

    private var chips: Bool { options.count <= 12 && options.allSatisfy { $0.hint.isEmpty && $0.label.count <= 28 } }

    var body: some View {
        Group {
            if chips {
                FlowRow(spacing: 6) {
                    ForEach(options, id: \.value) { choice($0) }
                }
            } else {
                VStack(spacing: 6) {
                    ForEach(options, id: \.value) { choice($0) }
                }
            }
        }
        .sensoryFeedback(.selection, trigger: picked)
    }

    private func choice(_ option: AskBlock.Option) -> some View {
        let on = picked.contains(option.value)
        let shape = RoundedRectangle(cornerRadius: chips ? Theme.Radius.sm : Theme.Radius.control, style: .continuous)
        return Button {
            withAnimation(Motion.quiet(0.2)) { onPick(option.value) }
        } label: {
            HStack(alignment: chips ? .center : .top, spacing: chips ? 8 : 10) {
                mark(on).padding(.top, chips ? 0 : 1)
                if chips {
                    Text(option.label).lineLimit(1)
                } else {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(option.label).multilineTextAlignment(.leading)
                        if !option.hint.isEmpty {
                            Text(option.hint).font(.silo(12)).foregroundStyle(Theme.ink3).multilineTextAlignment(.leading)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .font(.silo(13, .medium))
            .foregroundStyle(on ? Theme.ink : Theme.ink2)
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

    /// The box (many) or dot (one), filled cobalt once picked; its tick settles in over 200ms.
    private func mark(_ on: Bool) -> some View {
        let shape = RoundedRectangle(cornerRadius: multi ? 4 : 7, style: .continuous)
        return ZStack {
            shape.fill(on ? Theme.cobalt : Theme.surface)
            if on {
                Group {
                    if multi {
                        Image(systemName: "checkmark").font(.system(size: 8, weight: .heavy))
                    } else {
                        Circle().frame(width: 6, height: 6)
                    }
                }
                .foregroundStyle(Theme.onAccent)
                .transition(.scale(scale: 0.6).combined(with: .opacity))
            } else {
                shape.strokeBorder(Theme.lineStrong, lineWidth: 1)
            }
        }
        .frame(width: 14, height: 14)
    }
}

// MARK: - Ideas

struct Idea: Hashable {
    let title: String
    let prompt: String
}

/// An ideas widget's data: `{items: [{title, prompt?}]}`.
func ideasOf(_ data: [String: Any]) -> [Idea] {
    WidgetJSON.items(data["items"]).compactMap { item in
        let title = WidgetJSON.text(item["title"])
        guard !title.isEmpty else { return nil }
        let prompt = WidgetJSON.text(item["prompt"])
        return Idea(title: title, prompt: prompt.isEmpty ? title : prompt)
    }
}

/// A column of ideas: a press puts the prompt in the main chat's composer, never sent for the owner.
struct IdeaList: View {
    @Environment(AppModel.self) private var model
    let ideas: [Idea]

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(ideas, id: \.self) { idea in
                Button {
                    model.composerInsert = idea.prompt
                    model.showMainChat()
                } label: {
                    HStack(spacing: 8) {
                        Text(idea.title).font(.silo(13.5)).foregroundStyle(Theme.ink2).multilineTextAlignment(.leading)
                        Spacer(minLength: 4)
                        Image(systemName: "arrow.up.right").font(.system(size: 11, weight: .medium)).foregroundStyle(Theme.ink3)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 8)
                    .frame(minHeight: 40)
                    .contentShape(Rectangle())
                }
                .buttonStyle(RowPressStyle())
                .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            }
        }
    }
}

/// What a Bot can do for its owner before it knows them: a few things to ask, shaped by what is
/// connected. No model writes them; the Bot's own ideas widget replaces them.
private struct StarterIdeas: View {
    @Environment(AppModel.self) private var model
    @State private var connected: Set<String> = []

    private var ideas: [Idea] {
        var out: [String] = []
        if !connected.isDisjoint(with: ["email", "gmail"]) { out.append("Go through my unread mail and tell me what needs an answer") }
        if !connected.isDisjoint(with: ["calendar", "google_calendar"]) { out.append("Plan my week from my calendar") }
        out += [
            "Research a question for me and compare the top three options",
            "Turn my notes into a one-page PDF I can share",
            "Every morning, brief me on what matters today",
            "Watch a web page and tell me when it changes",
        ]
        return out.prefix(5).map { Idea(title: $0, prompt: $0) }
    }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        VStack(alignment: .leading, spacing: 4) {
            SectionLabel(text: "Things to ask \(model.selectedBot?.name ?? "")").padding(.horizontal, 10)
            IdeaList(ideas: ideas)
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(Theme.line))
        .task(id: model.selectedBotID) {
            guard let botID = model.selectedBotID,
                  let rows = try? await model.client.listBotConnectors(botID: botID) else { return }
            connected = Set(rows.flatMap { [$0.connector.builtin, $0.connector.identifier] }.filter { !$0.isEmpty })
        }
    }
}

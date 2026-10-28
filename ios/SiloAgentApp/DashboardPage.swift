import Charts
import SafariServices
import SiloClient
import SwiftUI

/// The owner's board: the asks the Bot needs decided, what it posted since the last look, and the
/// widgets it keeps current. The owner acts in place (checks, buttons); changing the board itself
/// is a request to the Bot through the ask bar, never a new chat.
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

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                // Approvals wait here with the Bot's asks, as on the web: the ones paused in the
                // open chat also sit in its composer.
                ForEach(model.approvals) { approval in
                    ApprovalSlip(approval: approval)
                }
                if let board {
                    asks(board.cards.filter { $0.status == "open" })
                    sinceLooked
                    // Until the Bot writes its own ideas, a few things to ask it.
                    if !board.widgets.contains(where: { $0.kind == "ideas" }) && board.widgets.count < 3 {
                        StarterIdeas()
                    }
                    widgets(board.widgets)
                    curator(board)
                } else {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                }
            }
            .padding(.horizontal)
            .padding(.bottom, 24)
        }
        .background(Theme.canvas.ignoresSafeArea())
        .safeAreaInset(edge: .bottom) {
            AskBar(focus: $focus) { text in await ask(text) }
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
        .confirmationDialog("Remove this widget?", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing) { w in
            Button("Remove \(w.title)", role: .destructive) { Task { await remove(w) } }
        } message: { _ in
            Text("The Bot can put it back if you ask it to.")
        }
    }

    // MARK: sections

    @ViewBuilder
    private func asks(_ cards: [Silo_V1_DashboardCard]) -> some View {
        if !cards.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                // The Bot's questions only wait; approvals above are what a run is paused on.
                SectionTitle(text: cards.count == 1 ? "Question for you" : "\(cards.count) questions", color: Theme.cobalt)
                ForEach(cards.sorted { $0.priority < $1.priority }, id: \.id) { card in
                    Button { asking = card } label: { AskRow(card: card) }
                        .buttonStyle(.plain)
                }
            }
        }
    }

    @ViewBuilder
    private var sinceLooked: some View {
        if !since.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                SectionTitle(text: "Since you last looked")
                ForEach(since, id: \.id) { post in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(post.title.isEmpty ? "Post" : post.title).font(.silo(.headline))
                            Spacer(minLength: 8)
                            Text(relativeTime(post.createdAt)).font(.silo(.caption)).foregroundStyle(.secondary)
                        }
                        ProseView(text: post.body).lineLimit(8)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .widgetCard()
                }
            }
        }
    }

    private func widgets(_ list: [Silo_V1_DashboardWidget]) -> some View {
        let columns = sizeClass == .regular ? [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)] : [GridItem(.flexible())]
        return LazyVGrid(columns: columns, alignment: .leading, spacing: 12) {
            ForEach(list.sorted { $0.position < $1.position }, id: \.id) { w in
                WidgetCard(
                    widget: w,
                    focused: focus?.id == w.id,
                    onRefresh: { await refresh(w) },
                    onState: { await setState(w, $0) },
                    onAction: { await run(w, $0, $1) },
                    onOpenApp: { await openApp(w) }
                )
                .contextMenu {
                    if w.hasProbe_p { Button { Task { await refresh(w) } } label: { Label("Refresh", systemImage: "arrow.clockwise") } }
                    Button { focus = w } label: { Label("Ask about this", systemImage: "bubble.left") }
                    Button(role: .destructive) { removing = w } label: { Label("Remove", systemImage: "trash") }
                }
            }
        }
    }

    /// The pinned Dashboard automation that curates the board: when it last ran and when it runs next.
    @ViewBuilder
    private func curator(_ board: Silo_V1_ListDashboardResponse) -> some View {
        if !board.automationID.isEmpty {
            HStack(spacing: 6) {
                if board.curating {
                    ProgressView().controlSize(.mini)
                    Text("The Bot is updating the board…")
                } else {
                    Image(systemName: "clock.arrow.circlepath")
                    Text(curatorLine(board))
                }
            }
            .font(.silo(.footnote))
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity)
            .padding(.top, 4)
        }
    }

    private func curatorLine(_ board: Silo_V1_ListDashboardResponse) -> String {
        var parts: [String] = []
        if !board.curatedAt.isEmpty { parts.append("Curated \(relativeTime(board.curatedAt))") }
        if !board.nextAt.isEmpty { parts.append("next \(relativeTime(board.nextAt))") }
        if parts.isEmpty { return board.schedule.isEmpty ? "Curated when you ask" : "Not curated yet" }
        return parts.joined(separator: ", ")
    }

    // MARK: actions

    private func load(first: Bool = false) async {
        guard let botID = model.selectedBotID else { return }
        do {
            let fresh = try await model.client.listDashboard(botID: botID)
            guard botID == model.selectedBotID else { return }
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
            board?.widgets.removeAll { $0.id == w.id }
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

private struct SectionTitle: View {
    let text: String
    var color: Color = .secondary

    var body: some View {
        Text(text)
            .font(.silo(.footnote, .semibold))
            .textCase(.uppercase)
            .foregroundStyle(color)
            .padding(.leading, 4)
    }
}

extension View {
    /// A widget's surface: the grouped-list card, so the board reads like the rest of the app.
    func widgetCard() -> some View {
        padding(14)
            .background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
    }
}

// MARK: - Ask bar

/// The owner's words to the Dashboard automation, optionally about one widget.
private struct AskBar: View {
    @Binding var focus: Silo_V1_DashboardWidget?
    let send: (String) async -> Bool
    @State private var text = ""
    @State private var sending = false
    @FocusState private var typing: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let focus {
                HStack(spacing: 6) {
                    Image(systemName: "scope")
                    Text(focus.title).lineLimit(1)
                    Button { self.focus = nil } label: { Image(systemName: "xmark.circle.fill") }
                        .accessibilityLabel("Stop asking about \(focus.title)")
                }
                .font(.silo(.caption, .medium))
                .foregroundStyle(Theme.cobalt)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(Theme.cobaltPale, in: Capsule())
            }
            HStack(spacing: 8) {
                TextField(focus == nil ? "Ask the Bot to change the board" : "Ask about this widget", text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .focused($typing)
                    .submitLabel(.send)
                    .onSubmit { submit() }
                Button { submit() } label: {
                    if sending { ProgressView() } else { Image(systemName: "arrow.up.circle.fill").font(.system(size: 28)).foregroundStyle(Theme.ink) }
                }
                .disabled(sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .accessibilityLabel("Send")
            }
            .padding(.leading, 16)
            .padding(.trailing, 8)
            .padding(.vertical, 8)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 22, style: .continuous).stroke(Theme.lineStrong))
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        .onChange(of: focus?.id) { _, id in if id != nil { typing = true } }
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
    let focused: Bool
    let onRefresh: () async -> Void
    let onState: (String) async -> Void
    let onAction: (WidgetAction, [String: Any]) async -> Void
    let onOpenApp: () async -> Void
    @State private var refreshing = false
    @State private var running = ""
    @State private var confirming: (WidgetAction, [String: Any])?

    private var data: [String: Any] { WidgetJSON.object(widget.dataJson) }
    private var acts: [WidgetAction] { widgetActions(widget.specJson) }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            header
            if widget.kind == "app" {
                Button { Task { await onOpenApp() } } label: {
                    Label("Open app", systemImage: "macwindow").frame(maxWidth: .infinity)
                }
                .buttonStyle(.quiet)
                .disabled(widget.artifactID.isEmpty)
            } else {
                content
            }
            footer
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .widgetCard()
        .overlay {
            if focused {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).strokeBorder(Theme.cobalt, lineWidth: 1.5)
            }
        }
        .confirmationDialog(confirming?.0.label ?? "", isPresented: Binding(get: { confirming != nil }, set: { if !$0 { confirming = nil } }),
                            titleVisibility: .visible) {
            if let (action, args) = confirming {
                Button(action.label) { perform(action, args) }
            }
        }
    }

    private var header: some View {
        HStack(spacing: 6) {
            if widget.fresh { Circle().fill(Theme.cobalt).frame(width: 6, height: 6).accessibilityLabel("Changed") }
            Text(widget.title).font(.silo(.subheadline, .semibold)).lineLimit(1)
            Spacer(minLength: 6)
            if widget.busy || refreshing {
                ProgressView().controlSize(.mini)
            } else if widget.kind == "checklist" {
                checklistCount
            } else {
                Text([widget.source, widgetAge(widget.dataAt)].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.siloMono(.caption))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            if widget.hasProbe_p {
                Button {
                    refreshing = true
                    Task { await onRefresh(); refreshing = false }
                } label: { Image(systemName: "arrow.clockwise").font(.silo(.caption)) }
                    .buttonStyle(.borderless)
                    .disabled(widget.busy || refreshing)
                    .accessibilityLabel("Refresh")
            }
        }
    }

    @ViewBuilder
    private var checklistCount: some View {
        let all = WidgetJSON.items(data["items"])
        let on = Set(widgetChecked(widget.stateJson))
        let done = all.enumerated().filter { on.contains(itemID($0.element, $0.offset)) }.count
        if !all.isEmpty { Text("\(done)/\(all.count)").font(.siloMono(.caption)).foregroundStyle(.secondary) }
    }

    @ViewBuilder
    private var content: some View {
        if widget.dataAt.isEmpty && data.isEmpty {
            Text(widget.hasProbe_p ? "Waiting for the first refresh." : "Nothing here yet.").font(.silo(.footnote)).foregroundStyle(.secondary)
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

    // MARK: kinds

    private var stat: some View {
        let series = ((data["series"] as? [Any]) ?? []).compactMap { WidgetJSON.number($0) }
        return VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 5) {
                Text(WidgetJSON.display(data["value"]))
                    .font(.silo(.largeTitle, .medium))
                    .monospacedDigit()
                    .minimumScaleFactor(0.5)
                    .lineLimit(1)
                unit
            }
            caption
            if series.count > 1 { Sparkline(values: series).frame(height: 36).padding(.top, 6) }
        }
    }

    @ViewBuilder
    private var unit: some View {
        let u = WidgetJSON.text(data["unit"])
        if !u.isEmpty { Text(u).font(.siloMono(.footnote)).foregroundStyle(.secondary) }
    }

    @ViewBuilder
    private var caption: some View {
        let c = WidgetJSON.text(data["caption"])
        if !c.isEmpty { Text(c).font(.silo(.footnote)).foregroundStyle(.secondary) }
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
        return VStack(alignment: .leading, spacing: 6) {
            if let last = points.last {
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Text(WidgetJSON.format(last.value)).font(.silo(.title2, .medium)).monospacedDigit()
                    unit
                    Spacer()
                    Text(last.label).font(.siloMono(.caption)).foregroundStyle(.secondary)
                }
            }
            caption
            Chart(points) { p in
                if bar {
                    BarMark(x: .value("Label", "\(p.id)"), y: .value("Value", p.value))
                        .foregroundStyle(p.id == points.count - 1 ? Color.primary : Color.secondary.opacity(0.45))
                        .cornerRadius(3)
                } else {
                    LineMark(x: .value("Index", p.id), y: .value("Value", p.value))
                        .foregroundStyle(Color.primary)
                        .interpolationMethod(.monotone)
                    if p.id == points.count - 1 {
                        PointMark(x: .value("Index", p.id), y: .value("Value", p.value)).foregroundStyle(Color.primary).symbolSize(24)
                    }
                }
            }
            .chartXAxis(.hidden)
            .chartYAxis(.hidden)
            .frame(height: 96)
            if points.count > 1 {
                HStack {
                    Text(points.first?.label ?? "")
                    Spacer()
                    Text(points.last?.label ?? "")
                }
                .font(.siloMono(.caption2))
                .foregroundStyle(.secondary)
            }
        }
    }

    private var list: some View {
        let rows = WidgetJSON.items(data["items"])
        let rowActs = acts.filter(\.row)
        return VStack(alignment: .leading, spacing: 0) {
            if rows.isEmpty { Text("Nothing here.").font(.silo(.footnote)).foregroundStyle(.secondary) }
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                HStack(spacing: 8) {
                    VStack(alignment: .leading, spacing: 2) {
                        if let url = widgetURL(row["url"]) {
                            Link(WidgetJSON.text(row["title"]), destination: url).font(.silo(.subheadline)).lineLimit(1)
                        } else {
                            Text(WidgetJSON.text(row["title"])).font(.silo(.subheadline)).lineLimit(1)
                        }
                        let detail = WidgetJSON.text(row["detail"])
                        if !detail.isEmpty { Text(detail).font(.silo(.caption)).foregroundStyle(.secondary).lineLimit(1) }
                    }
                    Spacer(minLength: 4)
                    let meta = WidgetJSON.text(row["meta"])
                    if !meta.isEmpty { Text(meta).font(.siloMono(.caption)).foregroundStyle(.secondary) }
                    rowMenu(rowActs, row)
                }
                .padding(.vertical, 6)
                if index < rows.count - 1 { Divider() }
            }
        }
    }

    private var table: some View {
        let columns = ((data["columns"] as? [Any]) ?? []).map { WidgetJSON.text($0) }
        let rows = ((data["rows"] as? [Any]) ?? []).compactMap { $0 as? [Any] }
        let rowActs = acts.filter(\.row)
        return ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 8) {
                if !columns.isEmpty {
                    GridRow {
                        ForEach(Array(columns.enumerated()), id: \.offset) { i, c in
                            Text(c.uppercased())
                                .font(.silo(.caption2, .semibold))
                                .foregroundStyle(.secondary)
                                .gridColumnAlignment(widgetNumeric(rows.first?[safe: i]) ? .trailing : .leading)
                        }
                        if !rowActs.isEmpty { Color.clear.frame(width: 1, height: 1) }
                    }
                    Divider()
                }
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, v in
                            Text(WidgetJSON.text(v))
                                .font(widgetNumeric(v) ? .siloMono(.caption) : .silo(.subheadline))
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

    private var timeline: some View {
        let rows = WidgetJSON.items(data["items"])
        let split = widgetNowSplit(rows.map { WidgetJSON.text($0["time"]) })
        return VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                if index == split { nowRule }
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Text(clock(WidgetJSON.text(row["time"]))).font(.siloMono(.caption)).foregroundStyle(.secondary).frame(width: 52, alignment: .leading)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(WidgetJSON.text(row["title"])).font(.silo(.subheadline, .medium))
                            .foregroundStyle(split >= 0 && index < split ? .secondary : .primary)
                        let detail = WidgetJSON.text(row["detail"])
                        if !detail.isEmpty { Text(detail).font(.silo(.caption)).foregroundStyle(.secondary) }
                    }
                }
                .padding(.vertical, 5)
            }
            if split == rows.count, !rows.isEmpty { nowRule }
        }
    }

    private var nowRule: some View {
        HStack(spacing: 6) {
            Text(Date().formatted(date: .omitted, time: .shortened)).font(.siloMono(.caption, .medium)).frame(width: 52, alignment: .leading)
            Circle().frame(width: 6, height: 6)
            Rectangle().frame(height: 1)
        }
        .foregroundStyle(Theme.cobalt)
        .accessibilityLabel("Now")
    }

    private func clock(_ s: String) -> String {
        parseDate(s).map { $0.formatted(date: .omitted, time: .shortened) } ?? s
    }

    private var status: some View {
        let rows = WidgetJSON.items(data["items"])
        return VStack(spacing: 0) {
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                let tone = widgetTone(row["state"])
                HStack(spacing: 10) {
                    Circle()
                        .fill(tone == .ok ? Theme.lamp : tone == .down ? Theme.vermilion : tone == .warn ? Color.secondary : Color.clear)
                        .overlay { if tone == .off { Circle().strokeBorder(Color.secondary, lineWidth: 1.5) } }
                        .frame(width: 8, height: 8)
                    Text(WidgetJSON.text(row["label"])).font(.siloMono(.subheadline)).lineLimit(1)
                    Spacer(minLength: 6)
                    let word = WidgetJSON.text(row["word"])
                    Text(word.isEmpty ? toneWord(tone) : word)
                        .font(.silo(.caption, .medium))
                        .foregroundStyle(tone == .ok ? Theme.emerald : tone == .down ? Theme.vermilion : .secondary)
                    let meta = WidgetJSON.text(row["meta"])
                    if !meta.isEmpty { Text(meta).font(.siloMono(.caption)).foregroundStyle(.secondary) }
                }
                .padding(.vertical, 6)
                if index < rows.count - 1 { Divider() }
            }
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
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                let id = itemID(row, index)
                let done = on.contains(id)
                Button {
                    Task { await onState(widgetToggle(widget.stateJson, id: id)) }
                } label: {
                    HStack(spacing: 10) {
                        Image(systemName: done ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(done ? Theme.emerald : .secondary)
                            .font(.silo(.body))
                        Text(WidgetJSON.text(row["text"]))
                            .font(.silo(.subheadline))
                            .strikethrough(done)
                            .foregroundStyle(done ? .secondary : .primary)
                            .multilineTextAlignment(.leading)
                        Spacer(minLength: 0)
                    }
                    .padding(.vertical, 6)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .sensoryFeedback(.selection, trigger: done)
                .accessibilityAddTraits(done ? .isSelected : [])
                if index < rows.count - 1 { Divider() }
            }
        }
    }

    private var progress: some View {
        let v = WidgetJSON.number(data["value"]) ?? 0
        let max = WidgetJSON.number(data["max"]) ?? 100
        let fraction = max > 0 ? Swift.max(0, Swift.min(1, v / max)) : 0
        let c = WidgetJSON.text(data["caption"])
        return VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 3) {
                Text("\(Int((fraction * 100).rounded()))").font(.silo(.largeTitle, .medium)).monospacedDigit()
                Text("%").font(.siloMono(.footnote)).foregroundStyle(.secondary)
            }
            Text(c.isEmpty ? "\(WidgetJSON.format(v)) / \(WidgetJSON.format(max))" : c).font(.silo(.footnote)).foregroundStyle(.secondary)
            ProgressView(value: fraction).tint(.primary)
        }
    }

    // MARK: actions and footer

    @ViewBuilder
    private func rowMenu(_ rowActs: [WidgetAction], _ row: [String: Any]) -> some View {
        if !rowActs.isEmpty {
            Menu {
                ForEach(rowActs) { action in
                    Button(action.label) { trigger(action, row) }
                }
            } label: {
                Image(systemName: "ellipsis.circle").foregroundStyle(.secondary)
            }
            .disabled(!running.isEmpty)
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
        }
    }

    @ViewBuilder
    private var footer: some View {
        let own = acts.filter { !$0.row }
        if !own.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(Array(own.enumerated()), id: \.element.id) { index, action in
                        Button { trigger(action, [:]) } label: {
                            HStack(spacing: 5) {
                                if running == action.id { ProgressView().controlSize(.mini) } else if action.hand { Image(systemName: "bubble.left") }
                                Text(action.label)
                            }
                        }
                        .buttonStyle(.quiet)
                        .tint(index == 0 && !action.hand ? Theme.cobalt : .secondary)
                        .controlSize(.small)
                        .disabled(!running.isEmpty)
                    }
                }
            }
        }
        if !widget.error.isEmpty {
            DisclosureGroup {
                Text(widget.error).font(.siloMono(.caption)).foregroundStyle(.secondary).textSelection(.enabled)
            } label: {
                Label("Couldn't refresh", systemImage: "exclamationmark.circle").font(.silo(.caption)).foregroundStyle(.secondary)
            }
        }
        if let rc = widgetReceipt(widget.receiptJson) {
            HStack(spacing: 6) {
                Image(systemName: rc.ok ? "checkmark" : "xmark").foregroundStyle(rc.ok ? Theme.emerald : .secondary)
                Text(rc.text).lineLimit(1)
                Spacer(minLength: 4)
                Text([rc.by, relativeTime(rc.at)].filter { !$0.isEmpty }.joined(separator: " · ")).foregroundStyle(.secondary)
            }
            .font(.silo(.caption))
        }
    }
}

/// A hairline with its last point marked: no fill, no grid.
private struct Sparkline: View {
    let values: [Double]

    var body: some View {
        Chart(Array(values.enumerated()), id: \.offset) { i, v in
            LineMark(x: .value("i", i), y: .value("v", v)).foregroundStyle(Color.primary).interpolationMethod(.monotone)
            if i == values.count - 1 { PointMark(x: .value("i", i), y: .value("v", v)).foregroundStyle(Color.primary).symbolSize(20) }
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartYScale(domain: .automatic(includesZero: false))
        .accessibilityLabel("Trend")
    }
}

extension Array {
    subscript(safe index: Int) -> Element? { indices.contains(index) ? self[index] : nil }
}

// MARK: - Asks

private struct AskRow: View {
    let card: Silo_V1_DashboardCard

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "questionmark.bubble").font(.system(size: 15)).foregroundStyle(Theme.cobalt).padding(.top, 2)
            VStack(alignment: .leading, spacing: 4) {
                Text(card.title).font(.silo(.headline)).foregroundStyle(.primary).multilineTextAlignment(.leading)
                if !card.summary.isEmpty {
                    Text(card.summary).font(.silo(.subheadline)).foregroundStyle(.secondary).lineLimit(3).multilineTextAlignment(.leading)
                }
                if !card.sourceName.isEmpty {
                    Text(card.sourceName).font(.silo(.caption)).foregroundStyle(.tertiary)
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right").font(.silo(.footnote, .semibold)).foregroundStyle(.tertiary)
        }
        .fixedSize(horizontal: false, vertical: true)
        .widgetCard()
    }
}

/// One ask as a sheet: its context blocks, native controls for its questions, and one submit that
/// opens a chat with the answers. "Not now" puts it away; "Don't suggest this again" also tells the Bot.
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
    @State private var busy = false

    var body: some View {
        NavigationStack {
            Form {
                if !card.summary.isEmpty {
                    Section { Text(card.summary).font(.silo(.body)) }
                }
                ForEach(blocks) { block in
                    blockView(block)
                }
                Section {
                    TextField("Add a note (optional)", text: $note, axis: .vertical).lineLimit(1...5)
                }
                Section {
                    Button("Not now") { Task { await dismissCard(suppress: false) } }
                    if !inThread {
                        Button("Don't suggest this again", role: .destructive) { Task { await dismissCard(suppress: true) } }
                    }
                }
            }
            .navigationTitle(card.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(card.submitLabel.isEmpty ? "Send" : card.submitLabel) { Task { await submit() } }
                        .disabled(busy)
                }
            }
            .disabled(busy)
        }
        .onAppear {
            let parsed = parseAskBlocks(card.blocksJson)
            blocks = parsed.blocks
            values = parsed.defaults
        }
    }

    @ViewBuilder
    private func blockView(_ b: AskBlock) -> some View {
        switch b.type {
        case "markdown":
            if !b.text.isEmpty { Section { ProseView(text: b.text) } }
        case "kv", "metrics":
            Section {
                ForEach(b.pairs, id: \.self) { pair in
                    LabeledContent(pair.label) {
                        HStack(spacing: 6) {
                            Text(pair.value).monospacedDigit()
                            if !pair.delta.isEmpty {
                                Text(pair.delta).font(.siloMono(.caption))
                                    .foregroundStyle(pair.tone == "good" ? Theme.emerald : pair.tone == "bad" ? Theme.vermilion : .secondary)
                            }
                        }
                    }
                }
            }
        case "diff":
            Section(b.path) {
                ScrollView(.horizontal) { Text(b.text).font(.siloMono(.caption)).textSelection(.enabled) }
            }
        case "switch":
            Section {
                Toggle(isOn: boolBinding(b.name)) {
                    Text(b.label)
                    if !b.hint.isEmpty { Text(b.hint) }
                }
            }
        case "select", "segmented":
            Section {
                if b.type == "segmented" && b.options.count <= 4 {
                    Picker(b.label, selection: stringBinding(b.name)) {
                        ForEach(b.options, id: \.value) { Text($0.label).tag($0.value) }
                    }
                    .pickerStyle(.segmented)
                } else {
                    Picker(b.label, selection: stringBinding(b.name)) {
                        ForEach(b.options, id: \.value) { Text($0.label).tag($0.value) }
                    }
                }
            } header: {
                if b.type == "segmented", !b.label.isEmpty { Text(b.label) }
            } footer: {
                if !b.hint.isEmpty { Text(b.hint) }
            }
        case "checkboxes":
            Section {
                ForEach(b.options, id: \.value) { option in
                    let on = listValue(b.name).contains(option.value)
                    Button {
                        var list = listValue(b.name)
                        if on { list.removeAll { $0 == option.value } } else { list.append(option.value) }
                        values[b.name] = .list(list)
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(option.label).foregroundStyle(.primary)
                                if !option.hint.isEmpty { Text(option.hint).font(.silo(.caption)).foregroundStyle(.secondary) }
                            }
                            Spacer()
                            if on { Image(systemName: "checkmark").foregroundStyle(Theme.cobalt) }
                        }
                    }
                }
            } header: {
                if !b.label.isEmpty { Text(b.label) }
            } footer: {
                if !b.hint.isEmpty { Text(b.hint) }
            }
        case "input", "textarea":
            Section {
                TextField(b.placeholder.isEmpty ? b.label : b.placeholder, text: stringBinding(b.name), axis: b.type == "textarea" ? .vertical : .horizontal)
                    .lineLimit(b.type == "textarea" ? 3...8 : 1...1)
            } header: {
                if !b.label.isEmpty { Text(b.label) }
            } footer: {
                if !b.hint.isEmpty { Text(b.hint) }
            }
        default:
            EmptyView()
        }
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
        busy = true
        defer { busy = false }
        do {
            let out = try await model.client.submitDashboardCard(botID: botID, id: card.id, valuesJSON: encodeAskValues(values), note: note)
            dismiss()
            await changed()
            if out.hasChat, !out.chat.id.isEmpty { model.openChat(out.chat) }
        } catch { await model.report(error) }
    }

    private func dismissCard(suppress: Bool) async {
        guard let botID = model.selectedBotID else { return }
        busy = true
        defer { busy = false }
        do {
            try await model.client.dismissDashboardCard(botID: botID, id: card.id, suppressKind: suppress)
            dismiss()
            await changed()
            await model.refreshBots()
        } catch { await model.report(error) }
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
        VStack(alignment: .leading, spacing: 0) {
            ForEach(ideas, id: \.self) { idea in
                Button {
                    model.composerInsert = idea.prompt
                    model.showMainChat()
                } label: {
                    HStack(spacing: 8) {
                        Text(idea.title).font(.silo(14)).foregroundStyle(Theme.ink2).multilineTextAlignment(.leading)
                        Spacer(minLength: 4)
                        Image(systemName: "arrow.up.right").font(.system(size: 11, weight: .medium)).foregroundStyle(Theme.ink3)
                    }
                    .padding(.vertical, 8)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
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
        VStack(alignment: .leading, spacing: 4) {
            Text("THINGS TO ASK \((model.selectedBot?.name ?? "").uppercased())")
                .font(.silo(11, .medium))
                .tracking(0.9)
                .foregroundStyle(Theme.ink3)
            IdeaList(ideas: ideas)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .widgetCard()
        .task(id: model.selectedBotID) {
            guard let botID = model.selectedBotID,
                  let rows = try? await model.client.listBotConnectors(botID: botID) else { return }
            connected = Set(rows.flatMap { [$0.connector.builtin, $0.connector.identifier] }.filter { !$0.isEmpty })
        }
    }
}

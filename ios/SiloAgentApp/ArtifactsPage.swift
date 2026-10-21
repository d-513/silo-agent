import QuickLook
import SiloClient
import SwiftUI

extension Silo_V1_ArtifactItem: @retroactive Identifiable {}

/// Files, skills and apps this Bot has handed over across conversations, newest first. Tap to open
/// the latest version; older versions are in the row's menu.
struct ArtifactsPage: View {
    @Environment(AppModel.self) private var model
    @State private var items: [Silo_V1_ArtifactItem] = []
    @State private var loaded = false
    @State private var preview: URL?
    @State private var app: IdentifiedURL?
    @State private var opening = ""
    @State private var deleting: Silo_V1_ArtifactItem?

    var body: some View {
        List {
            ForEach(items) { item in
                Button { Task { await open(item, item.versions.last) } } label: {
                    ArtifactRow(item: item, opening: opening == item.id)
                }
                .buttonStyle(.plain)
                .swipeActions {
                    Button(role: .destructive) { deleting = item } label: { Label("Delete", systemImage: "trash") }
                }
                .contextMenu {
                    Button { Task { await open(item, item.versions.last) } } label: {
                        Label(item.type == "app" ? "Open App" : "Open", systemImage: item.type == "app" ? "macwindow" : "eye")
                    }
                    if item.versions.count > 1 {
                        Menu("Versions") {
                            ForEach(item.versions.reversed(), id: \.n) { v in
                                Button("v\(v.n) · \(relativeTime(v.createdAt))") { Task { await open(item, v) } }
                                    .disabled(v.gone)
                            }
                        }
                    }
                    if !item.chatID.isEmpty, let chat = model.chats.first(where: { $0.id == item.chatID }) {
                        Button { model.openChat(chat) } label: { Label("Open Chat", systemImage: "bubble.left.and.bubble.right") }
                    }
                    Button(role: .destructive) { deleting = item } label: { Label("Delete", systemImage: "trash") }
                }
            }
        }
        .listStyle(.plain)
        .overlay {
            if loaded && items.isEmpty {
                ContentUnavailableView("No artifacts yet", systemImage: "shippingbox",
                                       description: Text("When the Bot hands you a file, a skill or an app, it is kept here across versions."))
            }
        }
        .quickLookPreview($preview)
        .sheet(item: $app) { SafariView(url: $0.url).ignoresSafeArea() }
        .confirmationDialog("Delete this artifact?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { item in
            Button("Delete \(item.title.isEmpty ? "artifact" : item.title)", role: .destructive) { Task { await delete(item) } }
        } message: { _ in
            Text("Every kept version is forgotten. Files in the workspace stay.")
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { items = try await model.client.listArtifacts(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func open(_ item: Silo_V1_ArtifactItem, _ version: Silo_V1_ArtifactVersion?) async {
        guard let botID = model.selectedBotID, let version, opening.isEmpty else { return }
        opening = item.id
        defer { opening = "" }
        if item.type == "app" {
            do {
                if let url = URL(string: try await model.client.openArtifactApp(botID: botID, artifactID: item.id, version: version.n)) {
                    app = IdentifiedURL(url: url)
                }
            } catch { await model.report(error) }
            return
        }
        preview = await model.fetchArtifact(item, version: version)
    }

    private func delete(_ item: Silo_V1_ArtifactItem) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.deleteArtifact(botID: botID, id: item.id)
            items.removeAll { $0.id == item.id }
        } catch { await model.report(error) }
    }
}

private struct ArtifactRow: View {
    let item: Silo_V1_ArtifactItem
    let opening: Bool

    var body: some View {
        HStack(spacing: 12) {
            thumbnail
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.silo(.headline)).lineLimit(1)
                Text(subtitle).font(.silo(.caption)).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 6)
            if opening { ProgressView() } else { Text(relativeTime(item.updatedAt)).font(.silo(.caption)).foregroundStyle(.secondary) }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    private var latest: Silo_V1_ArtifactVersion? { item.versions.last }

    private var title: String {
        if !item.title.isEmpty { return item.title }
        if let name = latest?.name, !name.isEmpty { return name }
        return item.path
    }

    private var subtitle: String {
        var parts: [String] = [item.type == "app" ? "App" : item.type == "skill" ? "Skill" : "File"]
        if item.versions.count > 1 { parts.append("v\(item.versions.count)") }
        if let size = latest?.size, size > 0 { parts.append(formatBytes(size)) }
        switch item.sourceKind {
        case "automation": parts.append("Automation · \(item.sourceName)")
        case "channel": parts.append("Channel · \(item.sourceName)")
        default: if !item.sourceName.isEmpty { parts.append(item.sourceName) }
        }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder
    private var thumbnail: some View {
        if let data = latest.flatMap({ imageData($0.previewImage) }), let image = UIImage(data: data) {
            Image(uiImage: image).resizable().scaledToFill().frame(width: 44, height: 44)
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        } else {
            Image(systemName: symbol)
                .font(.system(size: 18))
                .foregroundStyle(.secondary)
                .frame(width: 44, height: 44)
                .background(Theme.well, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
    }

    private var symbol: String {
        if item.type == "app" { return "macwindow" }
        if item.type == "skill" { return "scroll" }
        switch ((latest?.name ?? item.path) as NSString).pathExtension.lowercased() {
        case "pdf": return "doc.richtext"
        case "png", "jpg", "jpeg", "gif", "webp", "heic": return "photo"
        case "csv", "xlsx", "xls": return "tablecells"
        case "zip", "tar", "gz": return "doc.zipper"
        default: return "doc"
        }
    }

    /// An emit-time preview arrives as a data URL.
    private func imageData(_ url: String) -> Data? {
        guard url.hasPrefix("data:"), let comma = url.firstIndex(of: ",") else { return nil }
        return Data(base64Encoded: String(url[url.index(after: comma)...]))
    }
}

extension Silo_V1_AuditEvent: @retroactive Identifiable {}

/// What the Bot's guards decided: approvals, the output filter, the network filter. "Needs
/// attention" is what was flagged for a human.
struct AuditPage: View {
    @Environment(AppModel.self) private var model
    @State private var view = "attention"
    @State private var summary: Silo_V1_AuditSummaryResponse?
    @State private var events: [Silo_V1_AuditEvent] = []
    @State private var cursor = ""
    @State private var more = false
    @State private var query = ""
    @State private var loaded = false

    var body: some View {
        List {
            Section {
                Picker("Show", selection: $view) {
                    Text("Needs attention").tag("attention")
                    Text("Everything").tag("")
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            } footer: {
                if let summary {
                    Text("\(summary.requests) requests · \(summary.blocked) blocked · \(summary.flagged) flagged in the last \(summary.trafficDays) days. Lines are kept for \(summary.detailDays) days.")
                }
            }
            Section {
                ForEach(events) { event in
                    AuditRow(event: event)
                }
                if more {
                    Button("Load Older") { Task { await load(reset: false) } }
                }
            }
        }
        .searchable(text: $query, prompt: "Search the timeline")
        .onSubmit(of: .search) { Task { await load(reset: true) } }
        .onChange(of: query) { _, value in if value.isEmpty { Task { await load(reset: true) } } }
        .onChange(of: view) { Task { await load(reset: true) } }
        .overlay {
            if loaded && events.isEmpty {
                ContentUnavailableView(view == "attention" ? "Nothing needs your attention" : "Nothing on the timeline yet",
                                       systemImage: "checkmark.shield")
            }
        }
        .refreshable { await load(reset: true) }
        .task(id: model.selectedBotID) {
            await load(reset: true)
            guard let botID = model.selectedBotID else { return }
            summary = try? await model.client.auditSummary(botID: botID)
            if model.selectedBot?.auditUnseen ?? 0 > 0 {
                try? await model.client.markAuditRead(botID: botID)
                await model.refreshBots()
            }
        }
    }

    private func load(reset: Bool) async {
        guard let botID = model.selectedBotID else { return }
        do {
            let page = try await model.client.listAuditEvents(botID: botID, view: view, query: query, cursor: reset ? "" : cursor)
            events = reset ? page.events : events + page.events
            cursor = page.nextCursor
            more = page.more
        } catch { await model.report(error) }
        loaded = true
    }
}

private struct AuditRow: View {
    @Environment(AppModel.self) private var model
    let event: Silo_V1_AuditEvent

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                verdict
                Text(event.action).font(.siloMono(.subheadline)).lineLimit(1)
                Spacer(minLength: 6)
                Text(relativeTime(event.at)).font(.silo(.caption)).foregroundStyle(.secondary)
            }
            Text(line).font(.silo(.caption)).foregroundStyle(.secondary).lineLimit(3)
            if !event.chatID.isEmpty {
                Button {
                    if let chat = model.chats.first(where: { $0.id == event.chatID }) { model.openChat(chat) } else { model.showChat(event.chatID) }
                } label: {
                    Text(event.chatTitle.isEmpty ? "Open the conversation" : event.chatTitle).font(.silo(.caption)).lineLimit(1)
                }
                .buttonStyle(.borderless)
            }
        }
        .padding(.vertical, 2)
    }

    private var line: String {
        var parts = [who]
        if !event.host.isEmpty { parts.append(event.port > 0 ? "\(event.host):\(event.port)" : event.host) }
        let reason = why
        if !reason.isEmpty { parts.append(reason) }
        if !event.detail.isEmpty { parts.append(event.detail) }
        if event.bytesUp > 0 || event.bytesDown > 0 { parts.append("↑ \(formatBytes(event.bytesUp)) ↓ \(formatBytes(event.bytesDown))") }
        return parts.joined(separator: " · ")
    }

    private var who: String {
        switch event.actor {
        case "egress": return "Network filter"
        case "filter": return "Output filter"
        case "auto": return "Auto-approval"
        case "worker": return "The Bot"
        default: return event.actor
        }
    }

    private var why: String {
        [
            "threat_list": "On a threat list",
            "private": "A private address",
            "blocked_host": "On the blocked list",
            "exfil_sink": "Upload to a host that swallows data",
            "surrogate_leak": "A stand-in secret sent in the clear",
            "secret_shape": "Something shaped like a secret",
            "upload_budget": "Over the upload budget",
            "beaconing": "Calling out at a steady beat",
        ][event.reason] ?? event.reason
    }

    private var verdict: some View {
        let (label, color): (String, Color) = {
            switch event.decision {
            case "deny", "auto_deny", "block": return ("Denied", Theme.vermilion)
            case "output_withhold": return ("Withheld", Theme.vermilion)
            case "allow", "auto_allow", "allow_once": return ("Allowed", Theme.emerald)
            case "flag", "output_flag": return ("Flagged", Theme.cobalt)
            case "output_unchecked": return ("Unchecked", .secondary)
            default: return (event.decision.isEmpty ? "Unknown" : event.decision, .secondary)
            }
        }()
        return Text(label)
            .font(.silo(.caption2, .semibold))
            .foregroundStyle(color)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(color.opacity(0.12), in: Capsule())
    }
}

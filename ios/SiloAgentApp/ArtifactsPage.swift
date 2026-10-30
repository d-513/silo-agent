import QuickLook
import SiloClient
import SwiftUI

extension Silo_V1_ArtifactItem: @retroactive Identifiable {}

/// Files, skills and apps this Bot has handed over across conversations, newest first. Press to
/// open the latest version; older versions are in the card's menu.
struct ArtifactsPage: View {
    @Environment(AppModel.self) private var model
    @State private var items: [Silo_V1_ArtifactItem] = []
    @State private var loaded = false
    @State private var preview: URL?
    @State private var app: IdentifiedURL?
    @State private var opening = ""
    @State private var deleting: Silo_V1_ArtifactItem?

    var body: some View {
        PageScroll(spacing: 14) {
            if !loaded {
                SkeletonRows(count: 4, height: 64)
            } else if items.isEmpty {
                EmptyWell(symbol: "shippingbox", title: "No artifacts yet",
                          message: "When the Bot hands you a file, a skill or an app, it is kept here across versions.")
            } else {
                ForEach(items) { item in
                    VStack(alignment: .leading, spacing: 6) {
                        caption(item)
                        card(item)
                    }
                }
            }
        }
        .quickLookPreview($preview)
        .sheet(item: $app) { SafariView(url: $0.url).ignoresSafeArea() }
        .confirmSlip(item: $deleting, title: { _ in "Delete this artifact?" }, message: "Every kept version is forgotten. Files in the workspace stay.",
                     action: { "Delete \($0.title.isEmpty ? "artifact" : $0.title)" }) { item in
            Task { await delete(item) }
        }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
    }

    /// When it was handed over, where from, and the path it came from.
    private func caption(_ item: Silo_V1_ArtifactItem) -> some View {
        HStack(spacing: 5) {
            Text(When.stamp(item.updatedAt.isEmpty ? item.createdAt : item.updatedAt)).font(.siloMono(12)).layoutPriority(1)
            let source = sourceWords(item)
            if !source.isEmpty {
                Text("·").accessibilityHidden(true)
                Text(source).lineLimit(1)
            }
            if !item.path.isEmpty {
                Text("·").accessibilityHidden(true)
                Text(item.path).font(.siloMono(12)).lineLimit(1).truncationMode(.middle)
            }
        }
        .font(.silo(12))
        .foregroundStyle(Theme.ink3)
        .padding(.horizontal, 4)
    }

    private func sourceWords(_ item: Silo_V1_ArtifactItem) -> String {
        switch item.sourceKind {
        case "automation": return "Automation · \(item.sourceName)"
        case "channel": return "Channel · \(item.sourceName)"
        default: return item.sourceName
        }
    }

    private func card(_ item: Silo_V1_ArtifactItem) -> some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        return HStack(spacing: 4) {
            Button { Task { await open(item, item.versions.last) } } label: {
                ArtifactRow(item: item)
            }
            .buttonStyle(.press)
            if opening == item.id {
                Spinner(size: 15).frame(width: 40, height: 40)
            } else {
                menu(item)
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, 4)
        .padding(.vertical, 10)
        .background(Theme.surface, in: shape)
        .overlay(shape.stroke(Theme.line))
    }

    private func menu(_ item: Silo_V1_ArtifactItem) -> some View {
        SiloMenu {
            MenuRow(title: item.type == "app" ? "Open app" : "Open", symbol: item.type == "app" ? "macwindow" : "eye") {
                Task { await open(item, item.versions.last) }
            }
            if !item.chatID.isEmpty, let chat = model.chats.first(where: { $0.id == item.chatID }) {
                MenuRow(title: "Open chat", symbol: "bubble.left.and.bubble.right") { model.openChat(chat) }
            }
            if item.versions.count > 1 {
                MenuDivider()
                MenuHeading(text: "Versions")
                ForEach(item.versions.reversed(), id: \.n) { v in
                    MenuRow(title: "v\(v.n) · \(relativeTime(v.createdAt))", enabled: !v.gone) { Task { await open(item, v) } }
                }
            }
            MenuDivider()
            MenuRow(title: "Delete", symbol: "trash", destructive: true) { deleting = item }
        } label: {
            Image(systemName: "ellipsis")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(Theme.ink2)
                .frame(width: 40, height: 40)
        }
        .accessibilityLabel("More for \(item.title.isEmpty ? item.path : item.title)")
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
            withAnimation(Motion.state) { items.removeAll { $0.id == item.id } }
        } catch { await model.report(error) }
    }
}

private struct ArtifactRow: View {
    let item: Silo_V1_ArtifactItem

    var body: some View {
        HStack(spacing: 12) {
            thumbnail
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.silo(14.5, .medium)).foregroundStyle(Theme.ink).lineLimit(1)
                Text(subtitle).font(.silo(12.5)).foregroundStyle(Theme.ink3).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
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
        return parts.joined(separator: " · ")
    }

    @ViewBuilder
    private var thumbnail: some View {
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous)
        if let data = latest.flatMap({ imageData($0.previewImage) }), let image = UIImage(data: data) {
            Image(uiImage: image).resizable().scaledToFill().frame(width: 44, height: 44).clipShape(shape)
        } else if item.type == "app" || item.type == "skill" {
            Image(systemName: item.type == "app" ? "macwindow" : "scroll")
                .font(.system(size: 18))
                .foregroundStyle(Theme.ink2)
                .frame(width: 44, height: 44)
                .background(Theme.well, in: shape)
        } else {
            // The extension in a small well; a PDF's is vermilion.
            let ext = ((latest?.name ?? item.path) as NSString).pathExtension.uppercased().prefix(4)
            Text(ext.isEmpty ? "FILE" : String(ext))
                .font(.silo(10.5, .semibold))
                .tracking(0.4)
                .foregroundStyle(ext == "PDF" ? Theme.vermilion : Theme.ink2)
                .frame(width: 44, height: 44)
                .background(ext == "PDF" ? Theme.vermilionPale : Theme.well, in: shape)
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
        PageScroll(spacing: 12) {
            Segmented(options: [(value: "attention", label: "Needs attention"), (value: "", label: "Everything")], selection: $view)
            SearchBox(prompt: "Search the timeline", text: $query) { Task { await load(reset: true) } }
            if let summary {
                Text("\(summary.requests) requests · \(summary.blocked) blocked · \(summary.flagged) flagged in the last \(summary.trafficDays) days. Lines are kept for \(summary.detailDays) days.")
                    .font(.silo(12.5))
                    .foregroundStyle(Theme.ink3)
                    .padding(.horizontal, 4)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !loaded {
                SkeletonRows(count: 5, height: 56)
            } else if events.isEmpty {
                EmptyWell(symbol: "checkmark.shield", title: view == "attention" ? "Nothing needs your attention" : "Nothing on the timeline yet")
            } else {
                Panel {
                    ForEach(events) { event in
                        AuditRow(event: event)
                    }
                }
            }
            if more {
                Button("Load older") { Task { await load(reset: false) } }
                    .buttonStyle(.quiet)
                    .controlSize(.small)
                    .frame(maxWidth: .infinity)
            }
        }
        .onChange(of: query) { _, value in if value.isEmpty { Task { await load(reset: true) } } }
        .onChange(of: view) { Task { await load(reset: true) } }
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
                Text(event.action).font(.siloMono(12.5)).foregroundStyle(Theme.ink).lineLimit(1)
                Spacer(minLength: 6)
                Text(When.run(event.at)).font(.silo(12)).foregroundStyle(Theme.ink3).lineLimit(1)
            }
            Text(line).font(.silo(12.5)).foregroundStyle(Theme.ink2).lineLimit(3)
            if !event.chatID.isEmpty {
                Button {
                    if let chat = model.chats.first(where: { $0.id == event.chatID }) { model.openChat(chat) } else { model.showChat(event.chatID) }
                } label: {
                    Text(event.chatTitle.isEmpty ? "Open the conversation" : event.chatTitle)
                        .font(.silo(12.5))
                        .foregroundStyle(Theme.cobalt)
                        .lineLimit(1)
                        .frame(minHeight: 26)
                }
                .buttonStyle(.press)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
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

    private var verdict: Chip {
        switch event.decision {
        case "deny", "auto_deny", "block": return Chip(text: "Denied", tint: Theme.vermilion, fill: Theme.vermilionPale)
        case "output_withhold": return Chip(text: "Withheld", tint: Theme.vermilion, fill: Theme.vermilionPale)
        case "allow", "auto_allow", "allow_once": return Chip(text: "Allowed", tint: Theme.emerald, fill: Theme.emerald.opacity(0.1))
        case "flag", "output_flag": return Chip(text: "Flagged", tint: Theme.cobalt, fill: Theme.cobaltPale)
        case "output_unchecked": return Chip(text: "Unchecked")
        default: return Chip(text: event.decision.isEmpty ? "Unknown" : event.decision)
        }
    }
}

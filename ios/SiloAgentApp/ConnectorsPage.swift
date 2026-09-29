import SiloClient
import SwiftUI

extension Silo_V1_BotConnector: @retroactive Identifiable {}
extension Silo_V1_Connector: @retroactive Identifiable {}

/// Connectors attached to this Bot. Adding a library preset works here; OAuth and custom MCP forms
/// stay on the web (see `ios/todo_skipped.md`).
struct ConnectorsPage: View {
    @Environment(AppModel.self) private var model
    @State private var rows: [Silo_V1_BotConnector] = []
    @State private var loaded = false
    @State private var adding = false
    @State private var detaching: Silo_V1_BotConnector?
    @State private var busy: String?

    var body: some View {
        List {
            ForEach(rows) { row in
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(row.connector.name).font(.headline)
                        Spacer()
                        if busy == row.id || row.authStatus == "initializing" {
                            ProgressView().controlSize(.small)
                        } else {
                            Text(statusLabel(row)).font(.caption.weight(.medium)).foregroundStyle(tone(row))
                        }
                    }
                    if !row.connector.description_p.isEmpty {
                        Text(row.connector.description_p).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                    if !row.lastError.isEmpty {
                        Text(row.lastError).font(.caption).foregroundStyle(Theme.vermilion).lineLimit(3)
                    } else if row.authStatus == "needs_auth" {
                        Text(row.connector.auth == "oauth" ? "Authorize on the web client." : (row.statusDetail.isEmpty ? "Set up on the web client." : row.statusDetail))
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 2)
                .swipeActions {
                    Button(role: .destructive) { detaching = row } label: { Label("Detach", systemImage: "minus.circle") }
                    Button { Task { await refresh(row) } } label: { Label("Refresh", systemImage: "arrow.clockwise") }.tint(Theme.cobalt)
                }
            }
        }
        .overlay {
            if loaded && rows.isEmpty {
                ContentUnavailableView("No connectors", systemImage: "puzzlepiece.extension", description: Text("Connect tools like GitHub or Email."))
            }
        }
        .toolbar { ToolbarItem(placement: .primaryAction) { Button { adding = true } label: { Label("Add", systemImage: "plus") } } }
        .refreshable { await load() }
        .task(id: model.selectedBotID) { await load() }
        .sheet(isPresented: $adding) { AddConnectorSheet(attachedSources: Set(rows.map { $0.connector.sourceID })) { await load() } }
        .confirmationDialog("Detach this connector?", isPresented: Binding(get: { detaching != nil }, set: { if !$0 { detaching = nil } }), titleVisibility: .visible, presenting: detaching) { row in
            Button("Detach \(row.connector.name)", role: .destructive) { Task { await detach(row) } }
        } message: { _ in Text("Its rules are removed. Attach it again any time.") }
    }

    private func statusLabel(_ row: Silo_V1_BotConnector) -> String {
        switch row.authStatus {
        case "connected", "ready", "ok": return "Connected"
        case "needs_auth": return row.connector.auth == "oauth" ? "Needs authorization" : "Needs setup"
        case "error": return "Error"
        case "initializing": return "Starting"
        default: return row.authStatus.replacingOccurrences(of: "_", with: " ").capitalized
        }
    }

    private func tone(_ row: Silo_V1_BotConnector) -> Color {
        switch row.authStatus {
        case "connected", "ready", "ok": return Theme.emerald
        case "needs_auth", "error": return Theme.vermilion
        default: return .secondary
        }
    }

    private func load() async {
        guard let botID = model.selectedBotID else { return }
        do { rows = try await model.client.listBotConnectors(botID: botID) } catch { await model.report(error) }
        loaded = true
    }

    private func refresh(_ row: Silo_V1_BotConnector) async {
        guard let botID = model.selectedBotID else { return }
        busy = row.id
        defer { busy = nil }
        do { _ = try await model.client.refreshBotConnector(botID: botID, id: row.id) } catch { await model.report(error) }
        await load()
    }

    private func detach(_ row: Silo_V1_BotConnector) async {
        guard let botID = model.selectedBotID else { return }
        do {
            try await model.client.detachConnector(botID: botID, id: row.id)
            rows.removeAll { $0.id == row.id }
        } catch { await model.report(error) }
    }
}

private struct AddConnectorSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let attachedSources: Set<String>
    let onAdded: () async -> Void
    @State private var library: [Silo_V1_Connector] = []
    @State private var loaded = false
    @State private var query = ""
    @State private var adding: String?

    var body: some View {
        NavigationStack {
            List(filtered) { item in
                Button {
                    Task { await attach(item) }
                } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(item.name).font(.headline).foregroundStyle(.primary)
                            if !item.description_p.isEmpty {
                                Text(item.description_p).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                            }
                        }
                        Spacer()
                        if adding == item.id { ProgressView().controlSize(.small) } else {
                            Image(systemName: "plus.circle").foregroundStyle(Theme.cobalt)
                        }
                    }
                }
            }
            .searchable(text: $query, prompt: "Search connectors")
            .overlay { if loaded && library.isEmpty { ContentUnavailableView("The library is empty", systemImage: "puzzlepiece.extension") } }
            .navigationTitle("Add connector")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } } }
            .task {
                do { library = try await model.client.listConnectors() } catch { await model.report(error) }
                loaded = true
            }
        }
    }

    private var filtered: [Silo_V1_Connector] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return library.filter { q.isEmpty || ($0.name + " " + $0.description_p).lowercased().contains(q) }
    }

    private func attach(_ item: Silo_V1_Connector) async {
        guard let botID = model.selectedBotID else { return }
        adding = item.id
        defer { adding = nil }
        do {
            _ = try await model.client.attachConnector(botID: botID, connectorID: item.id)
            await onAdded()
            dismiss()
        } catch { await model.report(error) }
    }
}

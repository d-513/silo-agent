import SiloClient
import SwiftUI

extension Silo_V1_Connector: @retroactive Identifiable {}

/// The Workspace's connectors: every connection kept once here, which Bots use each, and adding
/// one from the library. A connection opens to Choose Bots, its sign-in and lending, and Delete.
/// OAuth sign-in, editing and custom MCP servers stay on the web (see `ios/todo_skipped.md`).
struct SpaceConnectorsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @State private var rows: [Silo_V1_Connector] = []
    @State private var loaded = false
    @State private var adding = false
    @State private var query = ""

    private var shown: [Silo_V1_Connector] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return rows.filter { q.isEmpty || ($0.name + " " + $0.description_p).lowercased().contains(q) }
    }

    var body: some View {
        List {
            Section {
                ForEach(shown) { c in
                    NavigationLink {
                        ConnectionPage(id: c.id, initial: c)
                    } label: {
                        ConnectionRow(c: c)
                    }
                }
            } footer: {
                Text("Connect a service once for the whole workspace, then choose which Bots can use it. Each Bot keeps its own session and its own rules for it.")
            }
        }
        .siloList()
        .searchable(text: $query, prompt: "Search connectors")
        .overlay {
            if loaded && rows.isEmpty {
                ContentUnavailableView {
                    Label("No connectors in this workspace yet", systemImage: "puzzlepiece.extension")
                } description: {
                    Text("Add one from the library, then choose which Bots can use it.")
                } actions: {
                    Button("Browse Library") { adding = true }.buttonStyle(.ink)
                }
            }
        }
        .toolbar { ToolbarItem(placement: .primaryAction) { Button { adding = true } label: { Label("Add", systemImage: "plus") } } }
        .refreshable { await load() }
        .task(id: spaces.currentID) { await load() }
        .sheet(isPresented: $adding) { AddConnectorSheet(names: rows.map(\.name)) { await load() } }
    }

    private func load() async {
        guard let spaceID = spaces.currentID else { return }
        do { rows = try await model.client.listSpaceConnectors(spaceID: spaceID) } catch { await model.report(error) }
        loaded = true
    }
}

/// One connection in the list: its name, what it is, whose sign-in it carries, who uses it.
private struct ConnectionRow: View {
    let c: Silo_V1_Connector

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(c.name).font(.silo(16, .medium)).lineLimit(1)
                if !c.category.isEmpty { WellChip(text: c.category, tone: Theme.ink3) }
                Spacer(minLength: 0)
                if needsSignIn(c) {
                    HStack(spacing: 5) {
                        StatusDot(status: "needs_you", size: 7)
                        Text("Needs sign-in").font(.silo(12, .medium)).foregroundStyle(Theme.vermilion)
                    }
                }
            }
            let about = c.description_p.isEmpty ? (c.transport == "stdio" ? c.stdioCommand : c.httpURL) : c.description_p
            if !about.isEmpty {
                Text(about).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(2)
            }
            UsedByLine(ids: c.usedBy, hidden: c.usedByHidden)
        }
        .padding(.vertical, 2)
    }
}

private func needsSignIn(_ c: Silo_V1_Connector) -> Bool { c.auth == "oauth" && !c.signedIn }

/// A connection: Choose Bots, its sign-in and lending, and Delete for who added it or an admin.
private struct ConnectionPage: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    @Environment(\.dismiss) private var dismiss
    let id: String
    @State private var c: Silo_V1_Connector
    @State private var deleting = false

    init(id: String, initial: Silo_V1_Connector) {
        self.id = id
        _c = State(initialValue: initial)
    }

    var body: some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: 6) {
                    Text(c.name).font(.silo(22, .semibold))
                    if !c.description_p.isEmpty {
                        Text(c.description_p).font(.silo(14)).foregroundStyle(Theme.ink2)
                    }
                    Text(c.transport == "builtin" ? "Built-in" : c.transport == "stdio" ? c.stdioCommand : c.httpURL)
                        .font(.siloMono(12)).foregroundStyle(Theme.ink3).lineLimit(2)
                    if !c.slug.isEmpty {
                        Text("tools.\(c.slug)").font(.siloMono(12)).foregroundStyle(Theme.ink3)
                    }
                }
                .padding(.vertical, 4)
                if needsSignIn(c) {
                    Label("Needs sign-in. Sign in on the web client.", systemImage: "exclamationmark.circle")
                        .font(.silo(14)).foregroundStyle(Theme.vermilion)
                } else if !c.lentBy.isEmpty {
                    Text("Signed in by \(c.lentBy)").font(.silo(14)).foregroundStyle(Theme.ink2)
                }
            }
            LendSection(lentBy: c.lentBy, shared: c.shared) { shared in
                await act { try await model.client.setLendShared(connectorID: c.id, shared: shared) }
            }
            ChooseBotsSection(granted: c.usedBy) { botID, on in
                await act {
                    if on { try await model.client.grantConnector(botID: botID, connectorID: c.id) } else {
                        try await model.client.revokeConnector(botID: botID, connectorID: c.id)
                    }
                }
            }
            if c.canEdit {
                Section {
                    Button("Delete connector", role: .destructive) { deleting = true }
                } footer: {
                    Text("Editing its settings is on the web client. Changes there apply to every Bot that uses it.")
                }
            }
        }
        .siloList()
        .navigationTitle(c.name)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await reload() }
        .confirmationDialog("Delete \(c.name)?", isPresented: $deleting, titleVisibility: .visible) {
            Button("Delete", role: .destructive) {
                Task {
                    await act { try await model.client.deleteConnector(id: c.id) }
                    dismiss()
                }
            }
        } message: {
            Text(c.usedBy.isEmpty && c.usedByHidden == 0 ? "It is removed from the workspace." : "Every Bot that uses it loses it too.")
        }
    }

    private func act(_ work: () async throws -> Void) async {
        do { try await work() } catch { await model.report(error) }
        await reload()
    }

    private func reload() async {
        guard let spaceID = spaces.currentID,
              let list = try? await model.client.listSpaceConnectors(spaceID: spaceID),
              let fresh = list.first(where: { $0.id == id }) else { return }
        c = fresh
    }
}

/// The library: pick a preset, name the copy, choose which Bots get it.
private struct AddConnectorSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let names: [String]
    let onAdded: () async -> Void
    @State private var library: [Silo_V1_Connector] = []
    @State private var loaded = false
    @State private var query = ""

    var body: some View {
        NavigationStack {
            List(filtered) { item in
                NavigationLink {
                    AddPresetForm(preset: item, names: names) {
                        await onAdded()
                        dismiss()
                    }
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.name).font(.silo(16, .medium))
                        if !item.description_p.isEmpty {
                            Text(item.description_p).font(.silo(13)).foregroundStyle(Theme.ink2).lineLimit(2)
                        }
                    }
                }
            }
            .siloList()
            .searchable(text: $query, prompt: "Search the library")
            .overlay { if loaded && library.isEmpty { ContentUnavailableView("The library is empty", systemImage: "puzzlepiece.extension") } }
            .navigationTitle("Library")
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
        return library.filter { q.isEmpty || ($0.name + " " + $0.description_p + " " + $0.category).lowercased().contains(q) }
    }
}

private struct AddPresetForm: View {
    @Environment(AppModel.self) private var model
    @Environment(SpaceModel.self) private var spaces
    let preset: Silo_V1_Connector
    let names: [String]
    let onDone: () async -> Void
    @State private var name = ""
    @State private var picked: Set<String> = []
    @State private var busy = false
    @State private var failure: String?

    var body: some View {
        Form {
            if !preset.catalogGuide.isEmpty {
                Section("Before you add") { ProseView(text: preset.catalogGuide) }
            }
            Section {
                TextField("Name", text: $name)
            } header: { Text("Name") } footer: {
                Text(preset.auth == "oauth" || !preset.fields.isEmpty
                     ? "Signing in or filling in its settings is on the web client, once it is added."
                     : "The Bots it is given reach its tools through import tools in Python.")
            }
            BotsStep(picked: $picked)
            if let failure { Section { Text(failure).foregroundStyle(Theme.vermilion) } }
        }
        .siloList()
        .navigationTitle(preset.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(busy ? "Adding…" : "Add") { Task { await add() } }
                    .disabled(busy || name.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        }
        .onAppear {
            guard name.isEmpty else { return }
            name = nextCopyName(preset.name, taken: names)
            picked = Set(spaces.managedBots.map(\.id))
        }
    }

    private func add() async {
        guard let spaceID = spaces.currentID else { return }
        busy = true
        defer { busy = false }
        do {
            _ = try await model.client.addLibraryConnector(spaceID: spaceID, preset: preset,
                                                           name: name.trimmingCharacters(in: .whitespaces), botIDs: Array(picked))
            await onDone()
        } catch {
            failure = (error as? SiloError)?.errorDescription ?? error.localizedDescription
        }
    }
}
